// Package eventid derives the stable identity of a received event.
//
// This is the mechanism that makes at-least-once webhook delivery (ADR-0011)
// idempotent: a redelivery must derive the same id so the insert conflicts and
// is discarded. ADR-0024 specifies the inputs; this package is that
// specification in code, and the tests are the specification's assertions.
//
// Two failure modes are being designed against, and they pull in opposite
// directions:
//
//   - Collapse. Two genuinely distinct events derive one id, so real data is
//     silently lost. Guarded by including every field that can distinguish
//     co-occurring events, and by length-prefixing so field boundaries cannot
//     be forged.
//   - Split. One event redelivered derives two ids, so idempotency fails open
//     and duplicates accumulate that every downstream count believes are real.
//     Guarded by excluding anything that can vary between deliveries of the
//     same event — above all the payload.
package eventid

import (
	"encoding/binary"
	"time"

	"github.com/google/uuid"

	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

// Namespace is the project's fixed uuidv5 namespace. It must never change:
// changing it re-identifies every event ever stored.
var Namespace = uuid.MustParse("6f1d5b0e-4c2a-5f7b-9e83-2d4a1c6b8f30")

// scheme prefixes every derivation input so the scheme itself can be revised
// later without silently re-identifying historical events. A change here is a
// migration, not a refactor.
const scheme = "v1"

// FromBackendEventID derives an id from a backend-supplied event id.
//
// This is the primary path. LiveKit supplies WebhookEvent.id, and when a
// backend guarantees uniqueness there is nothing for us to reason about.
func FromBackendEventID(backend session.Backend, backendEventID string) uuid.UUID {
	return uuid.NewSHA1(Namespace, encode(scheme, string(backend), backendEventID))
}

// Fields are the inputs to the fallback derivation, for backends that supply
// no event id of their own. mediasoup will be the first (ADR-0003).
type Fields struct {
	Backend             session.Backend
	EventType           session.EventType
	Room                string
	ParticipantIdentity string
	ParticipantSID      string

	// TrackSID is not optional where the backend has one. Without it, audio and
	// video published in the same millisecond are indistinguishable and collapse
	// into a single row — the concrete bug ADR-0024's earlier wording left open.
	TrackSID string

	// OccurredAt is used at nanosecond precision. Truncating to seconds would
	// collapse events that a busy room produces within the same second.
	OccurredAt time.Time

	// DeliveryOrdinal is the event's index within its delivery batch: the
	// last-resort discriminator for backends that can emit two genuinely
	// distinct, otherwise-identical events at the same instant. It must be
	// stable across retries of the same delivery, which is what makes it usable
	// as identity rather than as a nonce.
	DeliveryOrdinal int
}

// FromFields derives an id from the canonical tuple.
//
// The payload is deliberately not among the inputs. See the package comment and
// ADR-0024: hashing it fails in both directions, and the split failure is the
// worse one because nothing errors.
func FromFields(f Fields) uuid.UUID {
	return uuid.NewSHA1(Namespace, encode(
		scheme,
		string(f.Backend),
		string(f.EventType),
		f.Room,
		f.ParticipantIdentity,
		f.ParticipantSID,
		f.TrackSID,
		f.OccurredAt.UTC().Format(time.RFC3339Nano),
		itoa(f.DeliveryOrdinal),
	))
}

// encode joins fields with an explicit length prefix per field rather than a
// delimiter.
//
// With a delimiter, ("a", "bc") and ("ab", "c") produce identical input and
// therefore identical ids — a silent collapse of two distinct events. No choice
// of delimiter fixes this, because any delimiter can appear inside a
// caller-supplied room name or identity. Length prefixes make the boundaries
// unforgeable.
func encode(fields ...string) []byte {
	n := 0
	for _, f := range fields {
		n += 4 + len(f)
	}
	buf := make([]byte, 0, n)
	for _, f := range fields {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(f)))
		buf = append(buf, l[:]...)
		buf = append(buf, f...)
	}
	return buf
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
