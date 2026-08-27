package session

import "time"

// Backend names a media backend. It is stored on every row so a single
// deployment can ingest from more than one at a time.
type Backend string

const (
	BackendLiveKit   Backend = "livekit"
	BackendMediasoup Backend = "mediasoup"
)

// EventType is the canonical event vocabulary. These names are ours, not any
// backend's: a LiveKit participant_joined and its mediasoup equivalent must
// both arrive as EventParticipantJoined, or the adapter seam is not doing its
// job (ADR-0003).
type EventType string

const (
	EventRoomStarted       EventType = "room_started"
	EventRoomFinished      EventType = "room_finished"
	EventParticipantJoined EventType = "participant_joined"
	EventParticipantLeft   EventType = "participant_left"
	EventTrackPublished    EventType = "track_published"
	EventTrackUnpublished  EventType = "track_unpublished"
)

// CorrelatedEventTypes are the types the write path derives joins from.
//
// This is the vocabulary, not the intake policy. Which deliveries are accepted
// or rejected at the boundary is the receiver's decision and lives with the
// receiver (ADR-0022); unrecognised types are stored rather than dropped,
// because an unrecognised type is evidence the integration drifted.
var CorrelatedEventTypes = []EventType{
	EventRoomStarted,
	EventRoomFinished,
	EventParticipantJoined,
	EventParticipantLeft,
	EventTrackPublished,
	EventTrackUnpublished,
}

// EndReason records how a join's end was learned, not merely that it ended.
type EndReason string

const (
	// EndParticipantLeft: the participant's own departure was observed.
	EndParticipantLeft EndReason = "participant_left"

	// EndRoomFinished: the room ended, so every join still open in it is
	// closed. This is not inference — the backend is stating the room is gone
	// (ADR-0020). It stays distinct from EndParticipantLeft because "closed
	// because the room ended" carries different information than "left".
	EndRoomFinished EndReason = "room_finished"

	// EndInferredTimeout is reserved and never written. No sweeper closes
	// joins by timeout; see ADR-0020 on why open-and-stale is a measurement
	// worth keeping rather than an error worth repairing.
	EndInferredTimeout EndReason = "inferred_timeout"
)

// Event is one received webhook delivery, as stored in event_raw.
//
// Payload holds the full original body. The typed fields above it are exactly
// what correlation keys on; everything else stays in Payload because mediasoup
// will not share LiveKit's shape.
type Event struct {
	// ID is derived, never copied from the backend. Deterministic derivation
	// is what makes at-least-once delivery idempotent (ADR-0011).
	//
	// uuidv5 over a length-prefixed join of the backend's own event id where
	// one exists, and otherwise of (event_type, room, participant identity,
	// participant SID, track SID, occurred_at to nanoseconds, delivery
	// ordinal). ADR-0024 specifies the inputs exactly and explains why the
	// payload is not among them: hashing it both collapses distinct events
	// and splits redelivered ones.
	ID string

	Backend        Backend
	BackendEventID string
	Type           EventType

	// OccurredAt is the backend's own clock and the partition key.
	// ReceivedAt is ours. Arrival order is not event order, so nothing may
	// order by ReceivedAt.
	OccurredAt time.Time
	ReceivedAt time.Time

	RoomName            string
	RoomSID             string
	ParticipantIdentity string
	ParticipantSID      string
	TrackSID            string

	Payload []byte
}

// Join is one participant's presence in one room, from an observed join to an
// observed end. It is the durable unit (ADR-0019).
//
// Reconnects produce separate Joins. They are deliberately not stitched
// together at write time — grouping them into a Session applies a policy
// threshold, and policy belongs at read time.
type Join struct {
	ID      string
	Backend Backend

	// RoomName and ParticipantIdentity are the correlation key. Identity is
	// scoped to the room and no attempt is made to resolve the same person
	// across rooms (ADR-0017). Its stability within a room is an integration
	// contract the caller must uphold (ADR-0016).
	RoomName            string
	ParticipantIdentity string

	RoomSID string
	// ParticipantSID changes on every reconnect. Kept for traceability back to
	// the backend; never used for correlation.
	ParticipantSID string

	StartedAt time.Time

	// EndedAt is nil for "still open, or we never found out". Both are honest
	// states and the difference between them is inferred from staleness by the
	// reader, not stamped here by a sweeper (ADR-0020).
	EndedAt   *time.Time
	EndReason EndReason

	StartedEventID string
	EndedEventID   string
}

// IsOpen reports whether no end has been observed for this join. It does not
// mean the participant is still connected — only that nothing told us
// otherwise.
func (j Join) IsOpen() bool { return j.EndedAt == nil }

// Session is a derived view, never a stored row: the joins of one participant
// in one room, grouped under a reconnect-gap threshold (ADR-0019).
//
// Two Sessions built from identical Joins under different thresholds are both
// correct. That is the point — the threshold is a property of the question,
// so it travels with the query rather than with the data.
type Session struct {
	Backend             Backend
	RoomName            string
	ParticipantIdentity string

	// Gap is the threshold this grouping was produced under. It is carried on
	// the result so a Session is never interpretable without knowing the
	// policy that produced it.
	Gap time.Duration

	Joins []Join
}

// StartedAt is the beginning of the session's first join.
func (s Session) StartedAt() time.Time { return s.Joins[0].StartedAt }

// IsOpen reports whether the session's final join has no observed end.
func (s Session) IsOpen() bool { return s.Joins[len(s.Joins)-1].IsOpen() }

// Reconnects is the number of times the participant rejoined within the
// threshold, i.e. one fewer than the number of joins.
func (s Session) Reconnects() int { return len(s.Joins) - 1 }
