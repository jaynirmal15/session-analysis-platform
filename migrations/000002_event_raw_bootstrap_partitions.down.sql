-- Drop every partition of event_raw, leaving the empty parent behind.
DO $$
DECLARE
    part record;
BEGIN
    FOR part IN
        SELECT c.relname
        FROM pg_inherits i
        JOIN pg_class c   ON c.oid = i.inhrelid
        JOIN pg_class p   ON p.oid = i.inhparent
        WHERE p.relname = 'event_raw'
    LOOP
        EXECUTE format('DROP TABLE IF EXISTS %I', part.relname);
    END LOOP;
END $$;
