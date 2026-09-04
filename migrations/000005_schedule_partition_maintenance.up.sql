-- Schedule the maintenance function inside the database that needs it.
--
-- Separate from 000004 because this one has a hard dependency: pg_cron must be
-- in shared_preload_libraries, which is a server setting, not something a
-- migration can arrange. The mechanism (000004) works on any PostgreSQL; only
-- the schedule requires this.
--
-- ADR-0029 records why the schedule lives here rather than in Kubernetes or in
-- the ingester: partition creation is a property of the database, and a
-- schedule that lives anywhere else stops running the moment someone starts
-- this database without that other thing — including locally, during the
-- benchmark runs the ceiling work depends on.

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
