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
// TODO(scope): the canonical event and session types are being designed
// separately and are intentionally absent. Nothing in this repository should
// guess at them — a guessed schema that ships is a schema that gets built on.
// See ADR-0014.
package session
