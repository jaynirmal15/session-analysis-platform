package webhook

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/adapter/livekit"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/metrics"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/store"
	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

// Outcomes recorded on sap_ingest_webhook_requests_total. They describe what
// happened to the delivery, not merely which status code was returned: an
// accepted event and a deliberately ignored one both answer 200.
const (
	outcomeAccepted    = "accepted"
	outcomeDuplicate   = "duplicate"
	outcomeIgnored     = "ignored"
	outcomeUnverified  = "unverified"
	outcomeMalformed   = "malformed"
	outcomeTooLarge    = "too_large"
	outcomeDataLoss    = "partition_missing"
	outcomeStoreFailed = "store_failed"
)

var tracer = otel.Tracer("github.com/jaynirmal15/session-analysis-platform/internal/ingest/webhook")

// LiveKitHandler receives LiveKit webhook deliveries.
type LiveKitHandler struct {
	verifier     *livekit.Verifier
	writer       store.Writer
	metrics      *metrics.Ingest
	log          *slog.Logger
	maxBodyBytes int64
}

func NewLiveKitHandler(v *livekit.Verifier, w store.Writer, m *metrics.Ingest, log *slog.Logger, maxBody int64) *LiveKitHandler {
	return &LiveKitHandler{verifier: v, writer: w, metrics: m, log: log, maxBodyBytes: maxBody}
}

func (h *LiveKitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, span := tracer.Start(r.Context(), "webhook.livekit")
	defer span.End()

	backend := string(livekit.Backend)

	// Whether this delivery's signature verified, which is what makes an
	// undecodable body interpretable. A malformed body from an unauthenticated
	// sender is noise; the same body from a sender holding our secret means the
	// wire format changed under us. See ADR-0028.
	verified := false

	finish := func(status int, outcome string) {
		span.SetAttributes(
			attribute.String("sap.outcome", outcome),
			attribute.Bool("sap.verified", verified),
			attribute.Int("http.response.status_code", status),
		)
		h.metrics.Request(ctx, backend, outcome, verified, time.Since(start).Seconds())
		w.WriteHeader(status)
	}

	// Read the body under a hard cap, before anything else touches it.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			finish(http.StatusRequestEntityTooLarge, outcomeTooLarge)
			return
		}
		finish(http.StatusBadRequest, outcomeMalformed)
		return
	}

	// Verification happens here, on raw bytes, before any parsing of the
	// payload. An unverified delivery must never reach a JSON decoder.
	if err := h.verifier.Verify(r.Header.Get("Authorization"), body); err != nil {
		span.SetStatus(codes.Error, "unverified")
		// Logged without the body or the token: an unverified delivery is
		// attacker-controlled, and echoing it into logs moves the attacker's
		// input somewhere it will be read.
		h.log.WarnContext(ctx, "rejected unverified delivery",
			slog.String("backend", backend),
			slog.String("remote", r.RemoteAddr))
		finish(http.StatusUnauthorized, outcomeUnverified)
		return
	}
	verified = true

	// LiveKit sends one event per request, so the delivery ordinal is zero. It
	// is passed explicitly rather than assumed, because a batching backend
	// would need it and retrofitting it later would change every derived id.
	ev, disposition, err := livekit.Translate(body, 0)
	if err != nil {
		// Logged at error, not warn: the signature already verified, so this is
		// a sender holding our secret sending a body we cannot read. That is an
		// integration break, not a bad request.
		h.log.ErrorContext(ctx, "verified sender, undecodable body",
			slog.String("backend", backend),
			slog.Any("error", err))
		finish(http.StatusBadRequest, outcomeMalformed)
		return
	}

	if disposition == livekit.Reject {
		// Acknowledged, not stored. A non-2xx here would make LiveKit retry an
		// event we will never want, converting a scope decision into a traffic
		// problem.
		h.metrics.Rejected(ctx, backend, string(ev.Type))
		span.SetAttributes(attribute.String("sap.event_type", string(ev.Type)))
		finish(http.StatusOK, outcomeIgnored)
		return
	}
	if disposition == livekit.Store {
		h.metrics.Unrecognised(ctx, backend, string(ev.Type))
	}

	span.SetAttributes(
		attribute.String("sap.event_type", string(ev.Type)),
		attribute.String("sap.room", ev.RoomName),
	)

	res, err := h.writer.RecordEvent(ctx, ev)
	switch {
	case errors.Is(err, store.ErrPartitionMissing):
		// Real data loss, acknowledged deliberately. Retrying cannot succeed
		// for the usual cause — clock skew puts occurred_at outside every
		// partition — so a 5xx would turn one lost event into a retry storm
		// during an incident nobody can fix from here. The counter is the
		// signal (ADR-0024).
		h.metrics.PartitionMissing(ctx, backend)
		span.SetStatus(codes.Error, "no partition covers occurred_at")
		h.log.ErrorContext(ctx, "dropped event: no partition",
			slog.String("event_type", string(ev.Type)),
			slog.Time("occurred_at", ev.OccurredAt))
		finish(http.StatusOK, outcomeDataLoss)
		return

	case errors.Is(err, store.ErrCloseOutOfOrder):
		// The event is stored; only its close was refused, because ended_at
		// preceded started_at. The join stays open rather than being corrected
		// to something we did not observe (ADR-0020).
		h.metrics.CloseOutOfOrder(ctx, backend)
		h.log.WarnContext(ctx, "close rejected: ended_at precedes started_at",
			slog.String("room", ev.RoomName),
			slog.String("identity", ev.ParticipantIdentity))
		finish(http.StatusOK, outcomeAccepted)
		return

	case err != nil:
		// Genuinely retryable. A 5xx is correct here: LiveKit should try again.
		span.SetStatus(codes.Error, err.Error())
		h.log.ErrorContext(ctx, "store failed", slog.Any("error", err))
		finish(http.StatusInternalServerError, outcomeStoreFailed)
		return
	}

	if !res.Stored {
		// At-least-once delivery, observed rather than assumed. Join effects
		// were skipped, which is the whole point of deriving the id.
		h.metrics.Duplicate(ctx, backend, string(ev.Type))
		span.SetAttributes(attribute.Bool("sap.duplicate", true))
		finish(http.StatusOK, outcomeDuplicate)
		return
	}

	h.metrics.Stored(ctx, backend, string(ev.Type))
	if res.JoinOpened {
		h.metrics.JoinOpened(ctx, backend)
	}
	if res.JoinsClosed > 0 {
		h.metrics.JoinsClosed(ctx, backend, endReason(ev.Type), res.JoinsClosed)
	}
	if res.CloseUnmatched {
		// The opening event was never received. Recorded, not repaired.
		h.metrics.CloseUnmatched(ctx, backend)
		h.log.WarnContext(ctx, "close with no open join",
			slog.String("room", ev.RoomName),
			slog.String("identity", ev.ParticipantIdentity))
	}
	finish(http.StatusOK, outcomeAccepted)
}

// endReason names the reason a join was closed, for the metric label. It
// mirrors what the store wrote; the two must not drift, which is why both read
// from session's constants rather than string literals.
func endReason(t session.EventType) string {
	if t == session.EventRoomFinished {
		return string(session.EndRoomFinished)
	}
	return string(session.EndParticipantLeft)
}
