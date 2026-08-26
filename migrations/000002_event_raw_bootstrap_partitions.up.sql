-- Bootstrap partitions so a freshly migrated database can accept writes.
--
-- Daily granularity (ADR-0024): retention is measured in weeks, so daily lets
-- retention be expressed to the day and keeps partition count around 60 at an
-- 8-week window. Weekly would carry up to six extra days past the retention
-- boundary and prune far more coarsely for the drill-down query, which is
-- usually scoped to hours.
--
-- This migration is a BOOTSTRAP, not the partition maintenance mechanism.
-- Partitions must exist ahead of time or inserts fail (ADR-0004, accepted
-- knowingly). Recurring creation is a scheduled job, and deliberately NOT a
-- migration -- see ADR-0023 on why conflating maintenance with schema change
-- is the mistake this tool choice is meant to prevent.
--
-- TODO(scope): the recurring partition maintenance job. Until it exists, this
-- window is finite and inserts past its end WILL fail. That is intended: there
-- is no DEFAULT partition, because a default partition silently absorbs
-- misrouted rows and forces a full scan of itself on every later ATTACH.
-- Failing loudly is the same call ADR-0016 makes about unstable identity.

DO $$
DECLARE
    start_day date := (now() AT TIME ZONE 'UTC')::date - 7;
    end_day   date := (now() AT TIME ZONE 'UTC')::date + 14;
    day       date;
BEGIN
    day := start_day;
    WHILE day < end_day LOOP
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF event_raw '
            'FOR VALUES FROM (%L) TO (%L)',
            'event_raw_' || to_char(day, 'YYYYMMDD'),
            day::timestamptz,
            (day + 1)::timestamptz
        );
        day := day + 1;
    END LOOP;
END $$;
