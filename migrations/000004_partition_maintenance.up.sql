-- Partition maintenance: the obligation ADR-0024 created and ADR-0023
-- deliberately refused to solve as a migration.
--
-- This file provides the *mechanism*. Scheduling it is migration 000005, and
-- the two are separate on purpose: the function must exist on every database,
-- including ones with no pg_cron, so that tests and manual runs work anywhere.

-- Partition bounds are not exposed as structured catalog columns. pg_get_expr
-- returns them as text — "FOR VALUES FROM ('...') TO ('...')" — and parsing
-- that is the only route there is.
--
-- The parse lives in a view rather than in an exporter's configuration so it is
-- versioned here and asserted by the integration tests. A regex in a YAML file
-- that nobody tests is exactly the unverified artifact ADR-0028 is about, and
-- this one is load-bearing: it is the input to the metric that guards against
-- the schema silently running out of runway.
CREATE VIEW event_raw_partition AS
SELECT
    c.relname AS partition_name,
    (regexp_match(pg_get_expr(c.relpartbound, c.oid), $re$FROM \('([^']+)'\)$re$))[1]::timestamptz AS range_start,
    (regexp_match(pg_get_expr(c.relpartbound, c.oid), $re$TO \('([^']+)'\)$re$))[1]::timestamptz   AS range_end
FROM pg_inherits i
JOIN pg_class c ON c.oid = i.inhrelid
JOIN pg_class p ON p.oid = i.inhparent
WHERE p.relname = 'event_raw'
  AND c.relispartition;

COMMENT ON VIEW event_raw_partition IS
    'Parsed partition bounds for event_raw. Input to the runway metric; see ADR-0029.';

-- maintain_event_raw_partitions extends the schema forward and drops what has
-- aged out. One function for both, because they are two ends of the same
-- window and letting them drift apart is how you get a database that is
-- simultaneously out of runway and full of data nobody wants.
--
-- Idempotent: safe to run repeatedly, and running it more often than daily is
-- harmless. That matters because the recovery procedure for a lapsed schedule
-- is "run it again", and a recovery procedure that is dangerous to repeat is
-- not one anyone will use under pressure.
CREATE FUNCTION maintain_event_raw_partitions(
    ahead_days  int DEFAULT 14,
    retain_days int DEFAULT 56
) RETURNS TABLE (created int, dropped int)
LANGUAGE plpgsql AS $fn$
DECLARE
    today     date := (now() AT TIME ZONE 'UTC')::date;
    day       date;
    part      record;
    n_created int := 0;
    n_dropped int := 0;
BEGIN
    IF ahead_days < 1 THEN
        RAISE EXCEPTION 'ahead_days must be at least 1, got %', ahead_days;
    END IF;
    -- Guard against a caller wiping live data with a small retain_days. The
    -- forward window and the retention window must not overlap.
    IF retain_days < ahead_days THEN
        RAISE EXCEPTION 'retain_days (%) must be >= ahead_days (%)', retain_days, ahead_days;
    END IF;

    -- Extend forward. Starts at today, not at the furthest existing boundary,
    -- so a gap left by a previously failed run is filled rather than skipped.
    day := today;
    WHILE day < today + ahead_days LOOP
        IF NOT EXISTS (
            SELECT 1 FROM event_raw_partition
             WHERE range_start = day::timestamptz
        ) THEN
            EXECUTE format(
                'CREATE TABLE IF NOT EXISTS %I PARTITION OF event_raw FOR VALUES FROM (%L) TO (%L)',
                'event_raw_' || to_char(day, 'YYYYMMDD'),
                day::timestamptz,
                (day + 1)::timestamptz);
            n_created := n_created + 1;
        END IF;
        day := day + 1;
    END LOOP;

    -- Retire what has aged out. DROP on the partition detaches it implicitly;
    -- a separate DETACH first would take the parent lock twice for no gain
    -- here, since nothing archives the data on its way out.
    FOR part IN
        SELECT partition_name FROM event_raw_partition
         WHERE range_end <= (today - retain_days)::timestamptz
    LOOP
        EXECUTE format('DROP TABLE IF EXISTS %I', part.partition_name);
        n_dropped := n_dropped + 1;
    END LOOP;

    RETURN QUERY SELECT n_created, n_dropped;
END;
$fn$;

COMMENT ON FUNCTION maintain_event_raw_partitions IS
    'Extends event_raw forward and drops aged-out partitions. Idempotent. See ADR-0029.';
