// Package metrics holds the write path's OpenTelemetry instruments.
//
// Scoped to internal/ingest deliberately. It is not a shared metrics package —
// the read path will have its own, and a single cross-path one would be the
// junk drawer ADR-0009 forbids.
//
// Most of what is counted here is not error handling; it is measurement of
// things the platform has decided to observe rather than repair (ADR-0016,
// ADR-0020). A duplicate delivery, an unmatched close, a missing partition:
// each is a fact about the integration that belongs on a dashboard, and each
// would be invisible if the code simply handled it.
package metrics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Ingest is the instrument set for the webhook receiver.
type Ingest struct {
	requests        metric.Int64Counter
	duration        metric.Float64Histogram
	stored          metric.Int64Counter
	duplicates      metric.Int64Counter
	rejected        metric.Int64Counter
	unrecognised    metric.Int64Counter
	partitionMissed metric.Int64Counter
	joinsOpened     metric.Int64Counter
	joinsClosed     metric.Int64Counter
	closeUnmatched  metric.Int64Counter
	closeOutOfOrder metric.Int64Counter
}

// NewIngest builds the instrument set. An error here means telemetry is
// misconfigured, which is a startup failure rather than something to degrade
// through: a receiver whose measurements are silently absent is worse than one
// that refuses to start (ADR-0005).
func NewIngest() (*Ingest, error) {
	m := otel.Meter("github.com/jaynirmal15/session-analysis-platform/internal/ingest")

	var err error
	i := &Ingest{}
	c := func(name, desc, unit string) metric.Int64Counter {
		if err != nil {
			return nil
		}
		var ctr metric.Int64Counter
		ctr, err = m.Int64Counter(name, metric.WithDescription(desc), metric.WithUnit(unit))
		return ctr
	}

	i.requests = c("sap_ingest_webhook_requests_total",
		"Webhook deliveries received, by outcome.", "{request}")
	i.stored = c("sap_ingest_events_stored_total",
		"Events durably written to event_raw.", "{event}")
	i.duplicates = c("sap_ingest_events_duplicate_total",
		"Redeliveries discarded by the idempotency constraint. Non-zero is expected: this is at-least-once delivery being observed rather than assumed.", "{event}")
	i.rejected = c("sap_ingest_events_rejected_total",
		"Events refused at the boundary as out of scope (ADR-0022). Counted so the scope decision stays visible and reversible with evidence.", "{event}")
	i.unrecognised = c("sap_ingest_events_unrecognised_total",
		"Events of a type we do not know, stored anyway. Non-zero means the integration drifted.", "{event}")
	i.partitionMissed = c("sap_ingest_partition_missing_total",
		"Events dropped because no partition covers their occurred_at. Real data loss, deliberately loud (ADR-0024).", "{event}")
	i.joinsOpened = c("sap_ingest_joins_opened_total",
		"Joins opened from an observed participant_joined.", "{join}")
	i.joinsClosed = c("sap_ingest_joins_closed_total",
		"Joins closed from an observed event, by end_reason.", "{join}")
	i.closeUnmatched = c("sap_ingest_join_close_unmatched_total",
		"Close events with no matching open join: the opening event was never received. No synthetic join is created (ADR-0020).", "{event}")
	i.closeOutOfOrder = c("sap_ingest_join_close_out_of_order_total",
		"Closes rejected because ended_at preceded started_at, usually clock skew. The join is left open rather than corrected.", "{event}")

	if err == nil {
		i.duration, err = m.Float64Histogram("sap_ingest_webhook_duration_seconds",
			metric.WithDescription("Wall time from delivery receipt to response."),
			metric.WithUnit("s"))
	}
	if err != nil {
		return nil, err
	}
	return i, nil
}

func backendAttr(backend string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("backend", backend))
}

func (i *Ingest) Request(ctx context.Context, backend, outcome string, seconds float64) {
	i.requests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("backend", backend),
		attribute.String("outcome", outcome),
	))
	i.duration.Record(ctx, seconds, backendAttr(backend))
}

func (i *Ingest) Stored(ctx context.Context, backend, eventType string) {
	i.stored.Add(ctx, 1, metric.WithAttributes(
		attribute.String("backend", backend),
		attribute.String("event_type", eventType),
	))
}

func (i *Ingest) Duplicate(ctx context.Context, backend, eventType string) {
	i.duplicates.Add(ctx, 1, metric.WithAttributes(
		attribute.String("backend", backend),
		attribute.String("event_type", eventType),
	))
}

func (i *Ingest) Rejected(ctx context.Context, backend, eventType string) {
	i.rejected.Add(ctx, 1, metric.WithAttributes(
		attribute.String("backend", backend),
		attribute.String("event_type", eventType),
	))
}

func (i *Ingest) Unrecognised(ctx context.Context, backend, eventType string) {
	i.unrecognised.Add(ctx, 1, metric.WithAttributes(
		attribute.String("backend", backend),
		attribute.String("event_type", eventType),
	))
}

func (i *Ingest) PartitionMissing(ctx context.Context, backend string) {
	i.partitionMissed.Add(ctx, 1, backendAttr(backend))
}

func (i *Ingest) JoinOpened(ctx context.Context, backend string) {
	i.joinsOpened.Add(ctx, 1, backendAttr(backend))
}

func (i *Ingest) JoinsClosed(ctx context.Context, backend, endReason string, n int64) {
	if n <= 0 {
		return
	}
	i.joinsClosed.Add(ctx, n, metric.WithAttributes(
		attribute.String("backend", backend),
		attribute.String("end_reason", endReason),
	))
}

func (i *Ingest) CloseUnmatched(ctx context.Context, backend string) {
	i.closeUnmatched.Add(ctx, 1, backendAttr(backend))
}

func (i *Ingest) CloseOutOfOrder(ctx context.Context, backend string) {
	i.closeOutOfOrder.Add(ctx, 1, backendAttr(backend))
}
