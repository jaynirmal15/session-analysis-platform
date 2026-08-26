# Dashboards

Provisioned dashboard JSON goes here; Grafana loads every `*.json` file in this
directory into the "Session Analysis Platform" folder.

Empty on purpose. There are no domain metrics to chart yet — the ingester emits
only Go runtime metrics. Committing a dashboard now would mean committing
panels against a metric contract that has not been designed.

TODO(scope): add dashboards once the event schema and the first ingest metrics
exist. Two are planned, matching the two datasources:

- **Fleet** (Prometheus) — ingest rate, webhook latency, correlation lag,
  error rate by backend.
- **Session drill-down** (PostgreSQL) — one session's full timeline, queried
  directly from the partitioned tables.
