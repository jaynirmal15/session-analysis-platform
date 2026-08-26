package config

import (
	"fmt"
	"os"
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
