// Package adapter is the seam between a specific media backend and the
// canonical event model in internal/session.
//
// This package will define the interface every backend implements and the
// registry that routes a delivery to the right one. The interface itself is
// deliberately absent from the scaffolding commit: an abstraction designed
// against exactly one implementation is a description of that implementation
// wearing an interface. LiveKit gets built first, mediasoup gets built second,
// and the shape of the interface is whatever survives the second one
// (ADR-0003).
//
// The test the seam has to pass: adding a backend touches this directory and
// nothing else. If adding mediasoup requires changing internal/session,
// internal/ingest/store, or internal/query, the abstraction leaked and the
// article writes itself.
//
// TODO(scope): the adapter interface is explicitly out of scope. Do not
// introduce a placeholder interface here — a placeholder gets implemented
// against, and then it is not a placeholder.
package adapter
