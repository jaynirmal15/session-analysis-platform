package livekit

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/eventid"
	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

// Backend names this adapter's backend in stored rows.
const Backend = session.BackendLiveKit

// Disposition is what the receiver should do with a delivery, decided by its
// event type alone (ADR-0022).
type Disposition int

const (
	// Ingest: one of the six types the platform correlates on.
	Ingest Disposition = iota

	// Reject: known to exist, known to be out of scope. Egress and ingress
	// describe operations performed on a room, not experiences of a
	// participant. Storing them would inflate the very table whose ceiling
	// ADR-0004 exists to measure, making that measurement describe a workload
	// we do not have.
	Reject

	// Store: a type we do not recognise, kept anyway. Known-and-irrelevant is a
	// decision; unknown is a surprise, and a surprise is evidence the
	// integration drifted. A counter alone would tell us a type appeared while
	// destroying what it contained.
	Store
)

// canonical maps LiveKit's event names onto ours. The mapping is explicit
// rather than a string cast so that a rename on their side surfaces as an
// unrecognised type here instead of silently becoming a new canonical value.
var canonical = map[string]session.EventType{
	"room_started":       session.EventRoomStarted,
	"room_finished":      session.EventRoomFinished,
	"participant_joined": session.EventParticipantJoined,
	"participant_left":   session.EventParticipantLeft,
	"track_published":    session.EventTrackPublished,
	"track_unpublished":  session.EventTrackUnpublished,
}

var rejected = map[string]bool{
	"egress_started": true, "egress_updated": true, "egress_ended": true,
	"ingress_started": true, "ingress_ended": true,
}

// payload is the subset of LiveKit's webhook body this adapter reads.
//
// Deliberately not their generated protobuf type. Everything unread survives in
// event_raw.payload, and keeping their types out of the ingest path is what
// stops LiveKit's model from quietly becoming the canonical model (ADR-0003).
type payload struct {
	Event string `json:"event"`
	ID    string `json:"id"`

	// CreatedAt is LiveKit's clock, in Unix seconds.
	CreatedAt protoInt64 `json:"createdAt"`

	Room *struct {
		SID          string     `json:"sid"`
		Name         string     `json:"name"`
		CreationTime protoInt64 `json:"creationTime"`
	} `json:"room"`

	Participant *struct {
		SID      string     `json:"sid"`
		Identity string     `json:"identity"`
		JoinedAt protoInt64 `json:"joinedAt"`
	} `json:"participant"`

	Track *struct {
		SID string `json:"sid"`
	} `json:"track"`
}

// Classify reports the disposition of a delivery without translating it.
func Classify(eventName string) Disposition {
	if _, ok := canonical[eventName]; ok {
		return Ingest
	}
	if rejected[eventName] {
		return Reject
	}
	return Store
}

// Translate converts a verified delivery into a canonical event.
//
// ordinal is the event's index within its delivery; LiveKit sends one event per
// request, so it is zero today. It is threaded through because a backend that
// batches would need it as the last-resort identity discriminator (ADR-0024),
// and retrofitting it would change every previously derived id.
func Translate(body []byte, ordinal int) (session.Event, Disposition, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return session.Event{}, Store, fmt.Errorf("livekit: decode delivery: %w", err)
	}
	if p.Event == "" {
		return session.Event{}, Store, fmt.Errorf("livekit: delivery carries no event type")
	}

	disp := Classify(p.Event)
	if disp == Reject {
		// Returned with the type populated but nothing else: the caller needs
		// the name to label its counter, and parsing the body twice to get it
		// would be worse than returning a partial event that is never stored.
		return session.Event{Backend: Backend, Type: session.EventType(p.Event)}, Reject, nil
	}

	ev := session.Event{
		Backend:        Backend,
		BackendEventID: p.ID,
		Type:           canonical[p.Event],
		OccurredAt:     occurredAt(p),
		Payload:        body,
	}
	// An unrecognised type keeps LiveKit's own name. Inventing a canonical
	// value for something we do not understand would put a guess in the column
	// queries group by.
	if disp == Store {
		ev.Type = session.EventType(p.Event)
	}
	if p.Room != nil {
		ev.RoomSID, ev.RoomName = p.Room.SID, p.Room.Name
	}
	if p.Participant != nil {
		ev.ParticipantSID, ev.ParticipantIdentity = p.Participant.SID, p.Participant.Identity
	}
	if p.Track != nil {
		ev.TrackSID = p.Track.SID
	}

	// Primary path: LiveKit supplies its own event id, so identity needs no
	// reasoning from us. The fallback exists for backends that do not, and is
	// exercised here whenever a delivery arrives without one.
	if p.ID != "" {
		ev.ID = eventid.FromBackendEventID(Backend, p.ID).String()
	} else {
		ev.ID = eventid.FromFields(eventid.Fields{
			Backend:             Backend,
			EventType:           ev.Type,
			Room:                ev.RoomName,
			ParticipantIdentity: ev.ParticipantIdentity,
			ParticipantSID:      ev.ParticipantSID,
			TrackSID:            ev.TrackSID,
			OccurredAt:          ev.OccurredAt,
			DeliveryOrdinal:     ordinal,
		}).String()
	}
	return ev, disp, nil
}

// occurredAt prefers the backend's event time and falls back to the room's
// creation time for room_started, where createdAt is occasionally absent.
//
// It never falls back to time.Now(). occurred_at is the partition key and the
// ordering key; substituting our clock for theirs would silently file a late
// delivery under the wrong day and make arrival order look like event order —
// the exact confusion ADR-0011 warns about.
func occurredAt(p payload) time.Time {
	if p.CreatedAt > 0 {
		return time.Unix(p.CreatedAt.int64(), 0).UTC()
	}
	if p.Participant != nil && p.Participant.JoinedAt > 0 {
		return time.Unix(p.Participant.JoinedAt.int64(), 0).UTC()
	}
	if p.Room != nil && p.Room.CreationTime > 0 {
		return time.Unix(p.Room.CreationTime.int64(), 0).UTC()
	}
	return time.Time{}
}
