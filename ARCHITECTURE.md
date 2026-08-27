# Architecture Decision Log

This file, not the code, is the primary artifact of this repository right now.

Every entry records a decision, the alternatives that were genuinely considered,
why each was rejected, and — most importantly — **what would make us revisit
it**. A decision without a revisit trigger is a belief, not a decision.

Two conventions worth stating up front:

1. **Rejected does not mean wrong.** Several options below are rejected while
   being, on the technical merits, better at the specific job. Where that is
   true it is said plainly. An honest log is more useful than a flattering one.
2. **Entries are append-only.** Superseding a decision means adding an entry
   that supersedes it and marking the old one, not editing history.

**Format:** Status · Context · Decision · Alternatives considered · Consequences
· Revisit when.

---

## Index

| ID | Decision | Status |
|----|----------|--------|
| [ADR-0001](#adr-0001--go-for-the-ingester) | Go for the ingester | Accepted |
| [ADR-0002](#adr-0002--livekit-as-the-first-media-backend) | LiveKit as the first media backend | Accepted |
| [ADR-0003](#adr-0003--mediasoup-as-the-deliberate-second-backend) | mediasoup as the deliberate second backend | Accepted, not started |
| [ADR-0004](#adr-0004--plain-partitioned-postgresql-not-timescaledb) | Plain partitioned PostgreSQL, not TimescaleDB | Accepted |
| [ADR-0005](#adr-0005--opentelemetry-from-the-first-commit) | OpenTelemetry from the first commit | Accepted |
| [ADR-0006](#adr-0006--collector-in-the-path-prometheus-pulls-from-it) | Collector in the path; Prometheus pulls from it | Accepted |
| [ADR-0007](#adr-0007--metrics-first-trace-backend-deferred-not-tempo-yet) | Metrics first; trace backend deferred (not Tempo yet) | Deferred |
| [ADR-0008](#adr-0008--grafana-reading-both-prometheus-and-postgresql) | Grafana reading both Prometheus and PostgreSQL | Accepted |
| [ADR-0009](#adr-0009--two-binaries-separated-paths-no-shared-package) | Two binaries, separated paths, no `shared` package | Accepted |
| [ADR-0010](#adr-0010--no-pkg-directory) | No `pkg/` directory | Accepted |
| [ADR-0011](#adr-0011--push-based-webhook-ingest-not-polling) | Push-based webhook ingest, not polling | Accepted |
| [ADR-0012](#adr-0012--kubernetes-as-the-deployment-target-deferred) | Kubernetes as the deployment target, deferred | Deferred |
| [ADR-0013](#adr-0013--mit-license) | MIT license | Accepted |
| [ADR-0014](#adr-0014--event-schema-and-migrations-deliberately-deferred) | Event schema and migrations deliberately deferred | Superseded by ADR-0024 |
| [ADR-0015](#adr-0015--internalsession-as-the-one-shared-domain-package) | `internal/session` as the one shared domain package | Accepted, provisional |
| [ADR-0016](#adr-0016--stable-participant-identity-is-a-required-integration-contract) | Stable participant identity is a required integration contract | Accepted |
| [ADR-0017](#adr-0017--no-cross-room-canonical-identity) | No cross-room canonical identity | Rejected |
| [ADR-0018](#adr-0018--multi-tenancy-deferred) | Multi-tenancy deferred | Deferred |
| [ADR-0019](#adr-0019--the-join-is-the-durable-unit-the-session-is-a-view) | The join is the durable unit; the session is a view | Accepted |
| [ADR-0020](#adr-0020--ended_at-is-nullable-and-only-ever-set-from-an-observed-event) | `ended_at` is nullable and only ever set from an observed event | Accepted |
| [ADR-0021](#adr-0021--no-settling-window-at-write-time) | No settling window at write time | Accepted |
| [ADR-0022](#adr-0022--livekit-event-ingest-scope-room-lifecycle-stays-raw-only) | LiveKit event ingest scope; room lifecycle stays raw-only | Accepted |
| [ADR-0023](#adr-0023--golang-migrate-as-the-migration-tool) | golang-migrate as the migration tool | Accepted |
| [ADR-0024](#adr-0024--the-event-schema) | The event schema (supersedes ADR-0014) | Accepted |

---

## ADR-0001 — Go for the ingester

**Status:** Accepted · 2026-08-25

### Context

The ingester receives bursty webhook traffic from a media backend, persists it,
and runs a stateful correlation stage over the result. Its dominant
characteristics are I/O concurrency, predictable latency under burst, and a
deployment story that has to work in Kubernetes later.

### Decision

Go, targeting the current stable toolchain.

### Alternatives considered

**Rust** — the best fit on raw merit for a high-throughput event pipeline: no GC
pauses, lower per-event cost, and excellent Postgres and OTel crates.

*Rejected because* the bottleneck this project intends to study is PostgreSQL's
behaviour under time-partitioned analytical load, not per-event CPU in the
ingester. Choosing Rust would pay a permanent iteration-speed cost — on a
codebase that is going to be rewritten repeatedly as the schema evolves — to
optimise the component that is not the constraint. If profiling later shows the
ingester is the constraint, this was the wrong call and the ADR should be
superseded.

**Node.js / TypeScript** — the closest thing to a native ecosystem for this
problem. LiveKit's server SDKs are strongest in TypeScript, and mediasoup is a
Node library, not a server. Choosing Node would let the mediasoup adapter embed
mediasoup directly.

*Rejected because* correlation is a stateful, CPU-and-memory-heavy join, and the
single-threaded event loop plus worker-thread juggling is the wrong shape for
it. We accept a real cost for this: because mediasoup is Node-native, the
mediasoup adapter (ADR-0003) will have to communicate with a separate Node
process rather than embedding the library. That awkwardness is a known,
priced-in consequence — and, usefully, it makes the second backend genuinely
different rather than a second flavour of the same integration.

**Elixir / OTP** — arguably the ideal model for session correlation: a
supervised process per live session is almost exactly the domain model, and
late/out-of-order event handling is idiomatic.

*Rejected because* it would introduce too many novel variables at once. The
point of ADR-0004 is to attribute a performance ceiling to PostgreSQL with
confidence; doing that from an unfamiliar runtime makes every measurement
arguable. Also a smaller pool of readers for the article series, which is a
legitimate consideration for a project whose output is partly written.

**Java / Kotlin** — mature JDBC, mature OTel auto-instrumentation, excellent
profilers.

*Rejected because* JVM memory footprint and startup time are a poor fit for the
many-small-replicas shape that ingest scaling will take, and the build/iteration
loop is heavier than the project's cadence wants.

### Consequences

- GC pauses are a possible source of latency noise in ingest measurements, and
  will have to be ruled out explicitly before attributing latency to Postgres.
- Talking to mediasoup means crossing a process boundary (see above).

### Revisit when

- Profiling shows ingester CPU, not database time, dominating end-to-end latency
  at target volume.
- Correlation design converges on a process-per-session model, at which point
  the Elixir argument gets much stronger.

---

## ADR-0002 — LiveKit as the first media backend

**Status:** Accepted · 2026-08-25

### Context

The platform needs a real, self-hostable media backend that emits lifecycle
events (room started, participant joined, track published, and counterparts) so
that session timelines can be reconstructed from them.

### Decision

LiveKit, self-hosted, integrated via its webhook mechanism.

### Alternatives considered

**Janus** — long-established, well-understood, event handler plugins exist.

*Rejected because* its event model is plugin-specific rather than global: what
you receive depends on which plugin the room used, so "a session" has no single
definition at the source. Building the canonical model against that as the first
backend would mean designing the abstraction against the hardest case before
understanding the easy one.

**Jitsi / JVB** — mature, widely deployed, free.

*Rejected because* it is designed as a complete meeting product rather than a
programmable SFU. Extracting clean per-participant lifecycle events means
reading Colibri and XMPP internals, which is a large surface for a first
integration and produces knowledge that transfers to nothing else.

**mediasoup first** — deliberately held back to be the second backend. See
ADR-0003.

**A commercial hosted backend first** — see ADR-0003's treatment of Zoom Video
SDK; the self-hostability objection applies with more force to a first backend,
because it would make the repository unrunnable for contributors on day one.

### Consequences

- Webhook delivery is at-least-once and unordered, which shapes the entire write
  path (ADR-0011).
- LiveKit's identifiers (room name, participant identity) are caller-supplied
  and not unique over time; canonical identity must be derived rather than
  copied.
- Self-hosted LiveKit in Docker Desktop cannot route media reliably on macOS or
  Windows (no host networking). Signalling and webhooks — the parts this project
  needs — do work.

### Revisit when

- LiveKit's webhook payloads turn out to be too lossy to reconstruct a timeline
  without also polling its API (which would partially reverse ADR-0011).
- Operating a self-hosted LiveKit becomes a larger time cost than the analysis
  work it feeds.

---

## ADR-0003 — mediasoup as the deliberate second backend

**Status:** Accepted, not started · 2026-08-25

### Context

The adapter layer (`internal/ingest/adapter`) claims that media backends can be
swapped behind a canonical event model. That claim is unfalsifiable with one
implementation: an interface designed against a single backend is a description
of that backend with an `interface` keyword in front of it.

The second backend is therefore chosen as a *test*, and the selection criterion
is difference, not convenience.

### Decision

mediasoup, added after LiveKit is working end to end, specifically because it is
architecturally unlike LiveKit:

- It is a **library, not a server**. There is no vendor-defined webhook contract
  to receive; lifecycle signals come from whatever application wraps it.
- Its event vocabulary is **application-defined**, so the mapping to canonical
  events is a real translation rather than a rename.
- It is **Node-native**, so integration crosses a process boundary (ADR-0001).

If the abstraction survives this, it has been tested. If adding it requires
changing `internal/session`, `internal/ingest/store` or `internal/query`, the
abstraction leaked — and that finding is more valuable than a clean result.

### Alternatives considered

**Pion / ion-sfu** — Go-native, would integrate with the least friction of any
option, and ion-sfu is a genuine SFU with a room model.

*Rejected because* it is too similar to be a test. LiveKit is itself built on
Pion; a Pion-based second backend shares LiveKit's underlying assumptions about
transports, track identity and lifecycle ordering, so the adapter interface
could pass by accident rather than by design. Secondary objection: ion-sfu is
effectively unmaintained, so any friction found would be ambiguous between "the
abstraction is wrong" and "the project is abandoned".

**Zoom Video SDK** — on the difference criterion, this is the *strongest*
candidate: hosted, proprietary, with its own webhook semantics, its own
authentication model, rate limits, and an event stream we do not control. It
would stress the seam in ways an OSS library cannot, particularly around
delivery guarantees and backfill.

*Rejected because* it is not self-hostable. This is a public repository whose
purpose is partly to be run by readers; making a paid Zoom account and a
publicly reachable webhook endpoint prerequisites for `docker compose up` would
exclude most of them. The technical argument for it is acknowledged and stands.

**A second LiveKit deployment, or a synthetic/mock backend** — trivial to add.

*Rejected because* it proves nothing. A mock backend conforms to whatever
interface it is given, by construction.

### Consequences

- The mediasoup adapter needs a companion Node process; the repository will grow
  a non-Go component.
- The adapter interface stays **undesigned** until this backend is in progress.
  There is deliberately no placeholder interface in
  `internal/ingest/adapter` — a placeholder gets implemented against, and then
  it is not a placeholder.

### Revisit when

- The mediasoup integration turns out to require *no* adapter changes at all.
  That would be a warning sign, not a success: it likely means the canonical
  model quietly encodes LiveKit's assumptions and mediasoup is being bent to
  fit.
- A real use case requires a hosted backend, at which point the Zoom Video SDK
  rejection is worth re-opening with the contributor-friction cost accepted
  explicitly (for example, behind an optional compose profile).

---

## ADR-0004 — Plain partitioned PostgreSQL, not TimescaleDB

**Status:** Accepted · 2026-08-25

### Context

The workload has two shapes with opposite requirements. Ingest is append-heavy,
high-cardinality, and continuous. Reads are either wide time-range aggregates
across many sessions, or narrow deep lookups of one session's full timeline.
Retention is measured in weeks, so old data must be removable cheaply.

### Decision

Plain PostgreSQL 16, using **native declarative partitioning by event time**. No
Timescale extension, no columnar store, no second database.

This decision is made with full knowledge that specialised options are better at
parts of this workload. **Reaching the ceiling of plain PostgreSQL is an
intended outcome of the project**, and the measurements taken on the way there
are the subject of a planned article. A tool that removes the ceiling before it
has been measured also removes the finding.

### Alternatives considered

**TimescaleDB** — the obvious right answer on the merits. Hypertables automate
the partition management this decision makes manual; continuous aggregates solve
the rollup problem that the read path will otherwise have to solve by hand;
native compression would cut storage substantially for exactly this data shape.

*Rejected because* it would pre-empt the measurement described above. Two
secondary objections that would matter even without that: the most valuable
features are under the Timescale License rather than open source, and depending
on an extension narrows which managed PostgreSQL offerings the project can run
on later.

This is the rejection most likely to be reversed, and reversing it is a success
condition, not a failure — but only *after* there are numbers.

**ClickHouse** — dramatically faster for the fleet-wide aggregate queries;
columnar storage, excellent compression, purpose-built for exactly the
"aggregate metrics over an event stream" half of the problem.

*Rejected because* it is weak precisely where the other half of the problem
lives. Per-session drill-down is a narrow point lookup; correlation is naturally
expressed as idempotent upserts, and ClickHouse mutations are asynchronous with
no transactional guarantees. Running it *alongside* PostgreSQL is the real
industry pattern, and that is the rejection: two stateful systems, a
synchronisation path between them, and dual-write consistency semantics — all
adopted before any evidence that one store is insufficient.

**Kafka plus a stream processor (Flink / Kafka Streams)** — the textbook answer
for correlating an unbounded, out-of-order event stream, with real windowing and
watermark semantics instead of a hand-rolled settling window.

*Rejected as premature.* It introduces a second stateful system and moves the
correlation logic out of SQL, where it is easiest to explain, inspect and
change — which matters for a project that is being written about. Note that the
hard part is not avoided: the webhook source is already at-least-once and
unordered (ADR-0011), so idempotency and late arrival must be handled either
way. Kafka would provide better tools for it, not fewer problems.

**SQLite / DuckDB** — DuckDB in particular is excellent at the analytical half.

*Rejected because* single-writer, embedded stores do not fit a service that will
have multiple ingest replicas (ADR-0012).

### Consequences

Accepted knowingly:

- Partitions must be created **ahead of time**, or inserts fail. This needs a
  maintenance job, and forgetting it is an outage.
- Any query that omits the partition key scans every partition. The read path
  has to make bounded ranges mandatory, including for Grafana panels (ADR-0008).
- **No continuous aggregates.** Rollup tables, and keeping them correct in the
  face of late-arriving events, are the application's problem.
- Retention becomes `DETACH PARTITION` + `DROP`, which is cheap — this is the
  main thing partitioning buys, and it is worth a lot.
- Autovacuum behaviour on large partitioned tables is a known operational sharp
  edge and should be instrumented from the start (ADR-0005).

### Revisit when

Any of the following, **measured rather than suspected**.

Since [ADR-0024](#adr-0024--the-event-schema) the first trigger is stated in
terms of the actual reference query rather than a generic row count. A row count
alone does not describe this workload: the *open-join population* matters more
than the total, because open joins are unbounded ranges (ADR-0020) and are the
pathological case for the overlap index.

**The reference query.** `participant_join` rows overlapping a bounded time
range, windowed by `(backend, room_name, participant_identity)` with `lag()`
computing reconnect gaps and a threshold applied (ADR-0019). Trigger: **p95
above 2s**, with `EXPLAIN` confirming the GiST overlap index is in use, and
reported as a **(row count, open fraction) pair**. A row count alone is not
interpretable, because open joins are unbounded ranges (ADR-0020) and dominate
the result set.

How much that matters, from the measurements in ADR-0024 — the row count at
which the same 2s trigger fires, projected from the fitted model:

| open fraction | joins at which p95 ≈ 2s |
|---|---|
| 1% | ~11M |
| 5% | ~3.1M |
| 10% | ~1.65M |
| 30% | ~570k |

**A 5× swing in the triggering row count between 5% and 30% open**, from one
variable that has nothing to do with how much data was ingested. A deployment
with a lossy delivery path hits this ceiling five times sooner than a healthy
one holding the same volume — and the fix there is to repair delivery, not to
change database.

*Measured anchors — reproduce with `make bench`, harness and caveats in
[`benchmarks/`](benchmarks). PostgreSQL 16.15, 2-hour window, warm cache, median
of five to nine runs:* 200k joins @ 5% open = 115 ms · 200k @ 30% = 552 ms · 1M @ 5% =
821 ms · 1M @ 10% = 1,216 ms. The projections above extrapolate linearly in
matched rows from these; they are a fit on synthetic data and should be
re-derived against real ingest.

*Superseded baseline:* an earlier version of this trigger cited 66 ms at 200,000
joins with 5% open. That measurement was taken on degenerate seed data in which
all 200,000 rows shared one `started_at`, and has been discarded. ADR-0024
records how it failed, and `benchmarks/run.sh` now refuses to produce a number
under those conditions.

**The drill-down query.** All `event_raw` rows for one
`(room_name, participant_identity)` within a bounded range stops pruning to a
small number of partitions, or exceeds p95 1s.

**Unchanged:**

- Hand-maintained rollup code grows past the complexity of adopting continuous
  aggregates.
- Partition maintenance causes a second production incident.
- Storage cost becomes a material constraint that compression would relieve.

---

## ADR-0005 — OpenTelemetry from the first commit

**Status:** Accepted · 2026-08-25

### Context

The platform's subject is observing real-time sessions. A system that cannot
observe itself has no standing to make claims about anything else — and, more
practically, ADR-0004 commits to finding a performance ceiling by measurement,
which requires instrumentation that predates the code being measured.

### Decision

The OpenTelemetry SDK is wired in `internal/telemetry` and initialised by both
binaries before anything else, in the first commit — before there is any domain
code to instrument. Metrics and traces are both configured; Go runtime metrics
are exported immediately so the pipeline is verifiable end to end today.

### Alternatives considered

**Add instrumentation later, once there is something to instrument** — the
default choice, and it is not unreasonable.

*Rejected because* the expensive part of instrumentation is not the metric
definitions, it is threading context through code that was written without it.
Retrofitting propagation into a correlation pipeline is a refactor of the
pipeline. Additionally, adding instrumentation after the fact means no baseline,
and the Postgres-ceiling work needs before-and-after numbers.

**`prometheus/client_golang` directly** — simpler, one fewer dependency, no
collector hop, and what most Go services actually do.

*Rejected because* it provides metrics only. The correlation stage's interesting
failure modes — out-of-order arrival, late events, duplicate deliveries — are
trace-shaped, not counter-shaped. Choosing client_golang means adding a second
SDK later and running both during the transition.

**A vendor agent (Datadog, New Relic, Honeycomb SDKs)** — better out-of-the-box
experience than assembling collector + Prometheus + Grafana.

*Rejected because* this is a public repository. Requiring a vendor account to
run the stack locally is a hard barrier for contributors and readers, and it
would make the observability content in the articles unreproducible.

### Consequences

- The dependency graph carries the OTel SDK, its exporters and gRPC from commit
  one, which is not nothing for a repository that otherwise has no dependencies.
- Traces are configured but go nowhere useful yet (ADR-0007).

### Revisit when

- SDK overhead appears in ingest profiling at target volume.
- OTel's Go API makes another breaking change large enough that pinning becomes
  a maintenance burden.

---

## ADR-0006 — Collector in the path; Prometheus pulls from it

**Status:** Accepted · 2026-08-25

### Context

Application telemetry has to reach Prometheus. There are two defensible
topologies: each service exposes `/metrics` and Prometheus scrapes services
directly, or services push OTLP to a collector and Prometheus scrapes the
collector.

### Decision

Services push OTLP/gRPC to an OpenTelemetry Collector. The collector's
Prometheus exporter aggregates on `:8889`, and Prometheus scrapes *that*. The
application never speaks the Prometheus protocol.

### Alternatives considered

**Direct scrape of each service** (using the OTel SDK's Prometheus exporter) —
simpler, one fewer moving part, one fewer hop of staleness, and it keeps
Prometheus's `up` metric meaningful per service.

*Rejected because* of where this is going, not where it is. Ingest replicas
under Kubernetes (ADR-0012) are dynamic and short-lived; per-pod scrape targets
plus service discovery is more configuration than the collector hop it replaces.
The collector is also where trace tail-sampling and PII filtering will have to
live once traces are real, so it enters the architecture eventually regardless —
and introducing it later means changing every service at once.

**This is the closest call in this document.** If Kubernetes is dropped from the
roadmap, this decision should be re-opened immediately.

**Collector → Prometheus remote write** — removes the scrape hop.

*Rejected because* it requires enabling Prometheus's remote-write receiver, and
pull keeps Prometheus authoritative about staleness and target health rather
than trusting a pusher's clock.

**Push straight to a hosted backend** — see ADR-0005.

### Consequences

- Metrics are visible after up to ~30s (15s SDK export interval + 15s scrape
  interval). Fine for dashboards, irritating when debugging a burst live.
- The collector is a single point of failure for *all* application telemetry,
  including the telemetry that would tell you the collector is struggling. Its
  own metrics are scraped separately on `:8888` for this reason.
- Prometheus's `up` for the application is not directly observable: the
  collector's target is up whether or not any service is exporting to it.
  Liveness has to come from an explicit heartbeat metric instead.

### Revisit when

- Kubernetes is dropped or deferred indefinitely — the main argument evaporates.
- The staleness hop actively interferes with debugging ingest bursts.
- The collector becomes a per-node daemonset decision, at which point the
  topology needs rethinking anyway.

---

## ADR-0007 — Metrics first; trace backend deferred (not Tempo yet)

**Status:** Deferred · 2026-08-25

### Context

The OTel SDK is configured for traces (ADR-0005), but the local stack has no
trace backend. Traces currently reach the collector and are logged, then
dropped.

### Decision

Ship metrics only for now. Do not add a trace backend to the local stack until
there are spans worth looking at.

### Alternatives considered

**Grafana Tempo now** — the natural choice, and the one this project will most
likely adopt: it is Grafana-native so no second UI is needed, it stores traces
in object storage cheaply, and it integrates with the exemplars story on the
Prometheus side.

*Rejected for now, and explicitly not on merit.* There are no spans yet. A trace
backend with no traces is stack weight that every contributor pays to run —
another container, another volume, another thing that can fail during
`docker compose up` — in exchange for an empty UI. The cost of reversing this is
one compose service and one exporter block in `deploy/otel/collector.yaml`; the
application does not change at all. That reversibility is the whole argument.

**Jaeger** — mature, simple to run, good enough UI.

*Rejected because* it means a second web UI alongside Grafana for a single
capability, and its retention/storage story is worse for anything beyond local
development.

**Storing traces in PostgreSQL** — tempting given ADR-0004's "one store" bias.

*Rejected outright.* Traces are a different data shape with a different
retention profile, and it would add write load to the exact database whose
ceiling is being measured, contaminating the measurement.

### Consequences

- Span instrumentation written now is unverifiable beyond collector logs, so
  there is a real risk of writing spans nobody has ever looked at.

### Revisit when

- The correlation pipeline exists and a single event's path crosses more than
  two components. That is the point where reading logs stops working, and it is
  the concrete trigger to add Tempo.

---

## ADR-0008 — Grafana reading both Prometheus and PostgreSQL

**Status:** Accepted · 2026-08-25

### Context

The platform must answer two different kinds of question: aggregate metrics
across many sessions ("what is the fleet doing?") and per-session drill-down
("what happened in *this* session?"). The first lives naturally in Prometheus.
The second cannot: Prometheus is not a store for high-cardinality per-entity
history, and a session ID as a label is exactly the cardinality mistake that
kills a Prometheus instance.

### Decision

Grafana as the single dashboard layer, with **both** datasources provisioned:
Prometheus for aggregates and PostgreSQL for per-session drill-down.

### Alternatives considered

**Grafana on Prometheus only, drill-down served by a purpose-built UI on top of
the query API** — architecturally cleaner: the database stays a private
implementation detail behind one API.

*Rejected for now* because a custom UI is a project of its own and would delay
everything the articles are about. The `internal/query/api` package still
exists and is still the right long-term home for drill-down; this decision is
about what ships first.

**Metabase or Superset for the SQL half** — better ad-hoc SQL exploration than
Grafana's table panels.

*Rejected because* it means a second tool, a second auth story and a second set
of dashboards to keep coherent, for one capability that Grafana handles
adequately.

### Consequences

This is the consequence worth reading twice: **the database schema becomes a
public interface with at least two consumers.**

- A migration can break dashboards, and dashboards are not covered by the Go
  test suite.
- Panel SQL is untracked query load hitting the same partitions the correlation
  stage is writing to, competing with ingest for exactly the resources ADR-0004
  is trying to measure.
- Panel queries are written by humans in a text box, so nothing forces them to
  include the partition key. One unbounded panel query can scan every partition.

### Revisit when

- Dashboard SQL becomes a measurable share of database load — at which point the
  fix is probably a read-only view layer or dedicated rollup tables that panels
  target instead of base tables.
- A schema migration breaks panels in a way that is annoying rather than
  trivial. That is the signal the coupling has become real.

---

## ADR-0009 — Two binaries, separated paths, no `shared` package

**Status:** Accepted · 2026-08-25

### Context

Ingest and query have opposite failure modes and opposite tuning. Ingest is
bursty, externally triggered, and *loses data* when it is slow. Query is
human-triggered, tolerant of latency, and merely *annoys* someone when it is
slow. Ingest wants a small pool with short statement timeouts; query wants a
larger pool tolerant of long analytical scans.

### Decision

Two binaries — `cmd/ingester` and `cmd/queryapi` — over two separate package
trees, `internal/ingest` and `internal/query`. Neither tree may import the
other.

The shared surface is limited to **genuinely neutral concerns**:

- `internal/config` — process knobs
- `internal/telemetry` — OTel SDK setup
- `internal/database` — connection and pool construction, no queries

Plus exactly one documented domain exception, `internal/session` (ADR-0015).

**There is deliberately no `shared`, `common`, `util` or `pkg/util` package, and
one must not be created.**

### Alternatives considered

**One binary with subcommands** — cheaper to run locally, one image to build,
one deployment to manage, and easy to split later.

*Rejected because* "easy to split later" is usually false. A single binary makes
sharing free, so read and write internals converge by default — not through any
decision, just through proximity. By the time the split is needed, it is a
refactor rather than a configuration change.

**A `shared` or `common` package** — the pragmatic middle ground.

*Rejected explicitly and on principle.* A package named for its role in the
build rather than its subject has no membership criterion, so nothing can ever
be rejected from it. It becomes the destination for code whose home nobody
wanted to decide, and it silently re-couples the two paths that the split exists
to separate. The three permitted packages above are each named for a *subject*
and each have a stated boundary in their `doc.go`.

**Separate repositories** — maximum enforcement.

*Rejected because* the seam this project studies is between media backends, not
between read and write. Two repositories would split the decision log and the
article series across boundaries that do not match the subject matter.

### Consequences

- `internal/ingest/store` and `internal/query/store` will contain similar-looking
  code. This duplication is accepted; they describe the same database but ask
  genuinely different questions of it.
- Two images, two deployments, two sets of configuration.

### Revisit when

- A fourth candidate for the neutral set appears and its neutrality has to be
  argued for. That argument is the signal the rule is being bent, and it should
  produce a new ADR rather than a quiet addition.

---

## ADR-0010 — No `pkg/` directory

**Status:** Accepted · 2026-08-25

### Context

Go's community layout conventions offer `pkg/` for code intended for external
consumption and `internal/` for code the compiler forbids outsiders from
importing.

### Decision

`internal/` only. No `pkg/` directory.

### Alternatives considered

**`pkg/` from the start, in case something becomes reusable** — the common
default in Go project templates.

*Rejected because* nothing here has an external consumer, and a `pkg/` directory
with no importers is an unrequested promise of API stability. `internal/` makes
the compiler enforce that no such promise exists, which keeps refactoring cheap
during exactly the phase when the design is still moving.

**Flat packages at the repository root** — fine for small libraries.

*Rejected because* this is a service that already has two clearly separate
subsystems (ADR-0009), and a flat root would erase that structure.

### Consequences

- If an external consumer appears, code has to move, and moving it is a breaking
  import-path change for anyone who was importing it anyway — which, by
  definition, is nobody yet.

### Revisit when

- A genuine external consumer exists: for example, publishing the adapter
  contract so third parties can write their own media-backend adapters. That is
  a plausible future and would be the right trigger.

---

## ADR-0011 — Push-based webhook ingest, not polling

**Status:** Accepted · 2026-08-25

### Context

Session analysis needs *transitions*, not state. "Who is in the room now" is not
enough to reconstruct "who joined, when, and for how long".

### Decision

Ingest is push-based: the media backend delivers lifecycle events to a webhook
endpoint the platform exposes.

### Alternatives considered

**Poll the LiveKit server API** — no public endpoint required, no signature
verification, no retry semantics to handle, and much simpler to operate.

*Rejected because* the API exposes current state rather than transitions.
Anything shorter-lived than the poll interval is invisible — and short-lived
joins, reconnects and track flaps are precisely what session analysis exists to
surface. A polling design would silently under-report exactly the events that
matter most.

**Consume LiveKit's internal Redis / message bus** — richer than webhooks, and
avoids the public-endpoint problem.

*Rejected because* it is an undocumented internal contract that can change
between minor versions, and because it is entirely LiveKit-specific: it would
generalise to no second backend at all, contradicting ADR-0003.

**Tail the media server's logs with a sidecar** — works anywhere, needs no
cooperation from the backend.

*Rejected because* it means parsing a format that is not a contract. Log output
changes without notice and carries no delivery guarantees.

### Consequences

All four of these shape `internal/ingest` and none of them are optional:

- Delivery is **at-least-once**; every event needs a stable identity so
  duplicates are idempotent.
- Arrival order is **not** event order; canonical events must carry the
  backend's own timestamp and never rely on arrival sequence.
- **Retries are backpressure.** Slow handling makes a downstream stall worse by
  multiplying inbound traffic, which is why the receiver acknowledges on durable
  receipt and correlates afterwards.
- The webhook endpoint must be reachable *from* the media backend, which is a
  networking and authentication problem in every environment.

### Revisit when

- A backend appears with no push mechanism at all. mediasoup is close to this
  case: its "push" is whatever the wrapping application chooses to send, which
  may end up looking more like a client library than a webhook.

---

## ADR-0012 — Kubernetes as the deployment target, deferred

**Status:** Deferred · 2026-08-25

### Context

Nothing is deployed anywhere yet. But the deployment target influences design
now — it is the main argument in ADR-0006 and a secondary one in ADR-0001 — so
it is worth recording even though no manifests exist.

### Decision

Kubernetes, in a later phase. No manifests, Helm chart or operator config in
this repository yet.

### Alternatives considered

**Docker Compose in production** — the stack already exists and it would work.

*Rejected because* there is no rolling-deploy story. A webhook receiver that
must not drop deliveries cannot be restarted by stopping it first, and building
connection draining by hand on top of Compose reinvents the part of Kubernetes
that is actually needed here.

**Nomad** — genuinely simpler to operate, and a good fit for a mixed workload
(the Node process from ADR-0003 would slot in cleanly).

*Rejected because* the observability tooling this project depends on — the OTel
operator, Prometheus service discovery, Grafana's Kubernetes integrations — is
substantially more mature on Kubernetes, and observability is the subject
matter, not an afterthought.

**A PaaS (Fly.io, Render, Railway)** — least operational work by far.

*Rejected because* self-hosted LiveKit needs UDP port ranges and predictable
network topology, which fits PaaS abstractions poorly, and because a planned
article is about scaling behaviour — which requires control of the substrate
being measured.

### Consequences

- Design decisions are being made in anticipation of an environment that does
  not exist yet. ADR-0006 in particular is partly justified by it, which is
  called out there explicitly.

### Revisit when

- The correlation stage needs to scale independently of the webhook receiver.
  That is the first requirement that genuinely needs an orchestrator, and it is
  the right moment to write manifests.
- Kubernetes is dropped — in which case ADR-0006 must be re-opened.

---

## ADR-0013 — MIT license

**Status:** Accepted · 2026-08-25

### Context

The repository is public and accompanies a written article series. Readers are
expected to copy from it.

### Decision

MIT, copyright Jay Nirmal.

### Alternatives considered

**Apache-2.0** — the closest call. It carries an explicit patent grant and a
contributor patent-retaliation clause, both of which MIT lacks, and it is the
prevailing license in this ecosystem — LiveKit itself is Apache-2.0, which would
make the licenses uniform across the stack.

*Rejected* on brevity. The repository exists to be read and copied from
alongside articles, and MIT's length makes "you can just use this" unambiguous
to a reader skimming. The patent argument is real; it is simply not load-bearing
for a project with no patentable technique in it.

**AGPL-3.0** — would ensure that anyone running a modified version as a service
publishes their changes.

*Rejected because* it deters precisely the commercial reader the articles are
aimed at. Engineers at companies frequently cannot even evaluate AGPL code
without a legal conversation, which is a barrier at the wrong end of the
funnel.

**Business Source License** — protects a future commercial offering.

*Rejected because* it is not open source and there is no commercial product to
protect.

### Revisit when

- A patentable technique ends up in the repository.
- A corporate contributor's legal review asks for Apache-2.0 — a common and
  reasonable request, and cheap to accommodate before there are many
  contributors.

---

## ADR-0014 — Event schema and migrations deliberately deferred

**Status:** **Superseded by [ADR-0024](#adr-0024--the-event-schema)** · 2026-08-25

> Closed 2026-08-26. The schema landed; ADR-0024 records it. This entry is kept
> unedited because the log is append-only, and because the reasoning for *not*
> guessing early is the part worth preserving.

### Context

The canonical event model is the single highest-leverage design decision in the
project. Everything depends on it: the adapter contract (ADR-0003), the
partitioning strategy (ADR-0004), the correlation logic, the query API, and the
Grafana panels (ADR-0008).

It is being designed separately and is not part of this scaffolding.

### Decision

**No schema, no migrations, and no canonical types are committed.** Every
package that will depend on them carries an explicit `TODO(scope)` marker in its
`doc.go` instead. `migrations/` contains a README and nothing else.

### Alternatives considered

**Commit a plausible starter schema and iterate** — the normal thing to do, and
it keeps momentum.

*Rejected because* a committed schema stops being provisional within a day.
Correlation code, dashboard panels and adapter translation all get written
against it, and each one raises the cost of changing it. "We'll iterate" is true
only while nothing depends on it, which is a window measured in hours.

**Generate the canonical types from LiveKit's protobuf definitions** — free,
accurate, and immediately available.

*Rejected because* it makes LiveKit's model *the* model. ADR-0003 exists
specifically to prevent the first backend's assumptions from becoming the
platform's assumptions, and this option would bake them in before the second
backend is even started.

**Schemaless: store raw payloads as JSONB and figure it out on read** — maximum
flexibility, zero upfront design.

*Rejected as the primary model* because it moves the ceiling in the wrong
direction. The fields that need partition-friendly indexes are exactly the ones
that would be buried in JSONB, which makes ADR-0004's measurement a measurement
of the wrong thing. JSONB is still likely to be used for the backend-specific
remainder after canonical fields are extracted.

### Consequences

- The repository does not do anything yet, and that is the intended state of
  this commit.
- `internal/database` has no PostgreSQL driver dependency, because a driver
  chosen before there is a query to run is a driver chosen without evidence.

### Revisit when

This ADR closes when the schema lands. At that point it should be superseded by
an ADR that records the schema decision itself — partition key, partition
interval, identity derivation, and what is canonical versus what stays JSONB.

---

## ADR-0015 — `internal/session` as the one shared domain package

**Status:** Accepted, provisional · 2026-08-25

### Context

ADR-0009 restricts the shared surface between the read and write paths to
neutral infrastructure concerns. The canonical event and session types are not
neutral — they are the domain — yet both paths need them: ingest produces them,
query reads them.

### Decision

Allow exactly one shared domain package, `internal/session`, as a **named
exception** to ADR-0009. It holds the canonical vocabulary and nothing else, and
its contents are backend-neutral by construction: if a type can only be produced
by one media backend, it belongs in that backend's adapter instead.

Recording it as an exception rather than quietly adding it is the point. An
exception with a name and a revisit trigger is a decision; an exception without
one is the beginning of a `common` package.

### Alternatives considered

**A separate write model and read model (strict CQRS)** — the orthodox answer,
and the one that would follow from ADR-0009 without an exception.

*Rejected for now* because it means two definitions of "session" while the
schema is still being designed (ADR-0014). Every iteration would have to be made
twice, plus a mapping layer, with no evidence yet that the two models want to
differ.

**Put the canonical types under `internal/ingest`, since that is what produces
them** — no exception needed.

*Rejected because* it forces `internal/query` to import `internal/ingest`, which
is exactly the coupling ADR-0009 forbids, and it would make the read path
depend on the write path's release cadence.

**Duplicate the types in both trees and map between them** — no shared package
at all.

*Rejected because* the mapping code *is* the coupling, just harder to see and
untested. Two structs that must be kept identical by convention are worse than
one struct kept identical by the compiler.

### Consequences

- There is now a precedent for domain code outside the two trees. It is bounded
  by this ADR and by the "one backend cannot own a canonical type" rule above.

### Revisit when

- The read path starts wanting fields the write path does not produce —
  denormalised rollups, computed durations, derived quality scores. That is the
  natural point to introduce a distinct read model and retire this exception.
- A second exception is proposed. Two exceptions is a `common` package with
  extra steps, and the rule should be rewritten rather than eroded.

---

## ADR-0016 — Stable participant identity is a required integration contract

**Status:** Accepted · 2026-08-26

### Context

Correlation has to know that two events describe the same participant. LiveKit
supplies two identifiers. `identity` is chosen by the caller when an access
token is minted. `sid` is assigned by LiveKit and is **new on every join** — a
participant who reconnects four times has four SIDs — so it cannot answer "is
this the same person as before".

That leaves `identity`, which is caller-supplied and therefore only as good as
the integrator's discipline.

### Decision

Stable identity is a **contract the integrator must meet**, not a problem the
platform solves. Callers must supply a participant identity that is:

- stable for a given user within a room, across reconnects, and
- never reused for different users.

The platform assumes the contract and does not attempt to verify it by
inference.

### Alternatives considered

**Heuristic correlation when identity is unstable** — infer sameness from source
IP, user agent, the timing proximity of a leave and a join, or track
fingerprints. This is what a system that wants to "just work" against a sloppy
integration does.

*Rejected because it makes the system's central claim unverifiable.* Such
correlation is wrong some unknown fraction of the time, and there is no ground
truth to measure that fraction against — if we could tell which pairings were
correct, we would not have needed the heuristic. This platform exists to say
"this session had three reconnects". A heuristic turns that into "this session
had three reconnects, probably, with an error rate we cannot state". A number
you cannot put an error bar on is not a measurement.

**A fallback heuristic mode, used only when identity looks unstable** — keep the
strict path for good integrations, degrade gracefully for bad ones.

*Rejected, and this is the more tempting of the two.* Two correlation modes mean
every result carries an invisible "how was this derived" caveat. A dashboard
panel cannot show it, a query result does not carry it, and the person reading
the number does not know which mode produced it. Worse, the mode is per-room and
can change over time, so a single chart can silently mix both. One correlation
rule that sometimes refuses to produce an answer is better than two rules that
always produce an ambiguous one.

**Mint the tokens ourselves so we control identity** — put the platform in the
token path and derive identity there.

*Rejected because* it puts an observability tool into the media backend's
authentication path, which is a far larger integration than observing it. It
also does not generalise: mediasoup has no token concept to intercept
(ADR-0003).

### The principle

**Push ambiguity to the boundary rather than absorbing it.**

Unstable identity is an integrator bug. At the boundary it is visible,
attributable, and fixable at the point where tokens are minted — a small local
change by someone with the context to make it. Absorbed into the platform, the
same bug becomes a permanent distortion in every downstream query, invisible to
everyone and fixable by no one.

### Consequences

- Violations are **surfaced, never repaired.** The write path detects and counts
  suspected-unstable-identity patterns and emits them as a metric. It must never
  attempt to fix them.
- The counter is a *suspicion*, not a finding, and should be named like one. We
  can be wrong about it in both directions.
- Integrations that cannot supply stable identity produce data the platform will
  describe accurately as unusable, rather than data it quietly guesses at.

TODO(scope): the detection heuristic and its metric. Detecting a contract
violation and repairing one are different acts; only the first is in scope, and
the distinction is the entire point of this entry.

### Revisit when

- A backend appears with no caller-supplied identity at all, so the contract
  cannot be stated in its terms. mediasoup is the likely candidate.
- The suspicion metric fires broadly across integrators who are demonstrably
  trying to comply, which would mean the contract is unstatable in practice
  rather than merely unmet.

---

## ADR-0017 — No cross-room canonical identity

**Status:** Rejected · 2026-08-26

### Context

The same participant identity can appear in two different rooms. It is natural
to ask whether the platform should resolve those into one canonical user with a
cross-room history.

### Decision

No. The correlation key is `(room, identity)`. The same identity in two rooms
represents two separate things and is stored as two separate things. No
canonical user entity exists.

### Alternatives considered

**Build a canonical user across rooms** — a `user` table, identity as an alias
of it, sessions hanging off the user.

*Rejected because it is a product question about users, not a systems question
about sessions.* What counts as "the same user" depends entirely on the
deployment. In one, identity is an authenticated account id and the answer is
trivial. In another it is a display name and the answer is wrong. In a third,
the same human deliberately uses different identities in different rooms. The
platform has no basis on which to choose, and choosing wrongly is worse than not
choosing: it produces a canonical entity that silently merges different people
and offers no signal that it has done so.

The cost of declining is low. **Anyone wanting cross-room analysis already has
identity as a join key** and can write the query. Not building the entity does
not remove the capability; it removes a pre-commitment to one definition of it.

**A nullable `user_id` populated from an integrator-supplied mapping** — let each
deployment answer the question for itself.

*Rejected as premature rather than wrong.* It is the likely shape of the answer
if an answer is ever needed. Adding it later is an additive migration; adding it
now is a column nothing populates and a join nothing uses.

### Consequences

- Cross-room questions are answerable, but as ad-hoc SQL rather than as a
  first-class dimension.
- Identity uniqueness is scoped per room, which is precisely the property
  ADR-0018 relies on when it observes that tenancy would scope identity
  *further* rather than restructure anything.

### Revisit when

- **A concrete query we cannot answer without it.** Not "cross-room users would
  be nice" — an actual question, written down, that identity-as-a-join-key
  cannot express, or cannot answer at acceptable cost.

---

## ADR-0018 — Multi-tenancy deferred

**Status:** Deferred · 2026-08-26

### Context

Multi-tenancy was previously carried as an open question, with a note that it
affects partitioning and could not stay open long. This entry closes it as an
explicit deferral rather than leaving it ambient.

### Decision

No tenant concept. No `tenant_id` column, no row-level security, no per-tenant
partitioning.

The significant part is not the deferral but the reason it is safe: **nothing in
the current design is hostile to adding one later.** Identity uniqueness is
already scoped per room (ADR-0017). A tenant dimension would scope it *further*
— `(tenant, room, identity)` — which narrows an existing key rather than
restructuring anything. The correlation model, the join model and the index
shapes all survive that change; the migration would be additive plus a backfill
of a constant.

This is a deferral with a checked precondition, not a deferral by omission.

### Alternatives considered

**Add `tenant_id` now with a single default value** — costs almost nothing and
avoids a backfill later.

*Rejected because* "costs almost nothing" is how a schema accumulates columns
nothing reads. Every query would carry a predicate that is always true, and
every index would carry a leading column with exactly one distinct value — which
is actively harmful rather than merely useless, since a constant leading column
makes a btree index worse. A column that is present but unenforced also invites
code that assumes it means something.

**Schema-per-tenant or database-per-tenant** — strong isolation.

*Rejected for now*, with a warning attached: this is the option most damaged by
delay. It is a deployment topology rather than a column, and retrofitting it is
genuinely hard. If isolation requirements arrive, re-open this decision *before*
the row-level option gets chosen by default.

**Row-level security on a tenant column** — the conventional answer.

*Not rejected, merely not yet needed.* It is the likely shape if tenancy arrives
as an application requirement rather than a compliance one.

### Consequences

- The platform is single-tenant. Serving more than one customer means running
  more than one of it.
- ADR-0024's partitioning is decided without a tenant dimension. If tenancy
  arrives, partitioning by time stays correct and tenant becomes a leading index
  column — not a partition key, unless isolation demands otherwise.

### Revisit when

- A deployment must serve more than one customer from one instance.
- A compliance requirement demands data isolation, in which case start from the
  schema-per-tenant option rather than the column.

---
## ADR-0019 — The join is the durable unit; the session is a view

**Status:** Accepted · 2026-08-26

### Context

"Session" is the word in the project's name, so it is worth being exact about
what it denotes. Three candidate definitions:

1. the room's lifetime;
2. a participant's presence in a room, delimited by the backend's per-join
   identifier;
3. a participant's presence in a room, spanning reconnects.

Definition 3 is what a person means when they ask "how was that call?".

### Decision

Store **joins**. A join is one participant's presence in one room, from an
observed start to an observed end. Reconnects produce separate joins, stored
separately, **with the gaps between them intact**.

A **session** — definition 3 — is derived at read time by grouping a
participant's joins under a reconnect-gap threshold. The threshold is a query
parameter with a configured default.

The session is never stored.

### Alternatives considered

**Session = room lifetime.**

*Rejected* on two counts. It averages away individual experience: a room in
which one participant had a flawless hour and another reconnected nine times
becomes one "session" with mediocre aggregate numbers, and those two experiences
are exactly what the platform exists to distinguish. And the boundary is
arbitrary in practice — rooms are frequently long-lived or reused across
unrelated conversations, so "the room's lifetime" often does not correspond to
anything a participant would recognise.

**Session = participant-in-room delimited by the backend's per-join identifier
(LiveKit's SID).**

*Rejected*, and it is the most tempting option, because it is unambiguous: the
backend hands you the boundary and no policy is required. It is rejected because
that unambiguity is purchased by discarding the reconnect relationship. A user
who reconnected four times becomes four sessions, each of which looks fine. The
one number that describes their actual experience — dropped four times in twenty
minutes — is not merely absent from the data, it is contradicted by it.

**Apply the threshold at write time, hardcoded.**

**Apply the threshold at write time, configured.**

*Both rejected, and configuration does not fix the problem.* Either way a policy
value is baked into stored rows. When the value changes — and it will, because
thirty seconds is a guess — prior data remains stitched under the old rule with
no marker recording which rows were produced under which value. The table then
holds two incompatible interpretations of the same column with no way to
separate them.

This is the same class of error as ADR-0004's reasoning about the store: **it
destroys the ability to ask the question later.** Once joins are merged the gaps
are gone, and you cannot un-merge them.

### The tension, stated plainly

This pushes work to read time, which sits directly against the argument in
ADR-0011 and `internal/ingest/webhook` that correlation belongs at write time.
The two are not in conflict, but the boundary between them is load-bearing and
later work depends on it being stated precisely:

> **Correlating events into a coherent record is deterministic and belongs at
> write time. Applying a policy threshold to group records is interpretive and
> belongs at read time.**

"These two events describe the same join" has one right answer, derivable from
the events themselves, and recomputing it on every read would be waste. "These
two joins are the same session" has no right answer independent of a chosen
threshold, and freezing one choice destroys every other.

The test to apply to any future pipeline stage: *if two reasonable people could
choose different values and both be right, it is policy — and policy does not go
in the table.*

### Consequences

- "How would these group at 30 seconds versus 120?" is a query against real
  data, answerable on history from day one, rather than a migration and a wait.
- Every session-shaped result must carry the threshold that produced it or it is
  uninterpretable. `session.Session` carries a `Gap` field for exactly this
  reason.
- Read-time cost is higher: the grouping is a window function over the overlap
  result on every query. ADR-0024 measures what that costs.

### Revisit when

- Read-time grouping becomes the dominant cost in the reference query and a
  materialised grouping is needed. If so, materialise it **beside** the joins
  under a named threshold, never in place of them.
- A backend supplies the reconnect relationship directly, making the grouping
  observed rather than inferred. That would move it to write time legitimately,
  because it would no longer be policy.

---

## ADR-0020 — `ended_at` is nullable and only ever set from an observed event

**Status:** Accepted · 2026-08-26

### Context

A join opens when a `participant_joined` is observed and closes when a
`participant_left` is observed. Sometimes the second never arrives: the delivery
is lost, the backend crashes, the network partitions. The obvious fix is a
sweeper that closes joins older than some timeout.

### Decision

**No sweeper.** `ended_at` is nullable and is only ever written from an observed
event. A NULL `ended_at` means *"still open, or we never found out"* — one
honest state, not an error condition.

`end_reason` records **how** the end was learned: `participant_left`,
`room_finished`, or `inferred_timeout` (reserved, and currently never written).

### Why the null is worth more than a guess

A sweeper collapses the world into two states: ended, and not yet ended.
Declining to sweep yields three:

1. **Open and recent** — probably a live participant.
2. **Open and stale** — an event was lost. This is a direct measurement of
   delivery-path loss.
3. **Closed, with a reason** — we know what happened, and how we learned it.

A sweeper erases the middle case by converting it into the third with a
fabricated timestamp. That case is the single most useful signal the write path
produces about its own reliability, and it belongs on a dashboard rather than
under a rug. ADR-0011 accepted that delivery is at-least-once and unordered;
state 2 is how we find out what that actually costs in production.

Distinguishing states 1 and 2 is a matter of *staleness*, which the reader
computes from `started_at` with a cutoff appropriate to their question — the
same reasoning as ADR-0019 and ADR-0021.

### The exception, which is not inference

`room_finished` closes every join still open in that room, with
`end_reason = 'room_finished'`.

This is not a timeout and not a guess. The backend is stating that the room is
gone, and a participant cannot still be in a room that does not exist. The
information is observed; it simply arrives about the room rather than about the
participant.

It stays a distinct `end_reason` rather than being folded into
`participant_left` because the two carry different information. "Left" says
something about the participant's behaviour. "Closed because the room ended"
says nothing about the participant at all — and treating the second as the first
would record a departure for someone who never departed, which is precisely the
fabrication this entry exists to prevent.

### Alternatives considered

**A sweeper with a generous timeout** — close anything open for more than, say,
24 hours.

*Rejected* per the above. A generous timeout does not reduce the harm, it delays
it: the fabricated row is just as fabricated a day later, and the measurement is
just as erased.

**A sweeper that writes `inferred_timeout` honestly** — mark the fabrication
rather than hiding it.

*Rejected, and this is the closest call in this entry.* It is genuinely better
than a silent sweeper, and it is why the constant is reserved rather than
absent. It is still rejected because it makes swept rows *look answered*. Every
downstream query would then have to remember to exclude one `end_reason`, and
the queries that forget would be silently wrong. A NULL cannot be forgotten — it
forces the reader to decide what to do about it.

**A separate `join_closed_by_timeout` table** — keep the observations clean and
record inferences elsewhere.

*Rejected as* the same thing with an extra join.

### Consequences

- Open joins accumulate for any integration with a lossy delivery path, without
  bound if the loss is systemic. That is a true statement about that deployment,
  and it should be alarming.
- The overlap index treats an open join as `[started_at, ∞)`, so a large
  population of stale open joins is also the pathological case for the primary
  query's performance. ADR-0004's revisit trigger accounts for this explicitly.
- `inferred_timeout` exists in the CHECK constraint and in the Go vocabulary but
  is never written. If it ever is, this ADR is being reversed and should be
  superseded first.

### Revisit when

- Stale open joins reach a share of the table that materially degrades the
  reference query, *and* the cause is understood and unfixable at source. Even
  then, prefer archiving them to closing them.

---

## ADR-0021 — No settling window at write time

**Status:** Accepted · 2026-08-26

### Context

Events arrive late and out of order (ADR-0011), so a correlated record is never
final — only settled. The usual mechanism is a settling window: hold a record
mutable for N minutes, then treat it as final.

### Decision

No settling window at write time. Records are updated whenever a relevant event
arrives, however late. Queries that want only settled data specify their own
recency cutoff as a parameter.

**The window is a property of the question, not of the data.** A dashboard
showing the last 24 hours wants a different cutoff than a monthly report, and
neither is more correct than the other. Same reasoning as ADR-0019's gap
threshold.

### Alternatives considered

**A fixed settling window, after which records are frozen** — clean semantics,
and it makes "is this final?" answerable from the row itself.

*Rejected* for ADR-0019's reason: it bakes a policy value into stored state, and
changing it leaves earlier data settled under the old rule with no way to tell
which. It is in one respect worse than the gap-threshold case, because a late
event arriving after the window would be *dropped* — choosing to lose real data
in exchange for a semantic convenience.

**A settling window that marks rather than freezes** — set a `settled_at` and
keep accepting updates.

*Rejected as* a column recording when we stopped paying attention, which no
query needs. A reader wanting "records unlikely to change further" can express
that as `started_at < now() - cutoff` from data already present.

**Watermarks, as a stream processor would do it** — genuinely the rigorous
answer to late data.

*Rejected here because* watermarks are a property of a streaming runtime we
deliberately do not have (ADR-0004 rejected Kafka plus a stream processor as
premature). Approximating them in the database buys the complexity without the
guarantees.

### Consequences

- Any query over recent data may see records that later change. Callers must
  choose a cutoff deliberately; there is no safe default that relieves them of
  the choice.
- A late event can modify a record a dashboard has already displayed. This is
  correct behaviour and should be stated on the dashboard, not engineered away.
- Reproducing a past query result requires knowing both the time range and the
  time the query was run.

### Revisit when

- A consumer genuinely requires immutable historical records — a billing or
  compliance export. The answer then is a snapshot table with an explicit as-of
  timestamp, not a settling window on live data.

---
## ADR-0022 — LiveKit event ingest scope; room lifecycle stays raw-only

**Status:** Accepted · 2026-08-26

### Context

LiveKit emits eleven webhook types. Not all of them describe participant session
experience, and the platform has to decide what it accepts.

### Decision

**Ingested and stored** — `room_started`, `room_finished`, `participant_joined`,
`participant_left`, `track_published`, `track_unpublished`.

**Rejected at the boundary, and counted** — `egress_started`, `egress_updated`,
`egress_ended`, `ingress_started`, `ingress_ended`.

**Unrecognised types are stored, not dropped.**

**Room lifecycle events live only in `event_raw`. There is no `room` table.**

### Why egress and ingress are rejected

They describe recording, streaming and media injection — operations performed
*on* a room, not experiences *of* a participant. No planned query reads them.

The cost of storing them anyway is not merely disk. `event_raw` is the table
whose ceiling ADR-0004 exists to measure. Padding it with events no query
touches would make that measurement describe a workload we do not have, and the
resulting number would be wrong in the direction that matters most — it would
make plain PostgreSQL look worse than it actually is for the real job.

The rejection is counted by type so the decision stays visible and reversible
with evidence rather than from memory.

### Why unrecognised types are stored anyway

This looks inconsistent with the paragraph above, and the distinction is
deliberate: **known-and-irrelevant is a decision; unknown is a surprise.**

An unrecognised event type means the integration drifted — a LiveKit upgrade
added something, or a mediasoup wrapper is emitting a shape nobody planned. That
is exactly the moment the payload is worth having. A counter alone would tell us
that a type appeared while destroying the only evidence of what it contained.
Retaining it is the same instinct as ADR-0016's *surface, do not repair*.

*Rejected alternative:* reject unknown types and count them by name. Cleaner
intake, and it would keep `event_raw` to a predictable six types. Rejected
because the first time that counter fires, the only useful question is "what was
in it?" and the answer would be gone.

### Why room lifecycle stays raw-only

`room_finished` is read during correlation to close open joins (ADR-0020). That
is a lookup against the event stream, not a durable aggregate anything queries
directly.

*Rejected alternative:* a `room` table with started and finished timestamps.
Rejected because it would be a second durable entity carrying the same
open/closed problem the join model already solves — and rooms are the entity
ADR-0019 specifically rejected as a session boundary, for being long-lived and
reused. Promoting them to a table invites exactly the aggregate-by-room analysis
that decision rejects.

### Consequences

- Room-level reporting must be aggregated from joins, or read from raw events.
- `event_raw` carries a small tail of unrecognised events, unbounded in *type*.
  It is intake we do not control and should be monitored by type cardinality.

### Revisit when

- A query needs room attributes that cannot be derived cheaply from events.
- Recording quality becomes part of "how was that session", at which point
  egress events become session-relevant and this scope should be widened
  deliberately rather than by accident.
- The unrecognised-type tail stops being a tail.

---

## ADR-0023 — golang-migrate as the migration tool

**Status:** Accepted · 2026-08-26

### Context

ADR-0014 deferred the choice along with the schema. The schema now exists
(ADR-0024), and it needs a way to be applied and rolled back.

### Decision

`golang-migrate`, run as a container. Migrations are plain `.sql` files in
paired `up`/`down` form under `migrations/`.

They are applied automatically by a one-shot `migrate` service in docker
compose, which runs once PostgreSQL is healthy and before either binary starts.
`make migrate-up`, `migrate-down`, `migrate-reset`, `migrate-version` and
`migrate-force` provide manual control.

### Alternatives considered

**goose** — supports Go migrations as well as SQL, which would let partition
creation be written as a loop.

*Rejected, and the reason is the interesting one.* That capability is a trap
here. Partition creation is **recurring maintenance**, not schema change: it
must run every day forever, not once per deploy. Writing it as a migration would
make it *look* handled while quietly leaving the real obligation unmet.
golang-migrate's inability to express it enforces the boundary correctly — the
job has to live somewhere else, which is where it belongs.

**Atlas** — declarative schema-as-code, with real diffing and migration linting.

*Rejected as* heavier than this project currently needs, and partly commercial.
It is the strongest option if the schema grows enough that hand-written diffs
become error-prone, and it is the first place to look if that happens.

**sqitch** — dependency-ordered rather than linear, with excellent reversibility
discipline.

*Rejected on* its Perl runtime and its unfamiliarity to the likely reader of
this repository.

**No tool at all; a hand-applied `schema.sql`** — briefly tempting for a project
this young.

*Rejected because* the reversibility requirement is real. This schema will be
revised repeatedly as ADR-0024 is revisited, and being able to roll back and
re-apply cleanly is what keeps that cheap.

### Consequences

- **The dirty-state behaviour is genuinely annoying**, and worth warning about
  rather than discovering: a migration that fails part-way leaves the version
  table marked dirty and refuses to proceed until `migrate force <version>`
  clears it — which requires knowing what actually applied.
  `make migrate-force VERSION=n` exists for this and should be used with the
  schema in front of you, never reflexively.
- No Go dependency is needed to apply migrations, so `internal/database` stays
  driver-free until there is a query to run. ADR-0014's reasoning on that point
  survives its supersession.
- Partition maintenance is **not** covered by this tool, by design. TODO(scope).

### Revisit when

- Hand-written diffs start producing migration bugs, at which point Atlas's
  diffing earns its weight.
- Migrations need to be applied by the application itself rather than a sidecar
  — for example from a Kubernetes init container (ADR-0012).

---

## ADR-0024 — The event schema

**Status:** Accepted · 2026-08-26 · **Supersedes ADR-0014**

### Context

ADR-0014 deliberately deferred the schema so that no plausible-looking guess
would be committed and built upon. This entry closes it.

The decisions here are consequences of ADR-0016 through ADR-0022, not
independent choices. Read those first; this is the shape they imply.

### Decision

Two tables.

**`event_raw`** — append-only intake, partitioned daily by `occurred_at`. Typed
columns for the correlation keys, `payload jsonb` for everything else.

**`participant_join`** — the durable unit (ADR-0019). Not partitioned.

#### Partition key and granularity

`occurred_at` — the backend's own clock, not arrival time, because arrival order
is not event order (ADR-0011) and nothing may order by arrival.

**Daily.** Retention is measured in weeks, so daily granularity lets retention be
expressed to the day, keeps partition count near 60 at an eight-week window, and
prunes tightly for the drill-down query, which is typically scoped to hours.

- *Weekly rejected* — carries up to six extra days past the retention boundary
  and prunes far more coarsely.
- *Monthly rejected* — incoherent at weeks-scale retention.
- *Hourly rejected* — roughly 1,300 partitions at eight weeks, where planning
  overhead starts to matter and the maintenance job's failure modes multiply, in
  exchange for pruning finer than any expected query needs.

#### No DEFAULT partition

A default partition would prevent insert failures when the maintenance job falls
behind. *Rejected:* it silently absorbs misrouted rows, and every later `ATTACH`
must scan it. ADR-0004 already accepted that forgetting the maintenance job is
an outage; a default partition converts a loud outage into quiet wrongness,
which is the trade this project consistently refuses (ADR-0016, ADR-0020).

#### Why `participant_join` is not partitioned

Partitioning it by `started_at` would defeat pruning on the primary query. A
join that started long ago and is still open (ADR-0020) must be found by any
later time window, so an overlap search could never prune below "every partition
up to the range end" — unless a second predicate bounded `started_at`, and
adding that predicate would silently drop long-running sessions from results.

The two tables get different treatment because they are queried differently, not
because one matters more.

#### Idempotency

On `event_raw`: the primary key `(occurred_at, event_id)`, where `event_id` is
**derived, never copied**. A duplicate delivery derives the same id and
conflicts, so the receiver uses `ON CONFLICT DO NOTHING` and needs no dedup
table. A partitioned table's unique constraints must include the partition key,
which is why the PK is a pair.

Idempotency is load-bearing for ADR-0011, so the derivation inputs are stated
exactly rather than left to the implementer.

**Primary path — the backend supplies its own event id.** LiveKit does, as
`WebhookEvent.id`:

```
event_id = uuidv5(NAMESPACE_SAP, "v1" ‖ backend ‖ backend_event_id)
```

The backend guarantees uniqueness; nothing else is needed.

**Fallback path — no native event id.** mediasoup will not have one (ADR-0003):

```
event_id = uuidv5(NAMESPACE_SAP, "v1" ‖ backend ‖ event_type ‖ room_sid|room_name
                                ‖ participant_identity ‖ participant_sid
                                ‖ track_sid ‖ occurred_at(RFC3339, nanoseconds)
                                ‖ delivery_ordinal)
```

Four constraints on that construction, each of which is a defect if omitted:

- **`‖` is a length-prefixed join, not a separator.** With a naive delimiter,
  `("a", "bc")` and `("ab", "c")` produce the same input and therefore the same
  id. Each field is emitted as its byte length followed by its bytes.
- **`track_sid` must be included.** Without it, a participant publishing audio
  and video in the same millisecond produces two `track_published` events
  differing only in track, which would derive one id and silently collapse into
  a single row. This is the concrete case the earlier wording — "the canonical
  tuple" — left open, and it would have been a real data-loss bug.
- **`delivery_ordinal`** — the event's index within its delivery batch — is the
  last-resort discriminator for backends that can emit two genuinely distinct
  events with no distinguishing field at the same instant. It is stable across
  retries of the same delivery, which is what makes it usable as identity rather
  than a nonce.
- **`"v1"` prefixes every input** so the derivation scheme can be changed later
  without silently re-identifying historical events.

**The payload is deliberately not hashed**, and this is the decision most worth
recording, because hashing it is the obvious shortcut and it fails in *both*
directions:

- *Collapse.* Two genuinely distinct events with byte-identical payloads and
  timestamps become one row. Real data is lost, silently.
- *Split.* One event redelivered with a payload that differs immaterially —
  re-serialised JSON with different key order, an added retry counter, a
  server-side timestamp — derives a **different** id and is stored twice.
  Idempotency fails open, which defeats the entire purpose of deriving the id.

The second failure is the worse one and the harder to notice, because nothing
errors: the table just accumulates duplicates that every downstream count
believes are real.

On `participant_join`: a unique constraint on `started_event_id`.

*Rejected:* a partial unique index on
`(backend, room_name, participant_identity) WHERE ended_at IS NULL`, to prevent
duplicate open joins. It is wrong. When a `participant_left` is lost the join
stays open (ADR-0020), and that index would turn the participant's **next
legitimate join** into an insert failure — converting a measurement into an
outage. Multiple open joins for one `(room, identity)` are permitted, and
counted as a metric.

#### `active_range` and the overlap index

`participant_join.active_range` is a generated
`tstzrange(started_at, ended_at, '[)')`. An open join becomes `[started_at, )` —
unbounded above — so it overlaps any later window with no NULL handling at the
call site.

*Rejected:* btree indexes on `started_at` and `ended_at` with an explicit
`started_at < $2 AND (ended_at IS NULL OR ended_at > $1)` predicate. Correct,
but the NULL branch is easy to omit, and omitting it silently excludes exactly
the open joins ADR-0020 exists to make visible. Encoding the semantics in the
type removes the opportunity to get it wrong.

#### Indexes, and what each measurably earns

Measured on PostgreSQL 16.15, 200,000 joins across 4,000 rooms, ~5% open,
warm cache, median of nine runs. Reproduce with `make bench`; the harness is
[`benchmarks/`](benchmarks) and its caveats are in
[`benchmarks/README.md`](benchmarks/README.md).

| Index | Serves | Measured |
|---|---|---|
| `participant_join_active_range_idx` (GiST) | overlap filter in the reference query | bitmap index scan; 11,538 rows from a 2-hour window; 115 ms total |
| `participant_join_window_idx` (btree) | per-`(room, identity)` drill-down | index scan, 0.22 ms |
| `participant_join_open_idx` (partial btree) | the open-and-stale panel (ADR-0020) | index-only scan, 0 heap fetches, 2.0 ms for 10,000 open joins |
| `event_raw_pkey` | partition pruning; idempotent insert | — |
| `event_raw_participant_idx` | per-session drill-down into raw events | — |

`event_raw` gets only two indexes: it is the append path ADR-0011 requires to
stay fast, and every index is paid on every insert.

#### Measurement history for `participant_join_window_idx`

This index's justification was wrong twice before it was right. The history is
kept in full because an ADR that shows a justification failing is worth more
than one that looks clean.

**The original claim (2026-08-26).** The index was chosen on the reasoning that
it would supply the reference query's `PARTITION BY (backend, room_name,
participant_identity) ORDER BY started_at` in index order, so the `lag()` that
computes reconnect gaps would need no sort.

**First refutation, same day.** `EXPLAIN ANALYZE` showed an explicit `Sort` node
in the plan. The claim was recorded as refuted and the index re-justified on
drill-down alone. That correction was directionally right but incomplete: it
asserted the sort was unavoidable without testing whether it was.

**The measurement itself was then invalidated.** The seed data used
`LATERAL (SELECT now() - random() * 14 * interval '1 day')` with no correlation
to the generated row, so PostgreSQL evaluated it **once**: all 200,000 rows
shared a single identical `started_at` (`count(DISTINCT started_at) = 1`). The
tell was visible in the original plan and went unexamined — the 2-hour window
returned exactly 10,000 rows, precisely the open-join count, with *zero* closed
joins matching. Every number in the first version of this section, including
the 66 ms figure and the 39 ms attributed to the sort, was measured against
degenerate data and has been discarded.

`benchmarks/seed.sql` keeps the broken version in a comment alongside the fix,
and `run.sh` now refuses to report timings when the seed fails
`benchmarks/verify_seed.sql` — fewer than 90% distinct `started_at`, or zero
closed joins matching the window. Both conditions were true of the original run
and neither was checked. The general lesson, which is about noticing a
suspiciously round number rather than about `LATERAL`, is in
[`benchmarks/README.md`](benchmarks/README.md).

**The definitive experiment.** On corrected data (200,000 distinct
`started_at`), three plans for the same query:

| | Access path | Sort? | Execution |
|---|---|---|---|
| A — default | GiST bitmap index scan | **yes** | 96 ms |
| B — GiST dropped | parallel sequential scan | **yes** | 107 ms |
| C — GiST dropped, `enable_seqscan = off` | `Parallel Index Scan using participant_join_window_idx` | **no Sort node** | 112 ms |

**The corrected reasoning.** The sort is *not* inherent to the query shape. Test
C shows the btree can supply the ordering — the `Sort` disappears entirely,
replaced by a `Gather Merge` over index-ordered streams. The planner declines
that path because scanning 200,000 index entries and filtering down to ~11,500
costs more than bitmap-scanning 11,500 and sorting them, and the planner is
right by about 16%.

The two access paths are mutually exclusive **because they are indexes on
different columns**: selective filtering (GiST on `active_range`) or free
ordering (btree on the window key), never both. Choosing the selective path
means accepting the sort. That is a cost trade-off the planner re-evaluates per
query, not a missing index.

So `participant_join_window_idx` earns its place on per-`(room, identity)`
drill-down (0.22 ms, and nothing else serves it), **not** on the reference
query. The sort in the reference query is the accepted price of the selective
path, and it should stop being treated as a defect to engineer away.

#### How the reference query scales with the open-join fraction

Open joins are `[started_at, ∞)` and therefore overlap **every** later window.
A row count alone does not describe this workload; the open fraction does most
of the work. Measured on PostgreSQL 16.15, 2-hour window, warm cache, median of
nine runs, row count held constant at 200,000:

| open % | matched | of which open | of which closed | median | min–max |
|---|---|---|---|---|---|
| 1% | 3,447 | 1,981 | 1,466 | 43 ms | 31–148 |
| 5% | 11,538 | 10,116 | 1,422 | 115 ms | 98–212 |
| 10% | 21,402 | 20,079 | 1,323 | 202 ms | 165–378 |
| 30% | 60,712 | 59,661 | 1,051 | 552 ms | 503–696 |

The plan shape is identical at every fraction — GiST bitmap index scan
throughout. Nothing degrades qualitatively; the result set simply grows.

**The closed-row count barely moves** — roughly 1,000 to 1,500 across the whole
range, because that is the population genuinely active in a 2-hour window. Every
additional matched row comes from the open population. At 30% open, 98% of the
result set is open joins, which is to say 98% of the query's cost is being spent
on joins whose ends were never observed.

Two larger data points, at 1,000,000 joins:

| rows | open % | matched | median |
|---|---|---|---|
| 1M | 5% | 56,962 | 821 ms |
| 1M | 10% | 106,411 | 1,216 ms |

**The model.** Matched rows are predicted to within 8% across all six
measurements by

```
matched ≈ total_rows × (open_fraction + window_span / data_span)
```

where the second term is the fraction of closed joins the window catches — here
2 hours over 14 days, about 0.6%. Execution time is close to linear in matched
rows, at roughly 10 µs per row at 200,000 and 12–14 µs at 1,000,000, the
difference being heap locality.

This model is what makes ADR-0004's revisit trigger expressible as a pair rather
than a row count. It is a local fit on synthetic data, not a law, and should be
re-derived once real ingest exists.

Reproduce the whole table with `make bench`, or a single point with
`ROWS=1000000 FRACTIONS=0.10 make bench`. Run-to-run variance on a laptop is
appreciable — the shape across fractions is the durable result, not the absolute
milliseconds.

#### What is canonical, and what stays JSONB

**Canonical** (typed columns): `backend`, `event_type`, `occurred_at`,
`room_name`, `room_sid`, `participant_identity`, `participant_sid`, `track_sid`.
These are exactly what correlation keys on.

**JSONB**: everything else, retained in full, because mediasoup's events will not
share LiveKit's shape (ADR-0003) and a column set fitted to LiveKit would make
LiveKit's model the canonical model — the failure ADR-0014 named explicitly.

The boundary rule: **a field becomes a column when a query filters or joins on
it, not when it looks important.** Promoting a field later is an additive
migration; demoting one is not.

#### Two error shapes the receiver must handle, with their exact signatures

Both were captured against the running database rather than assumed, because
both are easy to get wrong in ways that only show up in production.

**A missing partition is a signal, not a crash.** `occurred_at` is the backend's
clock and there is no `DEFAULT` partition, so a late or clock-skewed event whose
target partition does not exist raises:

```
SQLSTATE 23514: no partition of relation "event_raw" found for row
DETAIL: Partition key of the failing row contains (occurred_at) = (...)
```

That failure is the behaviour this ADR chose deliberately. It must be **counted
and surfaced, not merely thrown**, or a clock-skewed integrator arrives as a
crash loop instead of as a number on a dashboard.

**SQLSTATE 23514 alone is not a sufficient test.** An ordinary CHECK constraint
violation uses the same code. The two are distinguished by whether the error
carries a constraint name — a CHECK violation populates it, a partition miss
leaves it empty:

```
partition miss    → SQLSTATE 23514, constraint name empty
CHECK violation   → SQLSTATE 23514, constraint name set (e.g. participant_join_end_together)
```

The metric is `sap_ingest_partition_missing_total`, labelled by backend, and it
belongs on the same dashboard as the stale-open-join panel from ADR-0020. Both
measure the same thing from different angles: the gap between what the backend
believes it sent and what we were able to record.

TODO(scope): the counter has no emitter yet, because nothing inserts. The
detection predicate above is the contract; it is recorded here and in
`internal/ingest/store` so the receiver implements it correctly rather than
rediscovering it.

**`participant_join_end_after_start` never fires.** Inserting `ended_at <
started_at` fails first at the generated `active_range` column, with a different
error class entirely:

```
SQLSTATE 22000: range lower bound must be less than or equal to range upper bound
```

The CHECK is kept — it states the invariant explicitly in the DDL, where someone
reading the schema will see it — but it is unreachable in practice, and the
receiver must handle the opaque 22000 range error rather than expecting a named
constraint violation. Recorded because a constraint that cannot fire looks like
protection and is not.

### Consequences

- Partition maintenance is now a live obligation. Until the recurring job
  exists, the bootstrap window in migration `000002` is finite and inserts past
  its end **will fail**. TODO(scope).
- Retention remains undecided. Daily granularity assumes weeks and would want
  revisiting if the answer turned out to be months.
- The reference query's cost scales with the open-join population, because open
  joins are unbounded ranges.
- `internal/database` still has no driver, because there is still no query to
  run. Correlation and the query API remain out of scope.

### Revisit when

See ADR-0004, whose revisit trigger is now stated in terms of this schema's
reference query rather than generic row counts.

---
## Open questions

Not yet decisions. Listed so they are not mistaken for oversights.

Four entries left this list on 2026-08-26: session-end semantics became
ADR-0020, the settling window became ADR-0021, canonical identity became
ADR-0017, and multi-tenancy became ADR-0018.

- **Correlation trigger.** Does the pipeline run on a schedule, on a queue, or
  incrementally per event? Now unblocked by ADR-0024 and the next thing to
  decide.
- **Partition maintenance mechanism.** Partitions must be created ahead of time
  or inserts fail (ADR-0024), and ADR-0023 deliberately keeps this out of the
  migration tool. Whether it is `pg_cron`, a Kubernetes CronJob, or a loop in
  the ingester is undecided — but it is an obligation now, not a future one.
- **The default reconnect gap.** ADR-0019 makes the threshold a query parameter
  with a configured default. The default's *value* is a guess nobody has
  justified yet, and picking it deserves data the platform can now collect.
- **Retention policy.** ADR-0004 assumes weeks and ADR-0024's daily partitioning
  is sized for that. The actual number is a product decision still unmade.
