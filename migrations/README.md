# Migrations

Applied with [golang-migrate](https://github.com/golang-migrate/migrate), run as
a container so no Go dependency is needed to change the schema
([ADR-0023](../ARCHITECTURE.md#adr-0023--golang-migrate-as-the-migration-tool)).

`docker compose up` applies them automatically: a one-shot `migrate` service
runs once PostgreSQL is healthy, and both binaries wait for it to succeed.

```bash
make migrate-up        # apply everything pending
make migrate-version   # current version
make migrate-down      # roll back exactly one
make migrate-reset     # roll back everything (destroys all data)
```

## What is here

| Version | Creates |
|---|---|
| `000001_event_raw` | append-only intake, partitioned by `occurred_at` |
| `000002_event_raw_bootstrap_partitions` | a finite bootstrap window of daily partitions |
| `000003_participant_join` | the durable unit, plus its three indexes |

The shape and the reasoning are in
[ADR-0024](../ARCHITECTURE.md#adr-0024--the-event-schema). The SQL files carry
the reasoning inline too — they are meant to be read.

## Two things that will bite you

**Migration `000002` is a bootstrap, not a maintenance mechanism.** It creates
daily partitions for a fixed window around the time it runs. Partitions must
exist ahead of time or **inserts fail**, and there is deliberately no `DEFAULT`
partition to catch the overflow — a default partition trades a loud outage for
quiet wrongness. The recurring job that keeps the window ahead of `now()` does
not exist yet.

TODO(scope): partition maintenance. It is a scheduled job, not a migration, and
ADR-0023 explains why keeping it out of this directory is deliberate.

**A failed migration leaves a dirty version.** golang-migrate marks the version
table dirty and refuses to proceed until it is cleared:

```bash
make migrate-force VERSION=<n>
```

Use it with the schema in front of you — you have to know what actually applied.

## Adding one

Create a matched pair, `NNNNNN_name.up.sql` and `NNNNNN_name.down.sql`. Both are
required: reversibility is not optional here, because this schema is expected to
be revised repeatedly as ADR-0024 is revisited. Verify the round trip before
committing:

```bash
make migrate-up && make migrate-reset && make migrate-up
```
