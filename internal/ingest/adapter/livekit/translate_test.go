package livekit

import (
	"testing"
	"time"

	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

func TestClassifyMatchesADR0022(t *testing.T) {
	ingest := []string{
		"room_started", "room_finished", "participant_joined",
		"participant_left", "track_published", "track_unpublished",
	}
	for _, name := range ingest {
		if got := Classify(name); got != Ingest {
			t.Errorf("%s: got %v, want Ingest", name, got)
		}
	}

	// Recording and media injection: operations on a room, not experiences of
	// a participant. Refused at the boundary so they cannot distort the volume
	// measurement ADR-0004 depends on.
	for _, name := range []string{
		"egress_started", "egress_updated", "egress_ended",
		"ingress_started", "ingress_ended",
	} {
		if got := Classify(name); got != Reject {
			t.Errorf("%s: got %v, want Reject", name, got)
		}
	}

	// Unknown is a surprise, not a decision: stored so the payload survives to
	// be looked at.
	if got := Classify("some_future_livekit_event"); got != Store {
		t.Errorf("unknown type: got %v, want Store", got)
	}
}

func TestTranslateParticipantJoined(t *testing.T) {
	body := []byte(`{
      "event":"participant_joined","id":"EV_1","createdAt":1787000000,
      "room":{"sid":"RM_1","name":"standup"},
      "participant":{"sid":"PA_1","identity":"alice"}}`)

	ev, disp, err := Translate(body, 0)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if disp != Ingest {
		t.Fatalf("disposition = %v, want Ingest", disp)
	}
	if ev.Type != session.EventParticipantJoined {
		t.Errorf("Type = %q", ev.Type)
	}
	if ev.RoomName != "standup" || ev.ParticipantIdentity != "alice" || ev.ParticipantSID != "PA_1" {
		t.Errorf("correlation keys wrong: %+v", ev)
	}
	if !ev.OccurredAt.Equal(time.Unix(1787000000, 0).UTC()) {
		t.Errorf("OccurredAt = %v", ev.OccurredAt)
	}
	if string(ev.Payload) != string(body) {
		t.Error("payload must be retained verbatim")
	}
}

// Redelivery is byte-identical, so identity must be stable across calls.
func TestTranslateIsIdempotentOnIdentity(t *testing.T) {
	body := []byte(`{"event":"room_started","id":"EV_9","createdAt":1787000000,
	                 "room":{"sid":"RM_9","name":"r"}}`)
	a, _, _ := Translate(body, 0)
	b, _, _ := Translate(body, 0)
	if a.ID != b.ID {
		t.Fatal("the same delivery derived two ids: redelivery would not be idempotent")
	}
}

// An unrecognised type keeps LiveKit's own name rather than being coerced into
// a canonical one, so nothing invents a value queries will group by.
func TestTranslateKeepsUnknownTypeName(t *testing.T) {
	body := []byte(`{"event":"quantum_event","id":"EV_X","createdAt":1787000000}`)
	ev, disp, err := Translate(body, 0)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if disp != Store {
		t.Fatalf("disposition = %v, want Store", disp)
	}
	if string(ev.Type) != "quantum_event" {
		t.Errorf("Type = %q, want the backend's own name", ev.Type)
	}
	if len(ev.Payload) == 0 {
		t.Error("an unrecognised event must keep its payload — that is the entire reason to store it")
	}
}

func TestTranslateRejectedCarriesTypeForCounting(t *testing.T) {
	body := []byte(`{"event":"egress_started","id":"EV_E"}`)
	ev, disp, err := Translate(body, 0)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if disp != Reject {
		t.Fatalf("disposition = %v, want Reject", disp)
	}
	if string(ev.Type) != "egress_started" {
		t.Errorf("rejected event must carry its type for the metric label, got %q", ev.Type)
	}
}

// occurred_at is the partition key and the ordering key. Substituting our clock
// would file a late delivery under the wrong day.
func TestTranslateNeverSubstitutesOurClock(t *testing.T) {
	body := []byte(`{"event":"room_started","id":"EV_2"}`)
	ev, _, err := Translate(body, 0)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !ev.OccurredAt.IsZero() {
		t.Errorf("OccurredAt = %v, want zero when the backend supplied no time", ev.OccurredAt)
	}
}

func TestTranslateRejectsGarbage(t *testing.T) {
	if _, _, err := Translate([]byte(`not json`), 0); err == nil {
		t.Error("malformed body accepted")
	}
	if _, _, err := Translate([]byte(`{"id":"EV_1"}`), 0); err == nil {
		t.Error("body with no event type accepted")
	}
}

// The shape LiveKit actually sends. Protobuf's canonical JSON mapping encodes
// int64 and uint64 as strings, because a JSON number cannot carry the full
// 64-bit range. Every fixture above uses numbers, and every one of them passed
// while the receiver rejected 100% of real deliveries — which is precisely the
// failure a test written from documentation cannot catch.
func TestTranslateAcceptsProtobufJSONIntegers(t *testing.T) {
	body := []byte(`{
      "event":"participant_joined","id":"EV_real","createdAt":"1787791385",
      "room":{"sid":"RM_1","name":"standup","creationTime":"1787791380"},
      "participant":{"sid":"PA_1","identity":"alice","joinedAt":"1787791385"}}`)

	ev, disp, err := Translate(body, 0)
	if err != nil {
		t.Fatalf("real LiveKit payload rejected: %v", err)
	}
	if disp != Ingest {
		t.Fatalf("disposition = %v", disp)
	}
	if !ev.OccurredAt.Equal(time.Unix(1787791385, 0).UTC()) {
		t.Errorf("OccurredAt = %v, want the string-encoded createdAt to be parsed", ev.OccurredAt)
	}
}

// Both encodings must work: the mapping is not uniformly applied across LiveKit
// versions and fields, and accepting both costs nothing.
func TestProtoInt64AcceptsBothEncodings(t *testing.T) {
	cases := map[string]string{
		"string": `{"event":"room_started","id":"E","createdAt":"1787791385"}`,
		"number": `{"event":"room_started","id":"E","createdAt":1787791385}`,
		"null":   `{"event":"room_started","id":"E","createdAt":null}`,
		"absent": `{"event":"room_started","id":"E"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			ev, _, err := Translate([]byte(body), 0)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			want := time.Unix(1787791385, 0).UTC()
			if name == "null" || name == "absent" {
				if !ev.OccurredAt.IsZero() {
					t.Errorf("OccurredAt = %v, want zero", ev.OccurredAt)
				}
				return
			}
			if !ev.OccurredAt.Equal(want) {
				t.Errorf("OccurredAt = %v, want %v", ev.OccurredAt, want)
			}
		})
	}
}
