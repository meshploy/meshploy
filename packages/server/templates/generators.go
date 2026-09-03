package templates

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Generator names usable in meta.yaml `generate:`.
const (
	genPassword  = "password"  // 24-char alphanumeric
	genSecret64  = "secret64"  // 64-char base64url
	genHex32     = "hex32"     // 32-char hex
	genUUID      = "uuid"      // v4 UUID
	genSubdomain = "subdomain" // <id>-<rand>.<base domain>; handled in Resolve

	// genBcryptPrefix marks a DERIVED generator, written `bcrypt(OTHER_KEY)`.
	// It produces a bcrypt hash of another variable's resolved value, so a
	// template can write a credential's hash into an htpasswd file instead of
	// the credential. Handled in Resolve -- unlike every other generator it
	// needs the other variables to exist first.
	//
	// Note what this does NOT buy: PrepareSpec substitutes into the compose and
	// `stacks.spec` stores the result, so the hash is in the database as
	// plaintext, and so is the password it came from, in `stacks.variables`.
	// What is gained is that the config file, the Kubernetes Secret and the
	// container hold only the hash.
	genBcryptPrefix = "bcrypt("
)

// bcryptRef returns the variable a `bcrypt(KEY)` generator hashes, and whether
// the generator was of that form at all.
func bcryptRef(generate string) (string, bool) {
	if !strings.HasPrefix(generate, genBcryptPrefix) || !strings.HasSuffix(generate, ")") {
		return "", false
	}
	key := strings.TrimSpace(generate[len(genBcryptPrefix) : len(generate)-1])
	if key == "" {
		return "", false
	}
	return key, true
}

// bcryptHash hashes a value with the $2a$ prefix htpasswd consumers expect.
//
// Cost 10 is Apache's htpasswd default. Higher costs are better for a login
// endpoint, but a registry authenticates every layer of every pull, and the
// hash is checked on each -- cost 12 would turn a docker pull into a CPU-bound
// operation on the gateway. 10 is the right point for this use.
func bcryptHash(value string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(value), 10)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	return string(h), nil
}

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
		if _, ok := bcryptRef(name); ok {
			// Derived: Resolve computes it once the referenced value exists.
			return "", fmt.Errorf("generator %q is derived and must be resolved with the other variables", name)
		}
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
