package store

import (
	"context"
	"errors"

	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

// ErrPartitionMissing reports that no partition covers the event's occurred_at.
//
// This is not an incidental storage error. occurred_at is the backend's clock
// and there is no DEFAULT partition (ADR-0024), so a late or clock-skewed event
// has nowhere to land. The receiver counts it and acknowledges rather than
// retrying, because a retry cannot succeed for the usual cause and would turn
// one lost event into a retry storm during an incident.
var ErrPartitionMissing = errors.New("store: no partition covers occurred_at")

// ErrCloseOutOfOrder reports that a close would have set ended_at before
// started_at. The join is left open rather than corrected: clamping the
// timestamp would be inference, and ADR-0020 does not permit inferred ends.
var ErrCloseOutOfOrder = errors.New("store: ended_at precedes started_at")

// Result describes what a single delivery actually did, so the caller can
// measure it. Every field here corresponds to something the platform decided to
// observe rather than repair.
type Result struct {
	// Stored is false when the insert conflicted, i.e. this delivery was a
	// redelivery of an event already recorded. Join effects are skipped in that
	// case — applying them twice is exactly what idempotency has to prevent.
	Stored bool

	// JoinOpened is true when the event opened a join.
	JoinOpened bool

	// JoinsClosed counts joins this event ended. Greater than one only for
	// room_finished, which closes every join still open in the room.
	JoinsClosed int64

	// CloseUnmatched is true when a close event found no open join, meaning
	// the opening event was never received. No synthetic join is created.
	CloseUnmatched bool
}

// Writer is the write path's persistence port.
//
// It is deliberately narrow: one method, because the receiver does exactly one
// thing per delivery. It exists as an interface because it has two real
// implementations — PostgreSQL and the receiver's test double — not because a
// third might appear one day.
type Writer interface {
	// RecordEvent durably stores the event and applies its effect on
	// participant_join, atomically. The two must share a transaction: the
	// insert's conflict is what decides whether the join effect runs at all.
	RecordEvent(ctx context.Context, ev session.Event) (Result, error)
}
