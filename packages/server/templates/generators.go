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
//
// The rejection limit is computed and compared in int, never byte. When the
// alphabet's length divides 256 the limit IS 256, and byte(256) wraps to 0 --
// every draw then satisfies "value >= limit", every draw is rejected, and the
// loop never advances. That is not a slow path but a permanent one: deploying
// any template with a `secret64` variable pinned a core reading one byte of
// kernel randomness per iteration, for as long as the process lived.
//
// Randomness is drawn in blocks rather than a byte at a time. A syscall per
// byte is what made the spin so expensive, and it is wasteful even when the
// loop terminates.
func randFromAlphabet(alphabet string, n int) string {
	size := len(alphabet)
	if size == 0 || size > 256 {
		// Unrepresentable in a single byte draw. Both callers pass a constant
		// alphabet, so this is a guard against a future one, not a live case.
		panic(fmt.Sprintf("randFromAlphabet: alphabet length %d must be 1..256", size))
	}
	limit := 256 - (256 % size) // 256 when size divides 256 — hence the int
	out := make([]byte, n)

	block := make([]byte, 64)
	pos := len(block) // force a fill on the first pass
	for i := 0; i < n; {
		if pos == len(block) {
			if _, err := rand.Read(block); err != nil {
				panic(err) // crypto/rand failure is unrecoverable
			}
			pos = 0
		}
		b := int(block[pos])
		pos++
		if b >= limit {
			continue // reject to keep the distribution uniform
		}
		out[i] = alphabet[b%size]
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
