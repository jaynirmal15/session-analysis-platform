// Command ingester is the write path: it will receive media-backend webhooks,
// translate them into canonical events, persist them, and correlate them into
// session timelines.
//
// It does none of that yet. This binary currently boots the OpenTelemetry SDK,
// serves a health endpoint, and exits cleanly on a signal. That is deliberate:
// it makes the telemetry path (process -> collector -> Prometheus -> Grafana)
// real and verifiable before any domain code exists to be observed.
//
// TODO(scope): register the webhook receiver and start the correlation
// pipeline once the event schema is designed. See ARCHITECTURE.md ADR-0014.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jaynirmal15/session-analysis-platform/internal/config"
	"github.com/jaynirmal15/session-analysis-platform/internal/telemetry"
)

// version is stamped at build time; see the Makefile.
var version = "dev"

const (
	serviceName       = "sap-ingester"
	defaultListenAddr = ":8080"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		slog.String("service", serviceName),
		slog.String("version", version),
	)
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadService(serviceName, version, defaultListenAddr)
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logger.Warn("telemetry shutdown", slog.Any("error", err))
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// TODO(scope): mount the webhook receiver here.
	//   mux.Handle("POST /webhook/{backend}", webhook.Handler(...))

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening",
			slog.String("addr", cfg.ListenAddr),
			slog.String("otlp_endpoint", cfg.OTLPEndpoint),
			slog.String("note", "scaffolding only: no webhook handlers registered"),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
