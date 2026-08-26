-- The durable unit (ADR-0019).
--
-- A "session" -- a participant in a room, spanning reconnects -- is NOT stored
-- here or anywhere. Joins are stored with their gaps intact, and session
-- grouping is derived at read time with the reconnect threshold as a query
-- parameter. Applying the threshold at write time would bake a policy value
-- into stored rows and destroy the ability to ask the question differently
-- later, which is the same class of error ADR-0004 is guarding against.
--
-- Deliberately NOT partitioned. Partitioning by started_at would defeat
-- pruning on the primary query: a join that started long ago and is still open
-- must be found by any later time window, so an overlap search could never
-- prune below "every partition up to the range end". The two tables get
-- different treatment for this reason, and only this reason.

CREATE TABLE participant_join (
    join_id              uuid        PRIMARY KEY,

    backend              text        NOT NULL,
    room_name            text        NOT NULL,

    -- The integration contract (ADR-0016): stable for a given user within a
    -- room, never reused across different users. LiveKit's participant SID is
    -- new on every join and cannot serve this purpose.
    participant_identity text        NOT NULL,

    room_sid             text,
    -- Per-join, so it changes across reconnects. Retained purely for
    -- traceability back to the backend, never used for correlation.
    participant_sid      text,

    started_at           timestamptz NOT NULL,

    -- Nullable, and only ever set from an observed event (ADR-0020). NULL
    -- means "still open, or we never found out" -- an honest state, not an
    -- error. No sweeper closes these by timeout, because open-and-stale is a
    -- direct measurement of delivery-path loss and belongs on a dashboard.
    ended_at             timestamptz,

    -- How the end was learned, not merely that it ended. 'room_finished'
    -- carries different information than 'participant_left'.
    -- 'inferred_timeout' is reserved and currently never written.
    end_reason           text,

    -- Provenance back into event_raw, and the idempotency anchor. Keying
    -- idempotency on the source event rather than on (room, identity) is
    -- deliberate: a partial unique index over open joins would turn a lost
    -- participant_left into an insert failure on the next legitimate join,
    -- converting ADR-0020's measurement into an outage. Multiple open joins
    -- for one (room, identity) are allowed, and counted as a metric.
    started_event_id     uuid        NOT NULL UNIQUE,
    ended_event_id       uuid,

    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    -- An open join becomes [started_at, ) -- unbounded above -- so it overlaps
    -- any later window with no NULL special-casing at the call site.
    active_range         tstzrange
        GENERATED ALWAYS AS (tstzrange(started_at, ended_at, '[)')) STORED,

    CONSTRAINT participant_join_end_together CHECK (
        (ended_at IS NULL AND end_reason IS NULL)
        OR (ended_at IS NOT NULL AND end_reason IS NOT NULL)
    ),
    CONSTRAINT participant_join_end_after_start CHECK (
        ended_at IS NULL OR ended_at >= started_at
    ),
    -- text + CHECK rather than a PostgreSQL enum type: enums are awkward to
    -- alter inside a migration, and this list will grow when mediasoup lands.
    CONSTRAINT participant_join_end_reason_known CHECK (
        end_reason IS NULL
        OR end_reason IN ('participant_left', 'room_finished', 'inferred_timeout')
    )
);

COMMENT ON TABLE participant_join IS
    'The durable unit. Sessions are derived from these at read time. See ADR-0019.';
COMMENT ON COLUMN participant_join.ended_at IS
    'NULL means still open OR never observed. Never set by a sweeper. See ADR-0020.';
COMMENT ON COLUMN participant_join.participant_identity IS
    'Caller-supplied, must be stable within a room. See ADR-0016.';
COMMENT ON COLUMN participant_join.active_range IS
    'Generated. Open joins are unbounded above, so overlap needs no NULL handling.';

-- The overlap filter for the primary query.
CREATE INDEX participant_join_active_range_idx
    ON participant_join USING gist (active_range);

-- Supplies the primary query's window frame -- PARTITION BY (backend,
-- room_name, participant_identity) ORDER BY started_at -- in index order, so
-- the lag() that computes reconnect gaps needs no sort.
CREATE INDEX participant_join_window_idx
    ON participant_join (backend, room_name, participant_identity, started_at);

-- Open joins only. Small by construction, and the source for the
-- open-and-stale panel that measures ingest reliability (ADR-0020).
CREATE INDEX participant_join_open_idx
    ON participant_join (started_at)
    WHERE ended_at IS NULL;
