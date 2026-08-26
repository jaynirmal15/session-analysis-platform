// Package store is the write path's persistence port: the interface the
// pipeline and adapters use to durably record events and correlated sessions.
//
// It exists as its own package so the correlation stage can be tested without
// a database and so the storage decision (ADR-0004) stays reversible. The
// bet on plain PostgreSQL is expected to hit a ceiling; a port makes reaching
// that ceiling a migration rather than a rewrite.
//
// The write side's demands are narrow and specific: high-volume append,
// idempotent insert on a stable event identity, and bulk paths that do not
// hold a transaction open across a correlation pass.
//
// TODO(scope): the port interface. The tables it writes to now exist
// (ADR-0024) — event_raw for intake, participant_join for the durable unit —
// but no persistence code is written yet.
package store
