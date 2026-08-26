// Package mediasoup adapts a mediasoup-based backend into canonical events.
//
// This package is a placeholder for the second backend, and it is empty on
// purpose. Its value is entirely in being architecturally different from
// LiveKit: mediasoup is a library, not a server, so there is no vendor-defined
// webhook contract to receive. Lifecycle signals come from whatever application
// wraps it, in whatever shape that application chose.
//
// That difference is the point (ADR-0003). An abstraction validated against two
// similar SFUs proves nothing; the seam is only tested by a backend that does
// not share LiveKit's assumptions about delivery, identity or event naming.
// Concretely, this adapter is expected to break at least one assumption the
// LiveKit adapter makes, and finding out which one is the exercise.
//
// TODO(scope): entirely deferred to a later phase. Not part of the first
// milestone.
package mediasoup
