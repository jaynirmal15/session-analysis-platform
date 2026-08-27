package eventid

import (
	"testing"
	"time"

	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

func base() Fields {
	return Fields{
		Backend:             session.BackendLiveKit,
		EventType:           session.EventTrackPublished,
		Room:                "standup",
		ParticipantIdentity: "alice",
		ParticipantSID:      "PA_aaa",
		TrackSID:            "TR_video",
		OccurredAt:          time.Date(2026, 8, 27, 10, 0, 0, 123456789, time.UTC),
		DeliveryOrdinal:     0,
	}
}

func TestDerivationIsDeterministic(t *testing.T) {
	if FromFields(base()) != FromFields(base()) {
		t.Fatal("same inputs must derive the same id, or redelivery is not idempotent")
	}
	a := FromBackendEventID(session.BackendLiveKit, "EV_123")
	if a != FromBackendEventID(session.BackendLiveKit, "EV_123") {
		t.Fatal("backend-id derivation must be deterministic")
	}
}

// The concrete bug ADR-0024's earlier wording left open: a participant
// publishing audio and video in the same millisecond emits two track_published
// events differing only in track. Without TrackSID they collapse into one row.
func TestSimultaneousTracksDoNotCollapse(t *testing.T) {
	video := base()
	audio := base()
	audio.TrackSID = "TR_audio"

	if FromFields(video) == FromFields(audio) {
		t.Fatal("two tracks published in the same millisecond must not derive the same id")
	}
}

// Length prefixing, not delimiting: with a naive separator ("a","bc") and
// ("ab","c") produce identical input. No delimiter choice fixes this, because
// any delimiter can occur inside a caller-supplied room name or identity.
func TestFieldBoundariesCannotBeForged(t *testing.T) {
	x := base()
	x.Room = "a"
	x.ParticipantIdentity = "bc"

	y := base()
	y.Room = "ab"
	y.ParticipantIdentity = "c"

	if FromFields(x) == FromFields(y) {
		t.Fatal(`("a","bc") and ("ab","c") derived the same id: field boundaries are forgeable`)
	}
}

func TestEveryFieldAffectsIdentity(t *testing.T) {
	cases := map[string]func(*Fields){
		"backend":     func(f *Fields) { f.Backend = session.BackendMediasoup },
		"event type":  func(f *Fields) { f.EventType = session.EventTrackUnpublished },
		"room":        func(f *Fields) { f.Room = "other" },
		"identity":    func(f *Fields) { f.ParticipantIdentity = "bob" },
		"participant": func(f *Fields) { f.ParticipantSID = "PA_bbb" },
		"track":       func(f *Fields) { f.TrackSID = "TR_other" },
		"ordinal":     func(f *Fields) { f.DeliveryOrdinal = 1 },
		"nanoseconds": func(f *Fields) { f.OccurredAt = f.OccurredAt.Add(time.Nanosecond) },
	}
	want := FromFields(base())
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := base()
			mutate(&f)
			if FromFields(f) == want {
				t.Errorf("changing %s did not change the derived id", name)
			}
		})
	}
}

// Nanosecond precision is load-bearing: a busy room produces many events per
// second, and truncating would collapse them.
func TestSubSecondEventsAreDistinct(t *testing.T) {
	a := base()
	b := base()
	b.OccurredAt = a.OccurredAt.Add(time.Millisecond)
	if FromFields(a) == FromFields(b) {
		t.Fatal("events 1ms apart must derive different ids")
	}
}

// Timezone must not affect identity: the same instant expressed in two zones is
// one event, and a redelivery normalised differently must not split.
func TestTimezoneDoesNotSplitIdentity(t *testing.T) {
	utc := base()
	other := base()
	other.OccurredAt = utc.OccurredAt.In(time.FixedZone("IST", 5*3600+1800))
	if FromFields(utc) != FromFields(other) {
		t.Fatal("the same instant in a different zone must derive the same id")
	}
}

func TestBackendIsNamespacedSeparately(t *testing.T) {
	if FromBackendEventID(session.BackendLiveKit, "EV_1") ==
		FromBackendEventID(session.BackendMediasoup, "EV_1") {
		t.Fatal("the same event id from two backends must not collide")
	}
}
