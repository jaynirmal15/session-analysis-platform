// Package store is the read path's persistence port.
//
// It is a separate port from internal/ingest/store, describing the same
// database, because the two paths ask genuinely different questions of it. The
// write side needs idempotent append; the read side needs bounded-range scans
// and single-session lookups. Collapsing them into one interface produces a
// type that is a union of two unrelated needs and satisfies neither well.
//
// TODO(scope): deferred with the schema (ADR-0014).
package store
