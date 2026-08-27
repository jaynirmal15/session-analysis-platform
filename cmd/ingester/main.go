// Command ingester is the write path: it receives media-backend webhooks,
// translates them into canonical events, persists them, and maintains the
// joins those events open and close.
//
// It acknowledges a delivery after one transaction covering both event_raw and
// participant_join. That is a change from ADR-0011's original plan of
// acknowledging on durable receipt and correlating afterwards — ADR-0019 moved
// session grouping to read time, so the only write-time work left is two
// indexed statements, and splitting them would cost the atomicity that makes
// redelivery idempotent. ADR-0025 records the reversal.
//
// TODO(scope): correlation beyond opening and closing joins, and the mediasoup
// adapter. No sweeper closes joins by timeout, and none ever should
// (ADR-0020).
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
	"github.com/jaynirmal15/session-analysis-platform/internal/database"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/adapter/livekit"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/metrics"
	pgstore "github.com/jaynirmal15/session-analysis-platform/internal/ingest/store/postgres"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/webhook"
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

	cfg, err := config.LoadIngester(serviceName, version, defaultListenAddr)
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, cfg.Service)
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

	pool, err := database.Open(ctx, cfg.DatabaseURL, database.IngestOptions())
	if err != nil {
		return err
	}
	defer pool.Close()

	verifier, err := livekit.NewVerifier(cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	if err != nil {
		return err
	}

	ingestMetrics, err := metrics.NewIngest()
	if err != nil {
		return err
	}

	handler := webhook.NewLiveKitHandler(
		verifier, pgstore.New(pool), ingestMetrics, logger, cfg.MaxBodyBytes)

	mux := http.NewServeMux()

	// Liveness only. It deliberately does not check the database: a receiver
	// that reports itself unhealthy during a database blip would be restarted
	// by an orchestrator, and restarting cannot fix a database. Storage
	// failures surface as 5xx on the ingest path, which is where the sender
	// can act on them.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// Readiness does check the database, because a receiver that cannot write
	// should not be sent traffic.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ready\n"))
	})

	mux.Handle("POST /webhook/livekit", handler)

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
			slog.String("webhook", "POST /webhook/livekit"),
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
