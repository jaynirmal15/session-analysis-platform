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
// are fixed by ADR-0022. Signature verification is in verify.go and uses
// golang-jwt rather than LiveKit's SDK, for reasons and at a cost recorded in
// ADR-0026.
//
// One wire-format detail worth knowing before editing the payload struct:
// LiveKit serialises int64 fields as JSON *strings*, per protobuf's canonical
// JSON mapping. Declaring them as int64 makes every real delivery fail to
// decode while every hand-written fixture passes. See protoInt64 in jsonnum.go.
//
// TODO(scope): nothing here is exercised against LiveKit versions other than
// the one in compose. scripts/webhook-smoke.sh is the guard, and it is only
// worth anything run against a real server rather than a recorded fixture.
package livekit
