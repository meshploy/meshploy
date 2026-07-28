// Package license is the shared contract between the Meshploy Enterprise issuer
// (this control plane) and the meshploy product that verifies licenses.
//
// The package is split into two halves that are deliberately separable:
//
//   - claims.go / verify.go / keys.go (public-safe) — the VERIFIER half. These
//     need only the public key and ship in the open-source product so a running
//     CE install can validate a license token offline when the user upgrades.
//   - sign.go — the ISSUER half. It holds/uses the private signing key and must
//     never ship in the product.
//
// Security rests entirely on the Ed25519 signature over the serialized claims;
// nothing in the claim set is secret.
package license

import (
	"strings"
	"time"
)

// SchemaVersion is the version of the License claim set. Bump it when the
// structure changes in a way the verifier must branch on.
const SchemaVersion = 1

// License is the set of claims carried by a signed token — the single source of
// truth shared by issuer and verifier. Keep field tags stable: they are part of
// the signed payload, so renaming one invalidates every previously issued token.
type License struct {
	Version       int       `json:"v"`
	LicenseID     string    `json:"lid"`
	Customer      string    `json:"cust"`
	Tier          string    `json:"tier"`                     // "enterprise" or "vendor-<name>"
	Features      []string  `json:"features,omitempty"`       // feature flags this license unlocks
	NodeLimit     int       `json:"node_limit,omitempty"`     // 0 = unlimited
	RegistryScope string    `json:"registry_scope,omitempty"` // which private image this grants
	Domains       []string  `json:"domains,omitempty"`        // install domains this license binds to; empty = unbound
	IssuedAt      time.Time `json:"iat"`
	ExpiresAt     time.Time `json:"exp"`
}

// HasFeature reports whether the license grants the named feature.
func (l License) HasFeature(name string) bool {
	for _, f := range l.Features {
		if f == name {
			return true
		}
	}
	return false
}

// Expired reports whether the license term has lapsed as of now.
func (l License) Expired() bool { return time.Now().After(l.ExpiresAt) }

// MatchesDomain reports whether the license may be used by an install serving
// the given base domain.
//
// Domain binding is the anti-redistribution control: sharing both the private
// image and a license with another company still fails, because their domain
// differs. Unlike a hardware fingerprint it survives migrations and DR, so it
// does not generate support tickets for legitimate moves.
//
// An empty Domains list means unbound — the license works anywhere. That keeps
// every previously issued token valid, since they predate this claim.
//
// A pattern may be an exact host ("acme.com") or a single-label wildcard
// ("*.acme.com", which matches "eu.acme.com" but not "acme.com" itself or
// "a.b.acme.com"). Comparison is case-insensitive.
func (l License) MatchesDomain(domain string) bool {
	if len(l.Domains) == 0 {
		return true
	}
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	for _, pattern := range l.Domains {
		p := strings.ToLower(strings.TrimSpace(pattern))
		if p == "" {
			continue
		}
		if suffix, ok := strings.CutPrefix(p, "*."); ok {
			// Wildcard matches exactly one extra label.
			rest, found := strings.CutSuffix(d, "."+suffix)
			if found && rest != "" && !strings.Contains(rest, ".") {
				return true
			}
			continue
		}
		if p == d {
			return true
		}
	}
	return false
}
