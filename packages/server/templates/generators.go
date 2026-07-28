package templates

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// Generator names usable in meta.yaml `generate:`.
const (
	genPassword  = "password"  // 24-char alphanumeric
	genSecret64  = "secret64"  // 64-char base64url
	genHex32     = "hex32"     // 32-char hex
	genUUID      = "uuid"      // v4 UUID
	genSubdomain = "subdomain" // <id>-<rand>.<base domain>; handled in Resolve
)

const alphanumeric = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// base64url alphabet without padding — URL/shell-safe, no '+' '/' '='.
const base64urlAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// generateValue produces a value for a value-generator name. The "subdomain"
// generator is handled in Resolve (it needs the id + base domain), not here.
func generateValue(name string) (string, error) {
	switch name {
	case genPassword:
		return randFromAlphabet(alphanumeric, 24), nil
	case genSecret64:
		return randFromAlphabet(base64urlAlphabet, 64), nil
	case genHex32:
		return randHex(16), nil
	case genUUID:
		return uuid.NewString(), nil
	default:
		return "", fmt.Errorf("unknown generator %q", name)
	}
}

// randFromAlphabet returns n characters drawn uniformly from alphabet using a
// rejection sampler over crypto/rand (no modulo bias).
func randFromAlphabet(alphabet string, n int) string {
	out := make([]byte, n)
	max := byte(len(alphabet))
	limit := byte(256 - (256 % int(max)))
	buf := make([]byte, 1)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			panic(err) // crypto/rand failure is unrecoverable
		}
		if buf[0] >= limit {
			continue // reject to keep the distribution uniform
		}
		out[i] = alphabet[buf[0]%max]
		i++
	}
	return string(out)
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
