-- Raw webhook intake. Append-only, partitioned by event time.
--
-- ADR-0024 records the shape; ADR-0004 records why this is plain PostgreSQL
-- with native declarative partitioning rather than TimescaleDB.
--
-- Typed columns hold exactly what correlation keys on. Everything else stays
-- in `payload`, because mediasoup's events will not share LiveKit's shape
-- (ADR-0003) and a column set fitted to LiveKit would make LiveKit's model the
-- canonical model.

CREATE TABLE event_raw (
    -- Derived, never copied: uuidv5 over (backend, backend_event_id), or over
    -- the canonical tuple for backends that do not supply an event id.
    -- Deterministic derivation is what makes duplicate delivery idempotent.
    event_id             uuid        NOT NULL,

    backend              text        NOT NULL,
    backend_event_id     text,
    event_type           text        NOT NULL,

    -- Partition key. The backend's own clock, not ours: arrival order is not
    -- event order (ADR-0011), so nothing may depend on received_at for
    -- ordering.
    occurred_at          timestamptz NOT NULL,
    received_at          timestamptz NOT NULL DEFAULT now(),

    -- Correlation keys. (room_name, participant_identity) is the pair the
    -- write path joins on; see ADR-0016 for the stability contract the
    -- integrator must uphold and ADR-0017 for why identity is scoped per room.
    room_name            text,
    room_sid             text,
    participant_identity text,
    participant_sid      text,
    track_sid            text,

    payload              jsonb       NOT NULL,

    -- A partitioned table's unique constraints must include the partition key.
    -- This PK is also the idempotency mechanism: a duplicate delivery derives
    -- the same event_id and conflicts, so the receiver can ON CONFLICT DO
    -- NOTHING rather than maintaining a separate dedup table.
    PRIMARY KEY (occurred_at, event_id)
) PARTITION BY RANGE (occurred_at);

COMMENT ON TABLE event_raw IS
    'Append-only webhook intake, partitioned daily by occurred_at. See ADR-0024.';
COMMENT ON COLUMN event_raw.event_id IS
    'Deterministically derived (uuidv5), never copied from the backend. Idempotency anchor.';
COMMENT ON COLUMN event_raw.occurred_at IS
    'Partition key. Backend-supplied event time, not arrival time.';
COMMENT ON COLUMN event_raw.payload IS
    'Full webhook body. Retained so a backend we have not modelled yet loses nothing.';

-- Per-session drill-down: every event for one participant in one room, within
-- a bounded range. Deliberately the only secondary index on this table --
-- event_raw is the append path ADR-0011 requires to stay fast, and every index
-- is paid on every insert.
CREATE INDEX event_raw_participant_idx
    ON event_raw (room_name, participant_identity, occurred_at);
