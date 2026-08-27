// Package webhook is the HTTP entry point for media-backend event deliveries.
//
// Responsibilities: bind the listener, authenticate the delivery, hand the
// verified payload to the adapter registry, and answer. Nothing else. In
// particular it does not parse the payload's meaning — that is the adapter's
// job, and keeping the split sharp is what makes a second backend (ADR-0003)
// a new adapter rather than a new receiver.
//
// The hard constraint on this package is latency. LiveKit retries deliveries
// it considers failed, so slow handling converts a downstream stall into
// duplicate inbound traffic — backpressure that makes the problem worse. The
// receiver must therefore acknowledge on durable receipt, not on completed
// processing. Correlation happens after the response is written.
//
// Authentication differs per backend: LiveKit signs deliveries with a JWT in
// the Authorization header whose sha256 claim covers the body, which means the
// body must be read and hashed before it can be trusted. Verification belongs
// here because it is transport-level; the per-backend detail of how to verify
// belongs to the adapter.
//
// The receiver acknowledges after one transaction covering both event_raw and
// participant_join, which amends ADR-0011's original plan of acknowledging on
// durable receipt and correlating later — ADR-0019 moved the expensive part to
// read time, and the insert's conflict is what decides whether the join effect
// runs at all. ADR-0025 records the reversal.
//
// Response codes here are instructions to the sender's retry machinery, not
// status reports; ADR-0027 has the table, including why a missing partition
// answers 200 and drops the event.
//
// TODO(scope): a signal separating an integration break from ordinary bad
// requests. A malformed rate of 100% from a verified sender is a different
// event from an occasional bad body, and today they look identical.
package webhook
