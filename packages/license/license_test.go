package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// The issuer half (Issue) lives in meshploy-admin and must never ship here, so
// these tests sign with a locally generated key rather than importing it. That
// is also a useful property in itself: it proves the verifier is self-contained.
func mint(t *testing.T, priv ed25519.PrivateKey, l License) string {
	t.Helper()
	payload, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sig := ed25519.Sign(priv, payload)
	enc := base64.RawURLEncoding
	return TokenPrefix + "." + enc.EncodeToString(payload) + "." + enc.EncodeToString(sig)
}

func validLicense() License {
	return License{
		Version:   SchemaVersion,
		LicenseID: "lic-1",
		Customer:  "acme",
		Tier:      "enterprise",
		Features:  []string{"sso", "audit-log"},
		IssuedAt:  time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	got, err := Verify(pub, mint(t, priv, validLicense()))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Customer != "acme" || !got.HasFeature("sso") || got.HasFeature("mtls") {
		t.Fatalf("claims did not survive the round trip: %+v", got)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tok := mint(t, priv, validLicense())

	// Re-encode escalated claims under the original signature.
	forged := validLicense()
	forged.Tier = "vendor-evil"
	payload, _ := json.Marshal(forged)
	parts := TokenPrefix + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + tok[len(tok)-86:]

	if _, err := Verify(pub, parts); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("a re-signed payload must be rejected, got %v", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	otherPub, _, _ := ed25519.GenerateKey(nil)

	if _, err := Verify(otherPub, mint(t, priv, validLicense())); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("a token signed by another key must be rejected, got %v", err)
	}
}

func TestVerifyRejectsUnknownFormat(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	for _, tok := range []string{"garbage", "a.b", "mlic-v9.aaa.bbb"} {
		if _, err := Verify(pub, tok); err == nil {
			t.Fatalf("%q must not verify", tok)
		}
	}
}

// Degrade, don't brick: an expired license still returns its claims so the
// product can show renewal details and fall back to CE rather than refusing to
// start. This is the difference between a lapsed customer and an outage.
func TestVerifyExpiredStillReturnsClaims(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	l := validLicense()
	l.ExpiresAt = time.Now().Add(-time.Minute)

	got, err := Verify(pub, mint(t, priv, l))
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
	if got.Customer != "acme" {
		t.Fatal("an expired license must still surface its claims for the renewal prompt")
	}
}

func TestMatchesDomain(t *testing.T) {
	cases := []struct {
		name    string
		domains []string
		domain  string
		want    bool
	}{
		{"unbound licenses work anywhere", nil, "anything.com", true},
		{"exact match", []string{"acme.com"}, "acme.com", true},
		{"exact mismatch", []string{"acme.com"}, "evil.com", false},
		{"case insensitive", []string{"ACME.com"}, "acme.COM", true},
		{"surrounding whitespace tolerated", []string{" acme.com "}, "acme.com", true},
		{"one of several", []string{"a.com", "b.com"}, "b.com", true},
		{"wildcard matches one label", []string{"*.acme.com"}, "eu.acme.com", true},
		{"wildcard does not match the apex", []string{"*.acme.com"}, "acme.com", false},
		{"wildcard does not match two labels", []string{"*.acme.com"}, "a.b.acme.com", false},
		{"wildcard does not match a different domain", []string{"*.acme.com"}, "eu.evil.com", false},
		{"bound license rejects an empty domain", []string{"acme.com"}, "", false},
		{"suffix confusion is rejected", []string{"acme.com"}, "notacme.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := License{Domains: tc.domains}
			if got := l.MatchesDomain(tc.domain); got != tc.want {
				t.Fatalf("MatchesDomain(%q) with %v = %v, want %v", tc.domain, tc.domains, got, tc.want)
			}
		})
	}
}

// Backward compatibility: tokens issued before the domains claim existed have
// no such field, and must keep working.
func TestPreDomainTokensStayValid(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	l := validLicense() // Domains left nil, as an older token would be

	got, err := Verify(pub, mint(t, priv, l))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !got.MatchesDomain("whatever.example") {
		t.Fatal("a token predating domain binding must remain unbound")
	}
}
