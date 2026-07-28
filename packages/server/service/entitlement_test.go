package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/license"
)

// Sign inline rather than importing the issuer: it lives in the private
// meshploy-admin repo and must never be a dependency of the product.
func mintToken(t *testing.T, priv ed25519.PrivateKey, l license.License) string {
	t.Helper()
	payload, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sig := ed25519.Sign(priv, payload)
	enc := base64.RawURLEncoding
	return license.TokenPrefix + "." + enc.EncodeToString(payload) + "." + enc.EncodeToString(sig)
}

func withTrustedKey(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	saved := LicensePublicKeys
	t.Cleanup(func() { LicensePublicKeys = saved })
	LicensePublicKeys = license.EncodePublicKey(pub)
}

func entLicense() license.License {
	return license.License{
		Version: license.SchemaVersion, LicenseID: "lic-1", Customer: "acme",
		Tier: "enterprise", Features: []string{"sso", "audit-log"},
		IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(24 * time.Hour),
	}
}

// A stock CE build trusts no signing key, so nothing is entitled. This is the
// safe default — an unconfigured build cannot accidentally unlock anything.
func TestEntitlementCEHasNothing(t *testing.T) {
	database := newExtTestDB(t)
	saved := LicensePublicKeys
	t.Cleanup(func() { LicensePublicKeys = saved })
	LicensePublicKeys = ""

	svc := &EntitlementService{db: database}
	if svc.Entitled(context.Background(), uuid.Nil, "sso") {
		t.Fatal("a CE build with no trusted key must entitle nothing")
	}
	st, err := svc.Describe(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if st.Licensed || len(st.Features) != 0 {
		t.Fatalf("want an unlicensed, featureless status, got %+v", st)
	}
}

func TestEntitlementActivatesAndGrants(t *testing.T) {
	database := newExtTestDB(t)
	pub, priv, _ := ed25519.GenerateKey(nil)
	withTrustedKey(t, pub)

	svc := &EntitlementService{db: database}
	st, err := svc.Activate(context.Background(), mintToken(t, priv, entLicense()), uuid.New())
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if !st.Licensed || st.Tier != "enterprise" {
		t.Fatalf("want an active enterprise license, got %+v", st)
	}
	if !svc.Entitled(context.Background(), uuid.Nil, "sso") {
		t.Fatal("a granted feature must be entitled")
	}
	if svc.Entitled(context.Background(), uuid.Nil, "mtls") {
		t.Fatal("a feature the license does not grant must not be entitled")
	}
}

// Degrade, don't brick: an expired license disables features but the install
// keeps running and can explain itself.
func TestEntitlementExpiredDegradesCleanly(t *testing.T) {
	database := newExtTestDB(t)
	pub, priv, _ := ed25519.GenerateKey(nil)
	withTrustedKey(t, pub)

	expired := entLicense()
	expired.ExpiresAt = time.Now().Add(-time.Minute)

	// Activation refuses an already-expired token with a clear message...
	svc := &EntitlementService{db: database}
	if _, err := svc.Activate(context.Background(), mintToken(t, priv, expired), uuid.New()); err == nil {
		t.Fatal("activating an expired license should report the expiry")
	}

	// ...and a license that lapses after activation disables features without
	// erroring, so the platform keeps serving.
	if err := database.Create(&meshdb.InstalledLicense{
		Token: mintToken(t, priv, expired), ActivatedBy: uuid.New(),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc2 := &EntitlementService{db: database}
	if svc2.Entitled(context.Background(), uuid.Nil, "sso") {
		t.Fatal("an expired license must not grant features")
	}
	st, err := svc2.Describe(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatalf("describe must not error on an expired license: %v", err)
	}
	if st.Licensed {
		t.Fatal("an expired license is not active")
	}
	if !st.Expired || st.Customer != "acme" || st.Problem == "" {
		t.Fatalf("expired status must still carry claims and a reason for the renewal prompt: %+v", st)
	}
}

// Domain binding is the anti-redistribution control: image + license shared with
// another company still fails, because their domain differs.
func TestEntitlementRejectsWrongDomain(t *testing.T) {
	database := newExtTestDB(t)
	pub, priv, _ := ed25519.GenerateKey(nil)
	withTrustedKey(t, pub)

	bound := entLicense()
	bound.Domains = []string{"acme.com"}
	tok := mintToken(t, priv, bound)

	// Wrong domain: refused at activation.
	wrong := &EntitlementService{db: database, domain: "evil.com"}
	if _, err := wrong.Activate(context.Background(), tok, uuid.New()); err == nil {
		t.Fatal("a license bound to another domain must not activate")
	}

	// Right domain: accepted.
	right := &EntitlementService{db: database, domain: "acme.com"}
	if _, err := right.Activate(context.Background(), tok, uuid.New()); err != nil {
		t.Fatalf("a license bound to this domain must activate: %v", err)
	}
	if !right.Entitled(context.Background(), uuid.Nil, "sso") {
		t.Fatal("domain-matched license must grant features")
	}
}

func TestEntitlementRejectsForeignKey(t *testing.T) {
	database := newExtTestDB(t)
	pub, _, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	withTrustedKey(t, pub)

	svc := &EntitlementService{db: database}
	if _, err := svc.Activate(context.Background(), mintToken(t, otherPriv, entLicense()), uuid.New()); err == nil {
		t.Fatal("a token signed by an untrusted key must be refused")
	}
}

// Key rotation must not invalidate existing licenses: a build trusting both the
// new and previous key accepts tokens signed by either.
func TestEntitlementAcceptsPreviousKeyDuringRotation(t *testing.T) {
	database := newExtTestDB(t)
	newPub, _, _ := ed25519.GenerateKey(nil)
	oldPub, oldPriv, _ := ed25519.GenerateKey(nil)

	saved := LicensePublicKeys
	t.Cleanup(func() { LicensePublicKeys = saved })
	LicensePublicKeys = license.EncodePublicKey(newPub) + "," + license.EncodePublicKey(oldPub)

	svc := &EntitlementService{db: database}
	if _, err := svc.Activate(context.Background(), mintToken(t, oldPriv, entLicense()), uuid.New()); err != nil {
		t.Fatalf("a license signed by the previous key must still verify during rotation: %v", err)
	}
}

// Node limits are a pricing meter, not a gate: over-limit is reported so the UI
// can warn, and nothing is blocked.
func TestEntitlementNodeLimitWarnsWithoutBlocking(t *testing.T) {
	database := newExtTestDB(t)
	pub, priv, _ := ed25519.GenerateKey(nil)
	withTrustedKey(t, pub)

	limited := entLicense()
	limited.NodeLimit = 1
	svc := &EntitlementService{db: database}
	if _, err := svc.Activate(context.Background(), mintToken(t, priv, limited), uuid.New()); err != nil {
		t.Fatalf("activate: %v", err)
	}

	org := meshdb.Organization{Name: "o", Slug: "o"}
	if err := database.Create(&org).Error; err != nil {
		t.Fatalf("org: %v", err)
	}
	// One gateway (never metered) and two workers — over a limit of 1.
	for _, n := range []meshdb.Node{
		{OrganizationID: org.ID, Name: "gw", TailscaleIP: "100.64.0.1", K3sRole: meshdb.K3sRoleServer},
		{OrganizationID: org.ID, Name: "w1", TailscaleIP: "100.64.0.2", K3sRole: meshdb.K3sRoleAgent},
		{OrganizationID: org.ID, Name: "w2", TailscaleIP: "100.64.0.3", K3sRole: meshdb.K3sRoleAgent},
	} {
		if err := database.Create(&n).Error; err != nil {
			t.Fatalf("node %s: %v", n.Name, err)
		}
	}

	svc.Invalidate()
	st, err := svc.Describe(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if st.NodeCount != 2 {
		t.Fatalf("the gateway must not be metered: want 2 workers, got %d", st.NodeCount)
	}
	if !st.OverLimit {
		t.Fatal("2 workers against a limit of 1 must report over-limit")
	}
	// Still licensed and still granting — a warning, not a gate.
	if !st.Licensed || !svc.Entitled(context.Background(), uuid.Nil, "sso") {
		t.Fatal("exceeding the node limit must not disable the license")
	}
}
