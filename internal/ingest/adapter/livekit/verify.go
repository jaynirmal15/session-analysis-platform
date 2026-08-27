package livekit

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrUnverified covers every reason a delivery failed authentication. The
	// reasons are deliberately not distinguished to the caller: telling an
	// unauthenticated sender *why* verification failed helps them iterate.
	ErrUnverified = errors.New("livekit: delivery failed verification")

	errNoSecret = errors.New("livekit: verifier requires an API secret")
)

// Verifier authenticates LiveKit webhook deliveries.
//
// LiveKit signs a delivery with a JWT in the Authorization header whose sha256
// claim is the base64 digest of the request body. Verification therefore has
// two parts, and both matter: the token proves the sender holds the shared
// secret, and the digest proves the body is the one that was signed. Checking
// only the token would let anyone who captured one delivery replay its header
// against a body of their choosing.
type Verifier struct {
	apiKey    string
	apiSecret []byte
}

func NewVerifier(apiKey, apiSecret string) (*Verifier, error) {
	if apiSecret == "" {
		return nil, errNoSecret
	}
	return &Verifier{apiKey: apiKey, apiSecret: []byte(apiSecret)}, nil
}

// Verify checks the Authorization token against the raw body.
//
// It takes the body as bytes, before any parsing, and that ordering is the
// point: an unverified delivery must never reach a JSON decoder. Parsing
// attacker-controlled input is the thing authentication exists to gate.
func (v *Verifier) Verify(authorization string, body []byte) error {
	if authorization == "" {
		return ErrUnverified
	}

	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(authorization, claims,
		func(*jwt.Token) (any, error) { return v.apiSecret, nil },
		// HS256 is asserted, never read from the token. Selecting the algorithm
		// from the header is the alg-confusion vulnerability: an attacker sends
		// alg=none, or swaps RS256 for HS256 so a public key is used as an HMAC
		// key. Naming the accepted algorithm here makes both impossible.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(v.apiKey),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnverified, err)
	}

	claimed, _ := claims["sha256"].(string)
	if claimed == "" {
		return fmt.Errorf("%w: token carries no body digest", ErrUnverified)
	}

	sum := sha256.Sum256(body)
	expected := base64.StdEncoding.EncodeToString(sum[:])

	// Constant-time: a timing-variable comparison leaks the expected digest one
	// byte at a time to a sender who can retry.
	if subtle.ConstantTimeCompare([]byte(claimed), []byte(expected)) != 1 {
		return fmt.Errorf("%w: body does not match the signed digest", ErrUnverified)
	}
	return nil
}
