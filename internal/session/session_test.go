package session

import (
	"testing"
	"time"
)

func TestJoinIsOpen(t *testing.T) {
	end := time.Now()
	if !(Join{}).IsOpen() {
		t.Error("a join with no observed end must report open")
	}
	if (Join{EndedAt: &end, EndReason: EndParticipantLeft}).IsOpen() {
		t.Error("a join with an observed end must not report open")
	}
}

func TestSessionDerivedFields(t *testing.T) {
	t0 := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(30 * time.Second)
	s := Session{
		Gap: 30 * time.Second,
		Joins: []Join{
			{StartedAt: t0, EndedAt: &t1, EndReason: EndParticipantLeft},
			{StartedAt: t1.Add(10 * time.Second)},
		},
	}
	if !s.StartedAt().Equal(t0) {
		t.Errorf("StartedAt = %v, want %v", s.StartedAt(), t0)
	}
	if s.Reconnects() != 1 {
		t.Errorf("Reconnects = %d, want 1", s.Reconnects())
	}
	if !s.IsOpen() {
		t.Error("a session whose last join is open must report open")
	}
}

// The reserved reason must stay in the vocabulary and stay unused: ADR-0020
// says no sweeper writes it. This guards the constant against being quietly
// repurposed.
func TestInferredTimeoutIsReserved(t *testing.T) {
	if EndInferredTimeout != "inferred_timeout" {
		t.Errorf("EndInferredTimeout = %q, want %q", EndInferredTimeout, "inferred_timeout")
	}
}

func TestCorrelatedEventTypesAreDistinct(t *testing.T) {
	seen := map[EventType]bool{}
	for _, et := range CorrelatedEventTypes {
		if seen[et] {
			t.Errorf("duplicate event type %q", et)
		}
		seen[et] = true
	}
	if len(CorrelatedEventTypes) != 6 {
		t.Errorf("got %d correlated event types, want 6 (ADR-0022)", len(CorrelatedEventTypes))
	}
}
