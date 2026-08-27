# Session Analysis Platform

A self-hosted platform for understanding what actually happened inside your
real-time media sessions.

It ingests lifecycle events from real-time media backends, correlates them into
per-session timelines, and exposes both aggregate metrics across the fleet and
drill-down into any individual session.

> **Status: ingesting.** The receiver takes real LiveKit webhooks, verifies
> them, stores them idempotently, and opens and closes joins. Sessions are not
> derived yet and there is no query API. See
> [Current status](#current-status) for exactly what does and does not work.

---

## What it does

Real-time media backends emit lifecycle events — a room started, a participant
joined, a track was published, a participant left. Individually they are noise.
Correlated, they answer the questions you actually have:

- How long did that session last, and who was in it?
- Are join failures rising, and for which backend?
- Why did *this* specific call go badly?

The platform sits alongside your media infrastructure and answers both the
fleet-wide question and the single-session question, from the same event stream.

### Design commitments

Three of these matter enough to state up front, because they shape everything
else:

- **The join is stored; the session is derived.** A "session" — a participant
  in a room, spanning reconnects — is never a row. Joins are stored with their
  gaps intact and grouped at read time under a threshold you pass in, so
  "how would these group at 30 seconds instead of 120?" is a query rather than
  a migration. Applying that threshold at write time would destroy the ability
  to ask the question later.
- **The media backend is pluggable, and that claim gets tested.** LiveKit is the
  first backend. mediasoup is the second, chosen specifically because it is
  architecturally unlike LiveKit — a library rather than a server, with no
  vendor-defined webhook contract. An abstraction validated against one
  implementation is not an abstraction.
- **Plain PostgreSQL, time-partitioned. Not TimescaleDB.** This is deliberate.
  Finding out where plain PostgreSQL breaks under this workload, with numbers,
  is one of the project's goals.
- **OpenTelemetry from the first commit.** A system for observing sessions that
  cannot observe itself has no standing to make claims about anything else.

Every decision, including the ones that were rejected and why, is recorded in
**[ARCHITECTURE.md](ARCHITECTURE.md)**. That file is currently the most
substantial thing in this repository, and that is intentional.

---

## Architecture at a glance

```
  LiveKit  ──webhook──┐
                      │
  mediasoup ─(later)──┤
                      ▼
              ┌───────────────┐        ┌──────────────┐
              │   ingester    │───────▶│  PostgreSQL  │
              │ receive       │        │  partitioned │
              │ adapt         │        │  by event    │
              │ store         │        │  time        │
              │ correlate     │        └──────┬───────┘
              └───────┬───────┘               │
                      │ OTLP           ┌──────┴───────┐
                      ▼                │   queryapi   │
              ┌───────────────┐        │  aggregates  │
              │ OTel Collector│        │  drill-down  │
              └───────┬───────┘        └──────────────┘
                      │ scrape
                      ▼
              ┌───────────────┐
              │  Prometheus   │
              └───────┬───────┘
                      │
                      ▼
              ┌───────────────────────────────────┐
              │ Grafana                           │
              │  Prometheus  → fleet aggregates   │
              │  PostgreSQL  → session drill-down │
              └───────────────────────────────────┘
```

Grafana reads PostgreSQL directly for drill-down. That is a deliberate trade —
it makes the schema an interface with more than one consumer. See
[ADR-0008](ARCHITECTURE.md#adr-0008--grafana-reading-both-prometheus-and-postgresql).

---

## Data model

Two tables. The full reasoning is
[ADR-0024](ARCHITECTURE.md#adr-0024--the-event-schema); the SQL in
[`migrations/`](migrations) carries it inline as well.

**`event_raw`** — every accepted webhook, append-only, partitioned daily by the
backend's own event time. Typed columns for the eight fields correlation keys
on; `payload jsonb` keeps the rest, because mediasoup's events will not share
LiveKit's shape. A field earns a column when a query filters on it, not when it
looks important.

**`participant_join`** — the durable unit. One participant's presence in one
room, from an observed start to an observed end.

Three properties of that table do most of the work:

- **`ended_at` is nullable and no sweeper ever fills it in.** NULL means "still
  open, or we never found out". That gives three states rather than two —
  open-and-recent, open-and-stale, and closed-with-a-reason — and the middle one
  is a direct measurement of how much your delivery path is losing. A sweeper
  would erase exactly that signal
  ([ADR-0020](ARCHITECTURE.md#adr-0020--ended_at-is-nullable-and-only-ever-set-from-an-observed-event)).
- **`end_reason` records how the end was learned**, not just that it happened.
  `room_finished` is not the same information as `participant_left`.
- **Overlap is indexed with GiST over a generated `tstzrange`.** An open join
  becomes `[started_at, )`, so it overlaps any later window with no NULL
  handling at the call site — the branch that is easy to forget, and whose
  omission would silently hide the open joins that matter most.

The correlation key is `(room, identity)`. Identity is caller-supplied and its
stability within a room is
[a contract you must meet](ARCHITECTURE.md#adr-0016--stable-participant-identity-is-a-required-integration-contract):
LiveKit's participant SID is new on every join and cannot serve. Violations are
counted and surfaced, never guessed at and repaired.

### Events consumed

Ingested: `room_started`, `room_finished`, `participant_joined`,
`participant_left`, `track_published`, `track_unpublished`.

Rejected and counted: everything egress and ingress — they describe operations
performed *on* a room, not experiences *of* a participant, and storing them
would distort the very volume measurement this project exists to take.
Unrecognised types are stored anyway, because an unknown type is evidence the
integration drifted and a counter alone would destroy it
([ADR-0022](ARCHITECTURE.md#adr-0022--livekit-event-ingest-scope-room-lifecycle-stays-raw-only)).

---

## Running it locally

**Requirements:** Docker with Compose v2, and Go 1.25+ if you want to run the
services on the host.

```bash
make up
```

That builds both service images and starts the full stack. Every value has a
working default, so no `.env` file is needed; copy `.env.example` to `.env` if
you want to override ports or credentials.

Migrations apply automatically: a one-shot `migrate` service runs once
PostgreSQL is healthy, and both binaries wait for it to succeed — so the step is
visible in compose output rather than hidden inside application startup. Use
`make migrate-up` / `migrate-down` / `migrate-version` for manual control, and
`make psql` for a shell on the database.

| Service | URL | Notes |
|---|---|---|
| Grafana | http://localhost:3000 | `admin` / `admin`; both datasources pre-provisioned |
| Prometheus | http://localhost:9090 | check **Status → Targets**; all should be `UP` |
| LiveKit | http://localhost:7880 | dev keys, `devkey` / `devsecret…` |
| OTel Collector | localhost:4317 | OTLP gRPC in; `:8889` metrics out |
| ingester | http://localhost:8080/webhook/livekit | receiving; `/healthz` and `/readyz` too |
| queryapi | http://localhost:8081/healthz | health endpoint only, for now |
| PostgreSQL | localhost:5432 | `sap` / `sap` / `sap`; migrated on startup |

### Verifying it works

```bash
curl -s localhost:8080/healthz && curl -s localhost:8081/healthz
```

Then open Prometheus and run:

```promql
sum by (service_name) (go_memory_used_bytes)
```

You should get two series, `sap-ingester` and `sap-queryapi`. That single query
exercises the entire telemetry path — the SDK in each process, OTLP to the
collector, the collector's Prometheus exporter, and the scrape — before any
domain metric exists to look at. The `service_name` label arrives because the
collector is configured to promote OpenTelemetry resource attributes to
Prometheus labels.

`go_goroutine_count` works the same way. Prometheus **Status → Target health**
should show four jobs, all `UP`: `prometheus`, `otel-collector-pipeline`,
`otel-collector-internal` and `livekit`.

To confirm the schema applied:

```bash
make migrate-version
```

It should print `3`. Both tables are empty — nothing writes to them yet.

### Watching it actually ingest

```bash
make smoke
```

This joins the local LiveKit with a real client publishing video, then asserts
the rows its webhooks produced — six event types, one join, opened and closed
from observed events. It runs the client **inside** the compose network, which
is why it works on macOS despite the media caveat above.

Expect roughly this:

```
 room_started       |                   | 00:55:08
 participant_joined | smoke-participant | 00:55:09
 track_published    | smoke-participant | 00:55:09
 participant_left   | smoke-participant | 00:55:13
 track_unpublished  | smoke-participant | 00:55:13
 room_finished      |                   | 00:55:33

 smoke-participant | PA_dsfQFoQhMBEw | closed | participant_left | 00:00:04
```

The join closes with `participant_left` rather than `room_finished` because the
participant departed before the room was reaped — the more specific observed
reason wins, and both are recorded distinctly on purpose
([ADR-0020](ARCHITECTURE.md#adr-0020--ended_at-is-nullable-and-only-ever-set-from-an-observed-event)).

Integration tests against a real PostgreSQL, including the partition-miss
classifier:

```bash
make test-integration
```

### Reproducing the numbers in the decision log

ADR-0004 and ADR-0024 cite measurements. They are reproducible rather than
asserted:

```bash
make bench
```

This seeds synthetic joins and times the reference query across open-join
fractions. **It truncates `participant_join`** — development databases only;
`make migrate-reset && make migrate-up` clears up afterwards.

[`benchmarks/README.md`](benchmarks/README.md) has the caveats, and the story of
how the first version of this benchmark produced a meaningless baseline that
reached an ADR before anyone noticed.

### The other local workflow

For iterating on Go code, run the dependencies in containers and the service on
your host:

```bash
make run-local
```

This starts PostgreSQL, LiveKit, the collector, Prometheus and Grafana in
compose, then runs the ingester with `go run`, pointed at the containerised
collector on `localhost:4317`. Telemetry from the host process shows up in
Grafana exactly like the containerised one. `make run-local-api` does the same
for the query API.

### Known limitation on macOS and Windows

Docker Desktop has no host networking, so self-hosted LiveKit cannot reliably
discover a routable external IP and **actual media will not flow**. Signalling,
the HTTP API and webhook delivery — the parts this project consumes — all work
normally. On Linux, media works too.

### Everything else

```bash
make help
```

---

## Current status

**Works today**, verified against real LiveKit and real PostgreSQL rather than
against fixtures:

- Full local stack comes up with `docker compose up`, migrations applied.
- **The LiveKit webhook receiver.** Signature verified before the payload is
  parsed; unverified deliveries never reach a decoder. Idempotent inserts keyed
  on a derived event id, so redelivery is discarded and its join effect is not
  re-applied.
- **Join maintenance.** `participant_joined` opens, `participant_left` closes,
  `room_finished` closes everything still open in the room with a distinct
  reason. `ended_at` is only ever written from an observed event.
- The schema: two tables, daily partitioning, three purpose-chosen indexes, all
  reversible.
- OpenTelemetry throughout, including counters for the things the platform
  decided to observe rather than repair — duplicate deliveries, unmatched
  closes, out-of-order closes, missing partitions.
- The decision log, now twenty-seven entries.

**Deliberately absent** — each has a `TODO(scope)` marker where it will land:

- Session derivation. Joins are stored; grouping them under a threshold is a
  read-time concern and there is nothing to read yet
  ([ADR-0019](ARCHITECTURE.md#adr-0019--the-join-is-the-durable-unit-the-session-is-a-view)).
- Query API handlers and Grafana dashboards.
- The media-backend adapter interface. Still not stubbed: LiveKit is
  implemented concretely, and the interface waits for mediasoup to force its
  shape ([ADR-0003](ARCHITECTURE.md#adr-0003--mediasoup-as-the-deliberate-second-backend)).
- Any sweeper that closes joins by timeout. There will never be one.

**One live obligation, worth knowing before you run this for real.** Partitions
must exist ahead of time or inserts fail, there is no `DEFAULT` partition to
absorb the overflow, and the recurring job that keeps the window ahead of `now()`
does not exist yet. Migration `000002` creates a finite bootstrap window and
nothing extends it. See [`migrations/README.md`](migrations/README.md).

---

## Repository layout

```
cmd/
  ingester/            write path binary — receiving
  queryapi/            read path binary
internal/
  config/              process configuration        ─┐
  telemetry/           OpenTelemetry SDK setup       ├─ the only shared packages
  database/            connection construction      ─┘
  session/             canonical domain vocabulary  ── documented exception, ADR-0015
  ingest/              WRITE PATH
    eventid/           uuidv5 derivation, length-prefixed
    webhook/           receive and authenticate deliveries
    metrics/           write-path OTel instruments
    adapter/           backend → canonical translation
      livekit/         first backend
      mediasoup/       second backend, deliberately different
    store/             write-side persistence port
    pipeline/
      correlate/       events → session timelines
  query/               READ PATH
    api/               aggregate + drill-down handlers
    store/             read-side persistence port
migrations/            golang-migrate .sql pairs, up and down
benchmarks/            reproduces the numbers cited in the ADRs
scripts/               webhook-smoke.sh — end to end against real LiveKit
deploy/                compose service configuration
```

`internal/ingest` and `internal/query` may not import each other. There is no
`shared`, `common` or `util` package, and adding one is explicitly out of
bounds — see [ADR-0009](ARCHITECTURE.md#adr-0009--two-binaries-separated-paths-no-shared-package)
for why, and what is allowed instead.

Every package has a `doc.go` explaining what belongs in it and what does not.
Those files are worth reading before the code.

---

## Roadmap

Each phase is expected to produce at least one written article, and articles
are written from [ARCHITECTURE.md](ARCHITECTURE.md).

**Phase 0 — Scaffolding · done**
Repository structure, local stack, telemetry path, decision log.

**Phase 1 — Schema · done**
Canonical event model, partitioned tables, reversible migrations, and the eight
decisions behind them (ADR-0016 to ADR-0024). Notably: what a session *is*, and
why it is not a row.

**Phase 2 — Ingest · done**
LiveKit webhook receipt with signature verification, idempotent storage, and
join maintenance. Two problems this phase normally carries were solved by
decision rather than code: sessions are not stitched at write time (ADR-0019),
and no sweeper closes joins by timeout (ADR-0020).

**Phase 3 — Partition maintenance**
The one live obligation. The bootstrap window is finite and nothing extends it.

**Phase 4 — Query and dashboards**
Aggregate and drill-down endpoints. The two Grafana dashboards, including the
open-and-stale panel that measures how much the delivery path is losing. First
real numbers on query cost against non-synthetic data.

**Phase 5 — The second backend**
mediasoup. The point at which the adapter abstraction is either validated or
shown to have leaked. Either outcome is a result.

**Phase 6 — The PostgreSQL ceiling**
Load generation, measurement, and finding where plain partitioned PostgreSQL
stops being enough. The revisit trigger in
[ADR-0004](ARCHITECTURE.md#adr-0004--plain-partitioned-postgresql-not-timescaledb)
is the acceptance criterion, and it is now stated in terms of the actual
reference query — with the open-join fraction reported alongside, because open
joins are unbounded ranges and are that query's pathological case.

**Phase 7 — Kubernetes**
Manifests, independent scaling of ingest and correlation, and whatever ADR-0006
turns out to have got wrong.

---

## Contributing

Early days, but the useful contribution right now is argument rather than code:
if a decision in [ARCHITECTURE.md](ARCHITECTURE.md) is wrong, the "Revisit when"
section of each entry is where to aim. Open an issue.

If you change something structural, add an ADR. Entries are append-only —
superseding a decision means adding an entry, not editing an old one.

## License

[MIT](LICENSE) © Jay Nirmal
