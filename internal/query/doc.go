// Package query is the root of the read path: everything that serves aggregate
// metrics and per-session drill-down.
//
// The read path is driven by humans and dashboards, not by the media backend,
// and its failure modes are the opposite of the write path's. A slow query
// degrades a dashboard; a slow write loses an event. They are separated into
// distinct package trees and distinct binaries so that neither one's tuning
// silently becomes the other's constraint (ADR-0009).
//
// Nothing under internal/ingest may import this package, or the reverse.
package query
