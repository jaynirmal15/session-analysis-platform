-- Sanity check for seed.sql. Run this every time, before trusting any timing.
--
-- The failure it exists to catch: an uncorrelated LATERAL (or any volatile
-- expression the planner is free to hoist) collapsing to a single value, so
-- every row shares one started_at. distinct_started_at came back as 1 for
-- 200,000 rows once already, and the resulting baseline survived into an ADR
-- before anyone noticed.
--
-- What to expect on healthy data:
--   distinct_pct     ~100    (every row its own timestamp)
--   open_pct         ~= the frac you passed
--   window_closed    > 0     (the window must catch SOME closed joins;
--                             zero closed rows is the exact symptom of the bug)

-- NULLIF guards the empty table: participant_join is empty until something
-- seeds it, and a diagnostic that dies with "division by zero" when asked about
-- an empty table is a diagnostic nobody trusts the second time.
SELECT
  count(*)                                                      AS rows,
  count(DISTINCT started_at)                                    AS distinct_started_at,
  round(100.0 * count(DISTINCT started_at) / nullif(count(*), 0), 2) AS distinct_pct,
  count(*) FILTER (WHERE ended_at IS NULL)                      AS open_joins,
  round(100.0 * count(*) FILTER (WHERE ended_at IS NULL) / nullif(count(*), 0), 2) AS open_pct,
  (SELECT count(*) FROM participant_join
     WHERE active_range && tstzrange(now() - interval '2 hours', now(), '[)'))
                                                                AS window_matched,
  (SELECT count(*) FROM participant_join
     WHERE active_range && tstzrange(now() - interval '2 hours', now(), '[)')
       AND ended_at IS NOT NULL)                                AS window_closed
FROM participant_join;
