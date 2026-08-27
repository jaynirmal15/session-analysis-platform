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
// Two insert failures here are signals rather than faults, and both must be
// counted rather than propagated as crashes.
//
// A missing partition — occurred_at is the backend's clock and there is no
// DEFAULT partition, so a late or clock-skewed event has nowhere to land. This
// is the behaviour ADR-0024 chose deliberately, but only if it is measured:
// uncounted, a skewed integrator arrives as a crash loop instead of a number.
// Detect it as SQLSTATE 23514 with an EMPTY constraint name — an ordinary CHECK
// violation carries the same code and a populated one — and increment
// sap_ingest_partition_missing_total, labelled by backend. It shares a
// dashboard with the stale-open-join panel (ADR-0020); both measure the gap
// between what the backend sent and what we managed to record.
//
// An out-of-order pair (ended_at < started_at) fails at the generated
// active_range column with SQLSTATE 22000, not at the CHECK constraint that
// appears to guard it. That constraint is unreachable; do not match on it.
//
// TODO(scope): the read side of this port. Nothing here reads yet; correlation
// beyond opening and closing joins is not this package's job (ADR-0019).
package store
