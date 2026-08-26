package config

import "testing"

func TestLoadServiceDefaults(t *testing.T) {
	t.Setenv("SAP_ENVIRONMENT", "")
	t.Setenv("SAP_LISTEN_ADDR", "")
	t.Setenv("SAP_OTLP_ENDPOINT", "")
	t.Setenv("SAP_OTLP_INSECURE", "")

	cfg, err := LoadService("svc", "v0", ":8080")
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.OTLPEndpoint != "localhost:4317" {
		t.Errorf("OTLPEndpoint = %q, want %q", cfg.OTLPEndpoint, "localhost:4317")
	}
	if !cfg.OTLPInsecure {
		t.Error("OTLPInsecure = false, want true by default")
	}
	if cfg.Environment != "local" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "local")
	}
}

func TestLoadServiceEnvOverrides(t *testing.T) {
	t.Setenv("SAP_LISTEN_ADDR", ":9999")
	t.Setenv("SAP_OTLP_ENDPOINT", "otel-collector:4317")
	t.Setenv("SAP_OTLP_INSECURE", "false")
	t.Setenv("SAP_ENVIRONMENT", "staging")

	cfg, err := LoadService("svc", "v0", ":8080")
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9999")
	}
	if cfg.OTLPEndpoint != "otel-collector:4317" {
		t.Errorf("OTLPEndpoint = %q, want %q", cfg.OTLPEndpoint, "otel-collector:4317")
	}
	if cfg.OTLPInsecure {
		t.Error("OTLPInsecure = true, want false")
	}
	if cfg.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "staging")
	}
}

func TestBoolEnvFallsBackOnGarbage(t *testing.T) {
	t.Setenv("SAP_OTLP_INSECURE", "not-a-bool")

	cfg, err := LoadService("svc", "v0", ":8080")
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if !cfg.OTLPInsecure {
		t.Error("OTLPInsecure = false, want the default to survive an unparseable value")
	}
}
