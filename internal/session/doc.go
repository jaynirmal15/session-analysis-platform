// Package session defines the canonical vocabulary of the platform: what a
// session is, what an event is, and what a correlated timeline looks like.
//
// This is the one domain package that both the write path and the read path
// depend on, and it is a deliberate exception to the shared-surface rule in
// ADR-0009. The alternative — a separate write model and read model that
// happen to describe the same thing — is a real option and is what a strict
// CQRS reading would recommend, but it is not worth two divergent definitions
// of "session" this early. ADR-0015 records the exception and the signal that
// should end it.
//
// The types here are backend-neutral by construction. A LiveKit
// participant_joined and the mediasoup equivalent must both land as the same
// canonical event, or the adapter seam (ADR-0003) is not doing its job. If a
// type in this package can only be produced by one backend, that type belongs
// in that backend's adapter instead.
//
// The vocabulary now exists (ADR-0024). Two things in it are worth knowing
// before reading the types:
//
// A Join is durable and a Session is not. Sessions are derived at read time by
// grouping a participant's joins under a reconnect-gap threshold, so Session
// carries the Gap that produced it — the same joins grouped at 30s and at 120s
// are two correct and different answers (ADR-0019).
//
// A nil EndedAt means "still open, or we never found out". It is never filled
// in by a sweeper, because the difference between a live participant and a lost
// event is a measurement worth keeping (ADR-0020).
//
// TODO(scope): nothing here is persisted or read yet. The store ports, the
// correlation stage and the query handlers are all still unwritten.
package session
