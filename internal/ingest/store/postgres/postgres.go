package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/store"
	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

// Store writes events and maintains participant_join.
type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const insertEvent = `
INSERT INTO event_raw (
    event_id, backend, backend_event_id, event_type, occurred_at, received_at,
    room_name, room_sid, participant_identity, participant_sid, track_sid, payload
) VALUES ($1,$2,$3,$4,$5,now(),$6,$7,$8,$9,$10,$11)
ON CONFLICT DO NOTHING
RETURNING event_id`

const openJoin = `
INSERT INTO participant_join (
    join_id, backend, room_name, participant_identity, room_sid,
    participant_sid, started_at, started_event_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (started_event_id) DO NOTHING`

// closeJoin targets the join by participant SID where the backend supplies one.
// The SID cannot identify a person (ADR-0016) but it precisely identifies a
// join, which is what closing needs. It is scoped to open joins so a redelivery
// cannot re-close an already-closed join with a later timestamp.
const closeBySID = `
UPDATE participant_join
   SET ended_at = $1, end_reason = $2, ended_event_id = $3, updated_at = now()
 WHERE backend = $4 AND room_name = $5 AND participant_sid = $6
   AND ended_at IS NULL`

// closeByIdentity is the fallback for backends with no per-join identifier.
// It closes the most recent open join, which is a guess only in the sense that
// multiple open joins for one identity already mean an event was lost.
const closeByIdentity = `
UPDATE participant_join
   SET ended_at = $1, end_reason = $2, ended_event_id = $3, updated_at = now()
 WHERE join_id = (
     SELECT join_id FROM participant_join
      WHERE backend = $4 AND room_name = $5 AND participant_identity = $6
        AND ended_at IS NULL
      ORDER BY started_at DESC
      LIMIT 1)`

// closeRoom closes every join still open in a room. Not inference: the backend
// has stated the room is gone, and a participant cannot remain in a room that
// does not exist (ADR-0020).
const closeRoom = `
UPDATE participant_join
   SET ended_at = $1, end_reason = $2, ended_event_id = $3, updated_at = now()
 WHERE backend = $4 AND room_name = $5 AND ended_at IS NULL`

// RecordEvent stores the event and applies its join effect in one transaction.
//
// The two cannot be separated. Whether the join effect runs at all is decided
// by whether the insert conflicted, and reading that decision outside the
// transaction would let a concurrent redelivery apply the effect twice.
func (s *Store) RecordEvent(ctx context.Context, ev session.Event) (store.Result, error) {
	var res store.Result

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted uuid.UUID
	err = tx.QueryRow(ctx, insertEvent,
		ev.ID, string(ev.Backend), nullable(ev.BackendEventID), string(ev.Type),
		ev.OccurredAt, nullable(ev.RoomName), nullable(ev.RoomSID),
		nullable(ev.ParticipantIdentity), nullable(ev.ParticipantSID),
		nullable(ev.TrackSID), ev.Payload,
	).Scan(&inserted)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Conflict: this event is already recorded. Commit the empty
		// transaction and skip every join effect. This is the idempotency
		// guarantee, and it is why the effects live in here.
		if err := tx.Commit(ctx); err != nil {
			return res, fmt.Errorf("postgres: commit duplicate: %w", err)
		}
		return res, nil
	case err != nil:
		if isPartitionMissing(err) {
			return res, store.ErrPartitionMissing
		}
		return res, fmt.Errorf("postgres: insert event: %w", err)
	}
	res.Stored = true

	if err := s.applyJoinEffect(ctx, tx, ev, &res); err != nil {
		return res, err
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("postgres: commit: %w", err)
	}
	return res, nil
}

func (s *Store) applyJoinEffect(ctx context.Context, tx pgx.Tx, ev session.Event, res *store.Result) error {
	switch ev.Type {
	case session.EventParticipantJoined:
		tag, err := tx.Exec(ctx, openJoin,
			uuid.New(), string(ev.Backend), ev.RoomName, ev.ParticipantIdentity,
			nullable(ev.RoomSID), nullable(ev.ParticipantSID), ev.OccurredAt, ev.ID)
		if err != nil {
			return fmt.Errorf("postgres: open join: %w", err)
		}
		res.JoinOpened = tag.RowsAffected() > 0

	case session.EventParticipantLeft:
		n, err := s.close(ctx, tx, ev, session.EndParticipantLeft)
		if err != nil {
			return err
		}
		res.JoinsClosed = n
		// No open join to close means the opening event was never received.
		// Recorded, not repaired: inventing a join would be exactly the
		// inference ADR-0020 refuses.
		res.CloseUnmatched = n == 0

	case session.EventRoomFinished:
		tag, err := tx.Exec(ctx, closeRoom,
			ev.OccurredAt, string(session.EndRoomFinished), ev.ID,
			string(ev.Backend), ev.RoomName)
		if err != nil {
			return classifyClose(err)
		}
		res.JoinsClosed = tag.RowsAffected()
	}
	return nil
}

func (s *Store) close(ctx context.Context, tx pgx.Tx, ev session.Event, reason session.EndReason) (int64, error) {
	if ev.ParticipantSID != "" {
		tag, err := tx.Exec(ctx, closeBySID,
			ev.OccurredAt, string(reason), ev.ID,
			string(ev.Backend), ev.RoomName, ev.ParticipantSID)
		if err != nil {
			return 0, classifyClose(err)
		}
		if tag.RowsAffected() > 0 {
			return tag.RowsAffected(), nil
		}
		// Fall through: the SID did not match an open join, which can happen
		// when the opening delivery was lost. Try identity before giving up.
	}
	tag, err := tx.Exec(ctx, closeByIdentity,
		ev.OccurredAt, string(reason), ev.ID,
		string(ev.Backend), ev.RoomName, ev.ParticipantIdentity)
	if err != nil {
		return 0, classifyClose(err)
	}
	return tag.RowsAffected(), nil
}

// isPartitionMissing implements the contract documented in ADR-0024.
//
// SQLSTATE 23514 alone is not sufficient: an ordinary CHECK constraint
// violation carries the same code. The two are told apart by whether the error
// names a constraint — a CHECK violation populates ConstraintName, a partition
// miss leaves it empty. Matching on the code alone would miscount every CHECK
// violation as data loss.
func isPartitionMissing(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	return pg.Code == "23514" && pg.ConstraintName == ""
}

// classifyClose recognises a close whose ended_at precedes started_at.
//
// It surfaces as SQLSTATE 22000 from the generated active_range column, not as
// the participant_join_end_after_start CHECK — that constraint is unreachable,
// because the range constructor rejects the row first. Matching on the
// constraint name here would never fire (ADR-0024).
func classifyClose(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "22000" {
		return store.ErrCloseOutOfOrder
	}
	return fmt.Errorf("postgres: close join: %w", err)
}

// nullable maps the empty string to SQL NULL. An absent identifier and an
// identifier that is the empty string are the same fact, and storing "" would
// make "has no room SID" indexable as a value.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
