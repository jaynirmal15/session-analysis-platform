# Session Analysis Platform

A self-hosted platform for understanding what actually happened inside your
real-time media sessions.

It ingests lifecycle events from real-time media backends, correlates them into
per-session timelines, and exposes both aggregate metrics across the fleet and
drill-down into any individual session.

> **Status: scaffolding.** The repository structure, local development stack and
> decision log are in place. There is no business logic yet — no schema, no
> webhook handling, no correlation. See [Current status](#current-status) for
> exactly what does and does not work today.

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

## Running it locally

**Requirements:** Docker with Compose v2, and Go 1.25+ if you want to run the
services on the host.

```bash
make up
```

That builds both service images and starts the full stack. Every value has a
working default, so no `.env` file is needed; copy `.env.example` to `.env` if
you want to override ports or credentials.

| Service | URL | Notes |
|---|---|---|
| Grafana | http://localhost:3000 | `admin` / `admin`; both datasources pre-provisioned |
| Prometheus | http://localhost:9090 | check **Status → Targets**; all should be `UP` |
| LiveKit | http://localhost:7880 | dev keys, `devkey` / `devsecret…` |
| OTel Collector | localhost:4317 | OTLP gRPC in; `:8889` metrics out |
| ingester | http://localhost:8080/healthz | health endpoint only, for now |
| queryapi | http://localhost:8081/healthz | health endpoint only, for now |
| PostgreSQL | localhost:5432 | `sap` / `sap` / `sap`; **empty, no schema** |

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

**Works today:**

- Full local stack comes up with `docker compose up`, wired end to end.
- Both binaries boot, serve `/healthz`, and export OpenTelemetry metrics through
  the collector to Prometheus.
- Grafana has both datasources provisioned and connected.
- Package structure with a documented responsibility per package.
- The decision log, seeded with fifteen entries.

**Deliberately absent** — each has a `TODO(scope)` marker in the package where
it will land:

- The canonical event schema and any database migrations.
- Webhook receipt, signature verification and payload handling.
- The media-backend adapter interface. Not stubbed either: a placeholder
  interface gets implemented against, and then it is not a placeholder.
- Correlation logic.
- Query API handlers and Grafana dashboards.

The schema is being designed separately. See
[ADR-0014](ARCHITECTURE.md#adr-0014--event-schema-and-migrations-deliberately-deferred)
for why nothing plausible-looking was committed in the meantime.

---

## Repository layout

```
cmd/
  ingester/            write path binary
  queryapi/            read path binary
internal/
  config/              process configuration        ─┐
  telemetry/           OpenTelemetry SDK setup       ├─ the only shared packages
  database/            connection construction      ─┘
  session/             canonical domain vocabulary  ── documented exception, ADR-0015
  ingest/              WRITE PATH
    webhook/           receive and authenticate deliveries
    adapter/           backend → canonical translation
      livekit/         first backend
      mediasoup/       second backend, deliberately different
    store/             write-side persistence port
    pipeline/
      correlate/       events → session timelines
  query/               READ PATH
    api/               aggregate + drill-down handlers
    store/             read-side persistence port
migrations/            empty on purpose
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

**Phase 1 — Schema and ingest**
Canonical event model. Partitioned tables and partition management. LiveKit
webhook receipt with signature verification. Durable-receipt-then-acknowledge,
because retries are backpressure.

**Phase 2 — Correlation**
Events to session timelines. The hard parts up front: idempotency under
at-least-once delivery, out-of-order arrival, and deciding when a session that
has stopped emitting events is actually over.

**Phase 3 — Query and dashboards**
Aggregate and drill-down endpoints. The two Grafana dashboards. First real
numbers on query cost.

**Phase 4 — The second backend**
mediasoup. The point at which the adapter abstraction is either validated or
shown to have leaked. Either outcome is a result.

**Phase 5 — The PostgreSQL ceiling**
Load generation, measurement, and finding where plain partitioned PostgreSQL
stops being enough. The revisit triggers in
[ADR-0004](ARCHITECTURE.md#adr-0004--plain-partitioned-postgresql-not-timescaledb)
are the acceptance criteria.

**Phase 6 — Kubernetes**
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
