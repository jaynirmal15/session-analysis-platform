// Package api serves the read-side HTTP interface.
//
// Two query shapes, with genuinely different cost profiles:
//
//   - Aggregate metrics across many sessions in a time window. Wide, shallow,
//     partition-pruned when the caller supplies a bounded range and
//     catastrophic when they do not.
//   - Per-session drill-down: the full correlated timeline for one session.
//     Narrow and deep, and the query that justifies keeping raw events rather
//     than only rollups.
//
// The tension to design against: Grafana reads Postgres directly for
// per-session panels (ADR-0008), so this API is not the only consumer of the
// schema. Anything this package treats as a private detail is not private.
//
// TODO(scope): handlers, routing and the response contract are out of scope
// pending the schema (ADR-0014).
package api
