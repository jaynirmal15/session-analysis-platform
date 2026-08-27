-- Synthetic participant_join data for the reference-query benchmark.
--
-- Parameters (psql -v):
--   rows  : number of joins to generate
--   frac  : fraction left open, i.e. ended_at IS NULL
--   span  : days over which started_at is spread
--
-- DESTRUCTIVE: truncates participant_join.
--
-- ---------------------------------------------------------------------------
-- Read this before changing the randomness. It is the reason this file exists.
-- ---------------------------------------------------------------------------
--
-- The first version of this seed, which produced the original (now discarded)
-- 66 ms baseline in ADR-0024, was written like this:
--
--     FROM generate_series(1, 200000) AS i,
--     LATERAL (SELECT now() - (random() * 14) * interval '1 day' AS st) AS s
--
-- That looks like it generates one random timestamp per row. It does not.
--
-- The LATERAL subquery never references `i`, so it is not actually correlated
-- with the outer relation. PostgreSQL is free to evaluate it ONCE and reuse the
-- single result for every row -- and it does. All 200,000 rows received an
-- identical started_at.
--
-- The consequences were not subtle, and they were not obvious either:
--
--   * Every join in the table started at the same instant, so the data had no
--     time distribution at all.
--   * The 2-hour reference window matched exactly the open joins and NO closed
--     joins, because the single shared timestamp fell outside the window.
--   * The resulting timings measured GiST over 200,000 identical ranges, which
--     is not the workload the platform has.
--
-- Every number derived from that run was wrong and has been discarded. See
-- "Measurement history" in ADR-0024, and benchmarks/README.md for the general
-- lesson about numbers that look too tidy.
--
-- The fix is to put the volatile expressions in the select list of a plain
-- subquery over generate_series, where they are evaluated per row. Always run
-- verify_seed.sql afterwards -- it is two seconds and it catches exactly this.

TRUNCATE participant_join;

INSERT INTO participant_join
  (join_id, backend, room_name, participant_identity, participant_sid,
   started_at, ended_at, end_reason, started_event_id)
SELECT
  gen_random_uuid(),
  'livekit',
  'room-' || (g.i % 4000),
  'user-' || (g.i % 25000),
  'PA_' || g.i,
  g.st,
  CASE WHEN g.op THEN NULL ELSE g.st + g.dur * interval '1 second' END,
  CASE WHEN g.op THEN NULL ELSE 'participant_left' END,
  gen_random_uuid()
FROM (
  -- random() here is evaluated once per row, because it sits in the select
  -- list of a subquery over generate_series. This is the whole fix.
  SELECT i,
         now() - (random() * :span) * interval '1 day' AS st,
         random() < :frac                              AS op,
         random() * 3600                               AS dur
  FROM generate_series(1, :rows) AS i
) g;

ANALYZE participant_join;
