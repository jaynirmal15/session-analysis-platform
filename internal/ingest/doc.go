// Package ingest is the root of the write path: everything that turns an
// external media-backend signal into a durable, correlated record.
//
// The write path is bursty and driven by someone else's schedule. A single
// large room teardown can produce thousands of events in a second, and the
// backend delivering them will retry on failure. Its subpackages are shaped
// around that: accept fast, persist raw, correlate after.
//
//	webhook  -> receive and authenticate an inbound delivery
//	adapter  -> translate a backend's payload into the canonical event
//	store    -> persist the canonical event durably
//	pipeline -> derive session timelines from persisted events
//
// Nothing under internal/query may import this package, or the reverse. The
// two paths meet at the database and at internal/session, nowhere else
// (ADR-0009).
package ingest
