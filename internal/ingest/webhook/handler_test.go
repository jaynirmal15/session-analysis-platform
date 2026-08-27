package webhook

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/adapter/livekit"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/metrics"
	"github.com/jaynirmal15/session-analysis-platform/internal/ingest/store"
	"github.com/jaynirmal15/session-analysis-platform/internal/session"
)

const (
	key    = "devkey"
	secret = "devsecretdevsecretdevsecretdevsecret"
)

// fakeWriter records what reached the store, and can be told to fail. It is the
// second implementation that justifies store.Writer being an interface.
type fakeWriter struct {
	got    []session.Event
	result store.Result
	err    error
}

func (f *fakeWriter) RecordEvent(_ context.Context, ev session.Event) (store.Result, error) {
	f.got = append(f.got, ev)
	return f.result, f.err
}

func newHandler(t *testing.T, w store.Writer) *LiveKitHandler {
	t.Helper()
	v, err := livekit.NewVerifier(key, secret)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	m, err := metrics.NewIngest()
	if err != nil {
		t.Fatalf("NewIngest: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewLiveKitHandler(v, w, m, log, 1<<20)
}

func signed(t *testing.T, body string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":    key,
		"exp":    time.Now().Add(time.Minute).Unix(),
		"sha256": base64.StdEncoding.EncodeToString(sum[:]),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func post(t *testing.T, h http.Handler, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhook/livekit", strings.NewReader(body))
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const joinedBody = `{"event":"participant_joined","id":"EV_1","createdAt":1787000000,` +
	`"room":{"sid":"RM_1","name":"standup"},"participant":{"sid":"PA_1","identity":"alice"}}`

func TestAcceptsVerifiedDelivery(t *testing.T) {
	f := &fakeWriter{result: store.Result{Stored: true, JoinOpened: true}}
	res := post(t, newHandler(t, f), joinedBody, signed(t, joinedBody))

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if len(f.got) != 1 || f.got[0].ParticipantIdentity != "alice" {
		t.Fatalf("store received %+v", f.got)
	}
}

// The ordering guarantee: an unverified delivery must never reach a parser, so
// nothing about it can reach the store either.
func TestUnverifiedNeverReachesTheStore(t *testing.T) {
	f := &fakeWriter{}
	h := newHandler(t, f)

	for name, auth := range map[string]string{
		"no header":    "",
		"garbage":      "not-a-token",
		"wrong secret": jwtWithSecret(t, joinedBody, "another-secret-long-enough-here"),
		"swapped body": signed(t, `{"event":"room_started","id":"EV_other"}`),
	} {
		t.Run(name, func(t *testing.T) {
			res := post(t, h, joinedBody, auth)
			if res.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", res.Code)
			}
		})
	}
	if len(f.got) != 0 {
		t.Fatalf("unverified deliveries reached the store: %+v", f.got)
	}
}

func jwtWithSecret(t *testing.T, body, s string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": key, "exp": time.Now().Add(time.Minute).Unix(),
		"sha256": base64.StdEncoding.EncodeToString(sum[:]),
	}).SignedString([]byte(s))
	return tok
}

// Rejected types must be acknowledged. A non-2xx would make LiveKit retry an
// event we will never want, turning a scope decision into a traffic problem.
func TestRejectedTypesAreAcknowledgedNotStored(t *testing.T) {
	body := `{"event":"egress_started","id":"EV_E","createdAt":1787000000}`
	f := &fakeWriter{}
	res := post(t, newHandler(t, f), body, signed(t, body))

	if res.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a retry would follow otherwise", res.Code)
	}
	if len(f.got) != 0 {
		t.Errorf("rejected event was stored: %+v", f.got)
	}
}

func TestUnrecognisedTypesAreStored(t *testing.T) {
	body := `{"event":"future_event","id":"EV_F","createdAt":1787000000}`
	f := &fakeWriter{result: store.Result{Stored: true}}
	res := post(t, newHandler(t, f), body, signed(t, body))

	if res.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", res.Code)
	}
	if len(f.got) != 1 {
		t.Fatalf("unrecognised event was not stored; it is the payload we want")
	}
}

// A missing partition is data loss we acknowledge on purpose: retrying cannot
// succeed for the usual cause, and a 5xx would produce a retry storm during an
// incident nobody can fix from here.
func TestPartitionMissingIsAcknowledged(t *testing.T) {
	f := &fakeWriter{err: store.ErrPartitionMissing}
	res := post(t, newHandler(t, f), joinedBody, signed(t, joinedBody))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
}

// An ordinary storage failure IS retryable, and must say so.
func TestStoreFailureAsksForRetry(t *testing.T) {
	f := &fakeWriter{err: errors.New("connection refused")}
	res := post(t, newHandler(t, f), joinedBody, signed(t, joinedBody))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 so the delivery is retried", res.Code)
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	v, _ := livekit.NewVerifier(key, secret)
	m, _ := metrics.NewIngest()
	h := NewLiveKitHandler(v, &fakeWriter{}, m, slog.New(slog.NewTextHandler(io.Discard, nil)), 16)

	res := post(t, h, joinedBody, signed(t, joinedBody))
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.Code)
	}
}

func TestMalformedBodyRejected(t *testing.T) {
	body := `{"not":"an event"}`
	f := &fakeWriter{}
	res := post(t, newHandler(t, f), body, signed(t, body))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}
	if len(f.got) != 0 {
		t.Error("malformed body reached the store")
	}
}

// A duplicate is a 200 like any accepted delivery: the sender did nothing
// wrong, and telling it otherwise would provoke a retry of an event we already
// hold.
func TestDuplicateIsAcknowledged(t *testing.T) {
	f := &fakeWriter{result: store.Result{Stored: false}}
	res := post(t, newHandler(t, f), joinedBody, signed(t, joinedBody))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
}

// The two failures that share outcome="malformed" and mean opposite things.
//
// This is the gap ADR-0027 recorded and ADR-0028 explains: for an entire
// session, a receiver that rejected 100% of genuine LiveKit deliveries produced
// the same signal as an endpoint being idly fuzzed. The distinguishing fact is
// whether the sender held our secret.
func TestMalformedIsSeparableByVerification(t *testing.T) {
	// Verified sender, undecodable body: the wire format changed under us.
	t.Run("verified sender", func(t *testing.T) {
		body := `{"id":"EV_1","createdAt":"1787000000"}` // valid JSON, no event type
		f := &fakeWriter{}
		res := post(t, newHandler(t, f), body, signed(t, body))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", res.Code)
		}
		if len(f.got) != 0 {
			t.Error("an undecodable body reached the store")
		}
	})

	// Unauthenticated sender: noise, and it must never be confused with the above.
	t.Run("unverified sender", func(t *testing.T) {
		f := &fakeWriter{}
		res := post(t, newHandler(t, f), `{"garbage`, "")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 — verification precedes parsing", res.Code)
		}
		if len(f.got) != 0 {
			t.Error("unverified body reached the store")
		}
	})
}

// The regression guard for the bug that started all this. A verified delivery
// in LiveKit's real wire format — int64 fields encoded as JSON strings — must
// be accepted. If this ever fails, the receiver is rejecting genuine traffic
// again and the malformed/verified=true ratio is the thing that will say so.
func TestRealWireFormatIsAccepted(t *testing.T) {
	body := `{"event":"participant_joined","id":"EV_real","createdAt":"1787791385",` +
		`"room":{"sid":"RM_1","name":"standup","creationTime":"1787791380"},` +
		`"participant":{"sid":"PA_1","identity":"alice","joinedAt":"1787791385"}}`

	f := &fakeWriter{result: store.Result{Stored: true, JoinOpened: true}}
	res := post(t, newHandler(t, f), body, signed(t, body))

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — this is the shape LiveKit actually sends", res.Code)
	}
	if len(f.got) != 1 {
		t.Fatal("a genuine delivery did not reach the store")
	}
}
