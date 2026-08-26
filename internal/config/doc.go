// Package config loads process-level configuration from the environment.
//
// This is one of the three packages that both the write path (cmd/ingester)
// and the read path (cmd/queryapi) are permitted to import. See ADR-0009 in
// ARCHITECTURE.md: the shared surface is limited to genuinely neutral
// concerns — configuration, telemetry setup, and database connection
// construction. There is deliberately no "shared" or "common" package.
//
// What belongs here: process knobs. Listen addresses, exporter endpoints,
// connection strings, timeouts, log levels.
//
// What does not belong here: anything that encodes a domain concept. Retention
// windows, partition intervals, correlation timeouts and event-type allowlists
// are decisions owned by the packages that act on them, not by config.
//
// TODO(scope): configuration for the store, webhook receiver and correlation
// stage lands here once those components exist and their schema is designed.
package config
