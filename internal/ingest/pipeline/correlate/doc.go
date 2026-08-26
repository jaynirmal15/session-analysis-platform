// Package correlate derives session timelines from the raw event stream.
//
// This is the stage that turns "a participant joined room X at T" into "this
// session lasted N seconds, involved these participants, and had these track
// transitions" — the record that per-session drill-down actually reads.
//
// The problems this stage exists to solve, all of which are real and none of
// which are solved yet:
//
//   - Sessions have no natural end. A room that stops emitting events has not
//     necessarily ended; it may have been partitioned away from the backend.
//     Some timeout closes the session, and that timeout is a lie of a known
//     size.
//   - Events arrive out of order and late. A correlated timeline is therefore
//     never final, only settled, and the settling window has to be explicit.
//   - Duplicate deliveries must be idempotent. Correlating the same event
//     twice must not produce a session that lasted twice as long.
//   - The work is a self-join over a time-partitioned table, which is exactly
//     the query shape plain PostgreSQL handles worst. That is intentional; the
//     ceiling is the subject of a later article (ADR-0004).
//
// Two of the four problems above are now settled by decision rather than by
// code. Sessions are not stitched here at all — the join is what this stage
// produces, and grouping joins into sessions is read-time policy (ADR-0019).
// And nothing here closes a join by timeout: ended_at is written only from an
// observed event (ADR-0020).
//
// TODO(scope): correlation logic. The schema it writes into exists (ADR-0024);
// the stage that fills it does not.
package correlate
