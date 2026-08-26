// Package store is the read path's persistence port.
//
// It is a separate port from internal/ingest/store, describing the same
// database, because the two paths ask genuinely different questions of it. The
// write side needs idempotent append; the read side needs bounded-range scans
// and single-session lookups. Collapsing them into one interface produces a
// type that is a union of two unrelated needs and satisfies neither well.
//
// TODO(scope): the port interface. The tables now exist (ADR-0024) and the
// primary query is known — joins overlapping a bounded range, windowed by
// (backend, room, identity), grouped under a caller-supplied gap threshold
// (ADR-0019) — but no query code is written yet.
package store
