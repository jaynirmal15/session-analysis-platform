-- Schedule partition maintenance. Deployment configuration, not schema.
--
-- This was migration 000005 until pg_cron's single-database restriction made
-- that untenable: CREATE EXTENSION pg_cron succeeds only in the database named
-- by the server's cron.database_name setting, so the migration could never
-- apply to CI, to a scratch database, or to any per-branch database. It failed
-- with "can only create extension in database sap" and left the version table
-- dirty. ADR-0029 records the split: 000004 creates the view and the function
-- and is schema; this file starts them running and is operations.
--
-- IDEMPOTENT. Safe to run any number of times, and re-running is the intended
-- recovery step if the schedule is ever lost. cron.schedule() keyed on a job
-- name is an upsert -- scheduling the same name again replaces that job's
-- definition rather than adding a second one -- so the job count stays at 1.
-- `make schedule-maintenance` runs this, and docker compose runs it once after
-- migrations complete.
--
-- Requires pg_cron in shared_preload_libraries; see deploy/postgres/Dockerfile.

CREATE EXTENSION IF NOT EXISTS pg_cron;

-- 02:17 UTC daily. Deliberately not midnight: partition boundaries fall on
-- midnight, so running the maintenance exactly then puts creation in
-- contention with the write path's busiest partition-routing moment for no
-- reason. The odd minute keeps it out of the way of every other cron-shaped
-- thing that defaults to :00.
SELECT cron.schedule(
    'maintain-event-raw-partitions',
    '17 2 * * *',
    $job$ SELECT maintain_event_raw_partitions() $job$
);

-- Report what is now scheduled, so a run that silently did nothing is visible
-- in the output rather than having to be queried for afterwards.
SELECT jobname, schedule, active FROM cron.job
 WHERE jobname = 'maintain-event-raw-partitions';
