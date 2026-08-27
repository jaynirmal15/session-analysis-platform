//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/store"
	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

// These run against a real, migrated PostgreSQL. The partition-miss classifier
// in particular cannot be tested any other way: its whole contract is about
// distinguishing two errors that PostgreSQL itself produces, and a fixture
// asserting what we believe those errors look like would only test our belief.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SAP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SAP_TEST_DATABASE_URL not set")
	}
	p, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(p.Close)
	if _, err := p.Exec(context.Background(),
		"TRUNCATE participant_join; TRUNCATE event_raw;"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return p
}

func ev(t session.EventType, room, identity, sid string, at time.Time) session.Event {
	return session.Event{
		ID: uuid.New().String(), Backend: session.BackendLiveKit,
		BackendEventID: uuid.NewString(), Type: t, OccurredAt: at,
		RoomName: room, RoomSID: "RM_1", ParticipantIdentity: identity,
		ParticipantSID: sid, Payload: []byte(`{}`),
	}
}

// The contract from ADR-0024, verified against the database rather than assumed.
func TestPartitionMissingIsClassified(t *testing.T) {
	s := New(pool(t))
	// Far outside the bootstrap window, which spans days rather than years.
	far := time.Now().AddDate(3, 0, 0).UTC()

	_, err := s.RecordEvent(context.Background(),
		ev(session.EventRoomStarted, "r", "", "", far))

	if !errors.Is(err, store.ErrPartitionMissing) {
		t.Fatalf("got %v, want ErrPartitionMissing", err)
	}
}

// The reason the classifier cannot match on SQLSTATE alone: an ordinary CHECK
// violation carries the same 23514. If this ever starts returning true, the
// receiver will report constraint bugs as data loss.
func TestCheckViolationIsNotMistakenForPartitionMiss(t *testing.T) {
	p := pool(t)
	_, err := p.Exec(context.Background(), `
		INSERT INTO participant_join
		  (join_id, backend, room_name, participant_identity, started_at,
		   ended_at, end_reason, started_event_id)
		VALUES ($1,'livekit','r','u', now(), NULL, 'participant_left', $2)`,
		uuid.New(), uuid.New())

	if err == nil {
		t.Fatal("expected the end_together CHECK to reject ended_at IS NULL with a reason set")
	}
	if isPartitionMissing(err) {
		t.Fatal("a CHECK violation was classified as a missing partition")
	}
}

func TestRedeliveryIsIdempotent(t *testing.T) {
	s := New(pool(t))
	ctx := context.Background()
	e := ev(session.EventParticipantJoined, "standup", "alice", "PA_1", time.Now().UTC())

	first, err := s.RecordEvent(ctx, e)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first.Stored || !first.JoinOpened {
		t.Fatalf("first delivery: %+v", first)
	}

	second, err := s.RecordEvent(ctx, e)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Stored {
		t.Error("redelivery was stored again")
	}
	// The point of skipping the effect: the join must not open twice.
	if second.JoinOpened {
		t.Error("redelivery opened a second join")
	}

	var joins int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM participant_join`).Scan(&joins); err != nil {
		t.Fatal(err)
	}
	if joins != 1 {
		t.Errorf("participant_join has %d rows, want 1", joins)
	}
}

func TestJoinLifecycle(t *testing.T) {
	s := New(pool(t))
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)

	if _, err := s.RecordEvent(ctx,
		ev(session.EventParticipantJoined, "standup", "alice", "PA_1", start)); err != nil {
		t.Fatalf("join: %v", err)
	}

	end := start.Add(30 * time.Minute)
	res, err := s.RecordEvent(ctx,
		ev(session.EventParticipantLeft, "standup", "alice", "PA_1", end))
	if err != nil {
		t.Fatalf("leave: %v", err)
	}
	if res.JoinsClosed != 1 {
		t.Fatalf("JoinsClosed = %d, want 1", res.JoinsClosed)
	}

	var endedAt time.Time
	var reason string
	if err := s.pool.QueryRow(ctx,
		`SELECT ended_at, end_reason FROM participant_join`).Scan(&endedAt, &reason); err != nil {
		t.Fatal(err)
	}
	// ended_at comes from the observed event, never from our clock.
	if !endedAt.UTC().Truncate(time.Second).Equal(end.Truncate(time.Second)) {
		t.Errorf("ended_at = %v, want the event's time %v", endedAt, end)
	}
	if reason != string(session.EndParticipantLeft) {
		t.Errorf("end_reason = %q", reason)
	}
}

// ADR-0020's exception: the backend states the room is gone, so every join
// still open in it is closed — with a reason distinct from "left", because
// "closed because the room ended" says nothing about the participant.
func TestRoomFinishedClosesEveryOpenJoin(t *testing.T) {
	s := New(pool(t))
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)

	for i, who := range []string{"alice", "bob", "carol"} {
		e := ev(session.EventParticipantJoined, "standup", who, "PA_"+who, start)
		_ = i
		if _, err := s.RecordEvent(ctx, e); err != nil {
			t.Fatalf("join %s: %v", who, err)
		}
	}
	// bob leaves normally first; his reason must survive the room ending.
	if _, err := s.RecordEvent(ctx,
		ev(session.EventParticipantLeft, "standup", "bob", "PA_bob", start.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}

	res, err := s.RecordEvent(ctx,
		ev(session.EventRoomFinished, "standup", "", "", start.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("room_finished: %v", err)
	}
	if res.JoinsClosed != 2 {
		t.Fatalf("JoinsClosed = %d, want 2 (bob had already left)", res.JoinsClosed)
	}

	var left, finished int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE end_reason = 'participant_left'),
		       count(*) FILTER (WHERE end_reason = 'room_finished')
		FROM participant_join`).Scan(&left, &finished); err != nil {
		t.Fatal(err)
	}
	if left != 1 || finished != 2 {
		t.Errorf("end_reason split = %d left / %d finished, want 1 / 2", left, finished)
	}
}

// The opening event was lost. Recorded, not repaired: no synthetic join.
func TestCloseWithNoOpenJoinIsCounted(t *testing.T) {
	s := New(pool(t))
	ctx := context.Background()

	res, err := s.RecordEvent(ctx,
		ev(session.EventParticipantLeft, "standup", "ghost", "PA_ghost", time.Now().UTC()))
	if err != nil {
		t.Fatalf("leave: %v", err)
	}
	if !res.CloseUnmatched || res.JoinsClosed != 0 {
		t.Fatalf("got %+v, want CloseUnmatched with nothing closed", res)
	}

	var joins int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM participant_join`).Scan(&joins); err != nil {
		t.Fatal(err)
	}
	if joins != 0 {
		t.Errorf("a synthetic join was invented: %d rows", joins)
	}
}

// Clock skew. The generated active_range column rejects the row with SQLSTATE
// 22000 before the (unreachable) CHECK constraint is consulted. The join is
// left open rather than corrected to a time nobody observed.
func TestOutOfOrderCloseIsRefused(t *testing.T) {
	s := New(pool(t))
	ctx := context.Background()
	start := time.Now().UTC()

	if _, err := s.RecordEvent(ctx,
		ev(session.EventParticipantJoined, "standup", "alice", "PA_1", start)); err != nil {
		t.Fatal(err)
	}
	_, err := s.RecordEvent(ctx,
		ev(session.EventParticipantLeft, "standup", "alice", "PA_1", start.Add(-time.Hour)))

	if !errors.Is(err, store.ErrCloseOutOfOrder) {
		t.Fatalf("got %v, want ErrCloseOutOfOrder", err)
	}

	var open bool
	if err := s.pool.QueryRow(ctx,
		`SELECT ended_at IS NULL FROM participant_join`).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if !open {
		t.Error("the join was closed with an unobserved timestamp")
	}
}
