// Command queryapi is the read path: it will serve aggregate session metrics
// and per-session drill-down over HTTP.
//
// It serves no routes yet beyond a health endpoint. Like the ingester, it boots
// the OpenTelemetry SDK from the first commit so the observability path is
// verifiable before there is anything to observe.
//
// TODO(scope): register query handlers once the schema is designed. See
// ARCHITECTURE.md ADR-0014.
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
	serviceName       = "sap-queryapi"
	defaultListenAddr = ":8081"
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

	// TODO(scope): mount the query API here.
	//   mux.Handle("GET /v1/sessions", api.ListSessions(...))
	//   mux.Handle("GET /v1/sessions/{id}", api.SessionTimeline(...))

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
			slog.String("note", "scaffolding only: no query handlers registered"),
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
