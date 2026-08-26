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
// TODO(scope): signature verification, the handler, and the delivery contract
// are out of scope for the scaffolding commit. See ADR-0011 for why ingest is
// push-based at all.
package webhook
