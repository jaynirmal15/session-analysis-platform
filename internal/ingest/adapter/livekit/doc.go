// Package livekit adapts LiveKit webhook deliveries into canonical events.
//
// LiveKit is the first backend (ADR-0002). It pushes lifecycle events —
// room_started, participant_joined, track_published, and their counterparts —
// as signed JSON over HTTP, which means correlation state can be built from
// deliveries alone with no polling.
//
// Known shape problems this package will have to absorb, recorded now because
// they are the reason the adapter layer exists:
//
//   - Deliveries are at-least-once. Retries mean the same event can arrive
//     twice, so translation must produce a stable identity per event.
//   - Ordering is not guaranteed. A participant_left can be processed before
//     the participant_joined it follows, so the canonical event must carry the
//     backend's own timestamp and never rely on arrival order.
//   - LiveKit's identifiers are LiveKit's. Room names and participant
//     identities are caller-supplied and not globally unique across time; the
//     canonical identity has to be derived, not copied.
//
// The events this adapter accepts, rejects and stores-despite-not-recognising
// are fixed by ADR-0022.
//
// TODO(scope): translation into session.Event. The target schema now exists
// (ADR-0024); the mapping does not.
package livekit
