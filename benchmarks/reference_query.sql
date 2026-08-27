-- The reference query: the shape ADR-0004's revisit trigger is stated in terms
-- of, and the thing the platform's read path is fundamentally for.
--
-- "Group a participant's joins into sessions, over a bounded time window,
--  under a reconnect-gap threshold supplied by the caller."
--
-- Three properties of it are decisions, not incidentals:
--
--   * The gap threshold is a PARAMETER. Sessions are derived at read time;
--     joins are what is stored (ADR-0019). Grouping the same joins at 30s and
--     at 120s gives two correct and different answers.
--   * The overlap uses active_range && tstzrange(...), not a hand-written
--     started_at/ended_at predicate. An open join is [started_at, ) and
--     overlaps any later window with no NULL branch to forget (ADR-0024).
--   * still_open is bool_or(ended_at IS NULL), not max(ended_at). A session
--     whose last join never ended is open, and max() would quietly report the
--     previous join's end as the session's end (ADR-0020).
--
-- Parameters: :window_hours, :gap_seconds

WITH overlapping AS (
    SELECT backend, room_name, participant_identity, started_at, ended_at,
           started_at - lag(ended_at) OVER w AS gap
    FROM participant_join
    WHERE active_range && tstzrange(
              now() - (:window_hours * interval '1 hour'), now(), '[)')
    WINDOW w AS (PARTITION BY backend, room_name, participant_identity
                 ORDER BY started_at)
), marked AS (
    SELECT *,
           (gap IS NULL OR gap > (:gap_seconds * interval '1 second'))::int AS is_new
    FROM overlapping
), grouped AS (
    SELECT *,
           sum(is_new) OVER (PARTITION BY backend, room_name, participant_identity
                             ORDER BY started_at
                             ROWS UNBOUNDED PRECEDING) AS session_ordinal
    FROM marked
)
SELECT backend, room_name, participant_identity, session_ordinal,
       min(started_at)              AS session_started_at,
       bool_or(ended_at IS NULL)    AS still_open,
       count(*)                     AS joins,
       count(*) - 1                 AS reconnects
FROM grouped
GROUP BY 1, 2, 3, 4;
