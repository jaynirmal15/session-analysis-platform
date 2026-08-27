package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Service is the configuration shared by every binary in this repository.
//
// It is intentionally small. Each binary embeds it alongside its own
// configuration rather than growing a single struct that knows about both the
// write path and the read path.
type Service struct {
	// Name identifies the service in telemetry. It becomes the OpenTelemetry
	// service.name resource attribute.
	Name string

	// Version is stamped into telemetry as service.version. It is injected at
	// build time via -ldflags; see the Makefile.
	Version string

	// Environment is stamped into telemetry as deployment.environment.
	Environment string

	// ListenAddr is the address the service's HTTP server binds to.
	ListenAddr string

	// OTLPEndpoint is the host:port of the OpenTelemetry Collector's OTLP
	// gRPC receiver. Inside docker compose this is "otel-collector:4317";
	// for `make run-local` the collector is reached on "localhost:4317".
	OTLPEndpoint string

	// OTLPInsecure disables transport security on the OTLP connection. True
	// for local development, where the collector is a sibling container.
	OTLPInsecure bool
}

// LoadService reads Service configuration from the environment, applying
// defaultListenAddr when SAP_LISTEN_ADDR is unset.
//
// Every knob has a working default so that `go run ./cmd/...` and
// `docker compose up` both succeed with no environment file present.
func LoadService(name, version, defaultListenAddr string) (Service, error) {
	cfg := Service{
		Name:         name,
		Version:      version,
		Environment:  env("SAP_ENVIRONMENT", "local"),
		ListenAddr:   env("SAP_LISTEN_ADDR", defaultListenAddr),
		OTLPEndpoint: env("SAP_OTLP_ENDPOINT", "localhost:4317"),
		OTLPInsecure: boolEnv("SAP_OTLP_INSECURE", true),
	}
	if cfg.ListenAddr == "" {
		return Service{}, fmt.Errorf("config: listen address must not be empty")
	}
	if cfg.OTLPEndpoint == "" {
		return Service{}, fmt.Errorf("config: SAP_OTLP_ENDPOINT must not be empty")
	}
	return cfg, nil
}

// Ingester is the write path's configuration: the neutral Service knobs plus
// what the receiver needs to authenticate deliveries and reach the database.
//
// It lives beside Service rather than inside it because the query API needs
// none of these, and a single struct that knew about both paths would be the
// first crack in ADR-0009.
type Ingester struct {
	Service

	// DatabaseURL is a libpq-style connection string.
	DatabaseURL string

	// LiveKitAPIKey and LiveKitAPISecret authenticate inbound webhooks. The
	// secret verifies the delivery signature; it is never logged, and the
	// receiver refuses to start without it rather than accepting unverified
	// traffic (ADR-0026).
	LiveKitAPIKey    string
	LiveKitAPISecret string

	// MaxBodyBytes caps a single webhook body. A delivery larger than this is
	// rejected before it is read into memory.
	MaxBodyBytes int64
}

// LoadIngester reads the write path's configuration from the environment.
//
// It fails rather than defaulting when a credential is missing: a receiver that
// starts without a verification secret would accept unsigned traffic, which is
// worse than not starting.
func LoadIngester(name, version, defaultListenAddr string) (Ingester, error) {
	svc, err := LoadService(name, version, defaultListenAddr)
	if err != nil {
		return Ingester{}, err
	}

	cfg := Ingester{
		Service:          svc,
		DatabaseURL:      env("SAP_DATABASE_URL", "postgres://sap:sap@localhost:5432/sap?sslmode=disable"),
		LiveKitAPIKey:    env("SAP_LIVEKIT_API_KEY", ""),
		LiveKitAPISecret: env("SAP_LIVEKIT_API_SECRET", ""),
		MaxBodyBytes:     intEnv("SAP_MAX_BODY_BYTES", 1<<20),
	}
	if cfg.LiveKitAPIKey == "" {
		return Ingester{}, fmt.Errorf("config: SAP_LIVEKIT_API_KEY is required")
	}
	if cfg.LiveKitAPISecret == "" {
		return Ingester{}, fmt.Errorf("config: SAP_LIVEKIT_API_SECRET is required")
	}
	if cfg.MaxBodyBytes <= 0 {
		return Ingester{}, fmt.Errorf("config: SAP_MAX_BODY_BYTES must be positive")
	}
	return cfg, nil
}

func intEnv(key string, fallback int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "y", "yes":
		return true
	case "0", "f", "false", "n", "no":
		return false
	default:
		return fallback
	}
}
