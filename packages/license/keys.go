package license

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Public-key helpers only.
//
// Keypair generation and every private-key helper stay with the issuer in
// meshploy-admin. Verifying a license requires only the public key — that
// asymmetry is what lets this package ship in the open-source build without
// giving anyone the ability to mint a license.

// Fingerprint returns a short, stable identifier for a public key (first 8 bytes
// of its SHA-256, hex). Used to record which signing key issued a license, so
// key rotation stays auditable.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// EncodePublicKey renders a public key as base64, suitable for pasting into the
// product as a Go constant.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// DecodePublicKey parses a base64-encoded public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: got %d, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}
