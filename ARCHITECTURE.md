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
| [ADR-0014](#adr-0014--event-schema-and-migrations-deliberately-deferred) | Event schema and migrations deliberately deferred | Open |
| [ADR-0015](#adr-0015--internalsession-as-the-one-shared-domain-package) | `internal/session` as the one shared domain package | Accepted, provisional |

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

Any of the following, **measured rather than suspected**:

- p95 for a bounded-range aggregate exceeds ~2s at the target event volume, with
  correct indexes and confirmed partition pruning.
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

**Status:** Open · 2026-08-25

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

## Open questions

Not yet decisions. Listed so they are not mistaken for oversights.

- **Correlation trigger.** Does the pipeline run on a schedule, on a queue, or
  incrementally per event? Depends on ADR-0014.
- **Session end semantics.** Every rule for closing an idle session is wrong in
  some way; the question is which failure is acceptable and how it is surfaced
  to the reader of a dashboard.
- **Settling window.** How long a correlated timeline stays mutable before it is
  treated as final, and how a late event that arrives after settling is handled.
- **Canonical identity.** How a globally unique, time-stable session identity is
  derived from backend-supplied identifiers that are neither.
- **Multi-tenancy.** Whether tenant is a first-class dimension. It affects the
  partitioning strategy, so it cannot stay open long after ADR-0014 closes.
- **Retention policy.** ADR-0004 assumes weeks. The actual number is a product
  decision that has not been made.
