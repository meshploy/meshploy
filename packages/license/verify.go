package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Verification outcomes. Callers branch on these to degrade gracefully:
// ErrExpired means the signature was valid but the term lapsed (enter a lapsed
// state, keep CE running), whereas ErrInvalidSignature/ErrMalformed/
// ErrUnknownFormat mean tampering or a bad token and must be rejected outright.
var (
	ErrMalformed        = errors.New("license: malformed token")
	ErrUnknownFormat    = errors.New("license: unknown token format")
	ErrInvalidSignature = errors.New("license: invalid signature")
	ErrExpired          = errors.New("license: expired")
)

// Verify checks a token against the embedded public key and returns the decoded
// claims. When the error is [ErrExpired] the parsed License is still returned so
// the caller can show details and enter a lapsed state; for any other error the
// returned License is zero and must be treated as untrusted.
//
// Verify is the VERIFIER half — it needs only the public key, so it is safe to
// ship in the open-source build.
func Verify(pub ed25519.PublicKey, token string) (License, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return License{}, ErrMalformed
	}
	if parts[0] != TokenPrefix {
		return License{}, ErrUnknownFormat
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[1])
	if err != nil {
		return License{}, fmt.Errorf("%w: payload: %v", ErrMalformed, err)
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return License{}, fmt.Errorf("%w: signature: %v", ErrMalformed, err)
	}
	if !ed25519.Verify(pub, payload, sig) {
		return License{}, ErrInvalidSignature
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return License{}, fmt.Errorf("%w: claims: %v", ErrMalformed, err)
	}
	if l.Expired() {
		return l, ErrExpired
	}
	return l, nil
}
