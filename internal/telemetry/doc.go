// Package telemetry wires the OpenTelemetry SDK for every binary in this
// repository.
//
// Telemetry is present from the first commit on purpose (ADR-0005). The
// platform's whole subject is observing real-time media sessions; a system
// that cannot observe itself has no standing to make claims about anything
// else. Instrumenting later would also mean retrofitting context propagation
// through code written without it, which is the expensive direction.
//
// This is one of the three packages both the write path and the read path may
// import. See ADR-0009 in ARCHITECTURE.md.
//
// Export path: process -> OTLP/gRPC -> OpenTelemetry Collector -> Prometheus
// exporter -> Prometheus scrape. The process never speaks Prometheus directly;
// see ADR-0006 for why the collector hop is worth its operational cost.
//
// Traces are configured but no spans are recorded yet. There is no trace
// backend in the local stack — see ADR-0007 on why Tempo is deferred rather
// than rejected. The tracer provider exists so that adding one later is a
// collector configuration change, not an application change.
package telemetry
