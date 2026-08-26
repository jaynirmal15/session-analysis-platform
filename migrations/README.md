# Migrations

Empty on purpose.

The event schema is being designed separately and is deliberately not committed
yet. See [ADR-0014](../ARCHITECTURE.md#adr-0014--event-schema-and-migrations-deliberately-deferred)
for the reasoning: a plausible-looking starter schema stops being provisional
within a day, because correlation code, adapter translation and Grafana panels
all start depending on it immediately.

`docker compose up` therefore brings up an empty PostgreSQL database. That is
the expected state.

## TODO(scope)

When the schema lands, this directory gets:

- The migration tool choice, recorded as its own ADR. Not yet decided.
- Base DDL for the canonical event table, partitioned by event time
  ([ADR-0004](../ARCHITECTURE.md#adr-0004--plain-partitioned-postgresql-not-timescaledb)).
- Partition management: partitions must be created **ahead of time** or inserts
  fail, so this is a scheduled job, not a one-off migration.
- Retention via `DETACH PARTITION`, which is the main thing partitioning buys.
- The correlated-session tables the query API and the Grafana drill-down panels
  read.
