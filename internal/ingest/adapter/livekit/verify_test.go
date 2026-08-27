package livekit

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testKey    = "devkey"
	testSecret = "devsecretdevsecretdevsecretdevsecret"
)

// sign builds a token the way LiveKit does: HS256 over claims carrying the
// base64 SHA-256 of the body.
func sign(t *testing.T, secret string, body []byte, mutate func(jwt.MapClaims)) string {
	t.Helper()
	sum := sha256.Sum256(body)
	claims := jwt.MapClaims{
		"iss":    testKey,
		"exp":    time.Now().Add(5 * time.Minute).Unix(),
		"nbf":    time.Now().Add(-time.Minute).Unix(),
		"sha256": base64.StdEncoding.EncodeToString(sum[:]),
	}
	if mutate != nil {
		mutate(claims)
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func verifier(t *testing.T) *Verifier {
	t.Helper()
	v, err := NewVerifier(testKey, testSecret)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestVerifyAcceptsGenuineDelivery(t *testing.T) {
	body := []byte(`{"event":"room_started"}`)
	if err := verifier(t).Verify(sign(t, testSecret, body, nil), body); err != nil {
		t.Fatalf("genuine delivery rejected: %v", err)
	}
}

// The attack the body digest exists to stop: a captured Authorization header
// replayed against a body of the attacker's choosing.
func TestVerifyRejectsSwappedBody(t *testing.T) {
	signed := []byte(`{"event":"room_started"}`)
	token := sign(t, testSecret, signed, nil)

	tampered := []byte(`{"event":"participant_joined","participant":{"identity":"attacker"}}`)
	if err := verifier(t).Verify(token, tampered); !errors.Is(err, ErrUnverified) {
		t.Fatalf("swapped body accepted, got %v", err)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	body := []byte(`{"event":"room_started"}`)
	token := sign(t, "a-different-secret-that-is-long-enough", body, nil)
	if err := verifier(t).Verify(token, body); !errors.Is(err, ErrUnverified) {
		t.Fatalf("token signed with the wrong secret accepted, got %v", err)
	}
}

// alg confusion: a token asserting "none" must not be accepted regardless of
// what the header claims. The algorithm is ours to assert, never the token's.
func TestVerifyRejectsAlgNone(t *testing.T) {
	body := []byte(`{"event":"room_started"}`)
	sum := sha256.Sum256(body)
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss":    testKey,
		"exp":    time.Now().Add(time.Minute).Unix(),
		"sha256": base64.StdEncoding.EncodeToString(sum[:]),
	})
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build alg=none token: %v", err)
	}
	if err := verifier(t).Verify(signed, body); !errors.Is(err, ErrUnverified) {
		t.Fatalf("alg=none accepted, got %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	body := []byte(`{"event":"room_started"}`)
	token := sign(t, testSecret, body, func(c jwt.MapClaims) {
		c["exp"] = time.Now().Add(-time.Hour).Unix()
	})
	if err := verifier(t).Verify(token, body); !errors.Is(err, ErrUnverified) {
		t.Fatalf("expired token accepted, got %v", err)
	}
}

func TestVerifyRejectsMissingDigestClaim(t *testing.T) {
	body := []byte(`{"event":"room_started"}`)
	token := sign(t, testSecret, body, func(c jwt.MapClaims) { delete(c, "sha256") })
	if err := verifier(t).Verify(token, body); !errors.Is(err, ErrUnverified) {
		t.Fatalf("token without a body digest accepted, got %v", err)
	}
}

func TestVerifyRejectsWrongIssuer(t *testing.T) {
	body := []byte(`{"event":"room_started"}`)
	token := sign(t, testSecret, body, func(c jwt.MapClaims) { c["iss"] = "someone-else" })
	if err := verifier(t).Verify(token, body); !errors.Is(err, ErrUnverified) {
		t.Fatalf("token from another issuer accepted, got %v", err)
	}
}

func TestVerifyRejectsEmptyAuthorization(t *testing.T) {
	if err := verifier(t).Verify("", []byte(`{}`)); !errors.Is(err, ErrUnverified) {
		t.Fatal("missing Authorization header accepted")
	}
}

// A verifier with no secret would accept unsigned traffic. Refusing to
// construct one is what makes that impossible rather than merely discouraged.
func TestVerifierRequiresSecret(t *testing.T) {
	if _, err := NewVerifier(testKey, ""); err == nil {
		t.Fatal("NewVerifier accepted an empty secret")
	}
}
