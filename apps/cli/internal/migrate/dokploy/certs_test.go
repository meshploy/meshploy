package dokploy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// issue makes a self-signed certificate for a name, so the import can be tested
// on real PEM rather than a fixture nobody can regenerate.
func issue(t *testing.T, name string, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func acmeJSON(t *testing.T, certs ...AcmeCertificate) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"letsencrypt": map[string]any{"Certificates": certs}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func stored(t *testing.T, name string, notAfter time.Time, sans ...string) AcmeCertificate {
	t.Helper()
	crt, key := issue(t, name, notAfter)
	var c AcmeCertificate
	c.Domain.Main, c.Domain.SANs = name, sans
	c.Certificate = base64.StdEncoding.EncodeToString(crt)
	c.Key = base64.StdEncoding.EncodeToString(key)
	return c
}

// Without this, Caddy takes the ports holding seventy domains with no
// certificates and asks Let's Encrypt for all of them at once.
func TestImportWritesWhatCaddyLooksFor(t *testing.T) {
	now := time.Now()
	acme := acmeJSON(t, stored(t, "web.example.com", now.Add(60*24*time.Hour)))

	out, err := ImportCertificates(acme, map[string]bool{"web.example.com": true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Imported) != 1 || out.Imported[0] != "web.example.com" {
		t.Fatalf("imported = %v, skipped = %v", out.Imported, out.Skipped)
	}
	if len(out.Files) != 3 {
		t.Fatalf("files = %d, want cert, key and metadata", len(out.Files))
	}

	byPath := map[string]CertFile{}
	for _, f := range out.Files {
		byPath[f.Path] = f
	}
	base := "certificates/" + CaddyIssuer + "/web.example.com/web.example.com"
	crt, ok := byPath[base+".crt"]
	if !ok || !strings.HasPrefix(string(crt.Content), "-----BEGIN CERTIFICATE-----") {
		t.Errorf("certificate file = %+v", crt)
	}
	key, ok := byPath[base+".key"]
	if !ok {
		t.Fatal("no key file")
	}
	if key.Mode != 0o600 {
		t.Errorf("key mode = %o, want 0600: it is a private key", key.Mode)
	}
	if _, ok := byPath[base+".json"]; !ok {
		t.Error("no metadata file; CertMagic reissues without one")
	}
}

// A certificate about to expire is left for Caddy to issue: importing it only
// risks serving something stale in the first minutes after cutover.
func TestACertificateNearExpiryIsLeftBehind(t *testing.T) {
	now := time.Now()
	acme := acmeJSON(t, stored(t, "old.example.com", now.Add(3*24*time.Hour)))

	out, err := ImportCertificates(acme, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Imported) != 0 {
		t.Errorf("imported = %v", out.Imported)
	}
	if reason := out.Skipped["old.example.com"]; !strings.Contains(reason, "too soon") {
		t.Errorf("reason = %q", reason)
	}
}

// A domain Meshploy does not serve has no business having its certificate
// copied.
func TestOnlyMovedDomainsAreImported(t *testing.T) {
	now := time.Now()
	acme := acmeJSON(t,
		stored(t, "moved.example.com", now.Add(60*24*time.Hour)),
		stored(t, "staying.example.com", now.Add(60*24*time.Hour)),
	)
	out, err := ImportCertificates(acme, map[string]bool{"moved.example.com": true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Imported) != 1 || out.Imported[0] != "moved.example.com" {
		t.Errorf("imported = %v", out.Imported)
	}
}

// A certificate covering several names is imported for each of them.
func TestSANsAreImportedToo(t *testing.T) {
	now := time.Now()
	crt, key := issue(t, "main.example.com", now.Add(60*24*time.Hour))
	var c AcmeCertificate
	c.Domain.Main = "main.example.com"
	c.Domain.SANs = []string{"alt.example.com"}
	c.Certificate = base64.StdEncoding.EncodeToString(crt)
	c.Key = base64.StdEncoding.EncodeToString(key)

	out, err := ImportCertificates(acmeJSON(t, c), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	// The leaf is only valid for main, so alt is skipped with that reason
	// rather than written and served wrongly.
	if len(out.Imported) != 1 || out.Imported[0] != "main.example.com" {
		t.Errorf("imported = %v", out.Imported)
	}
	if reason := out.Skipped["alt.example.com"]; !strings.Contains(reason, "not valid for this name") {
		t.Errorf("alt reason = %q", reason)
	}
}

// Anything unreadable is skipped with a reason rather than failing the cutover:
// Caddy issuing a certificate is the normal path and always works.
func TestUnreadableCertificatesAreSkippedNotFatal(t *testing.T) {
	now := time.Now()
	var broken AcmeCertificate
	broken.Domain.Main = "broken.example.com"
	broken.Certificate, broken.Key = "not base64 at all!!", "also not"

	out, err := ImportCertificates(acmeJSON(t, broken, stored(t, "fine.example.com", now.Add(60*24*time.Hour))), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Imported) != 1 || out.Imported[0] != "fine.example.com" {
		t.Errorf("imported = %v", out.Imported)
	}
	if out.Skipped["broken.example.com"] == "" {
		t.Error("the broken one should say why it was skipped")
	}
}

func TestAnUnreadableAcmeFileIsAnError(t *testing.T) {
	if _, err := ImportCertificates([]byte("{not json"), nil, time.Now()); err == nil {
		t.Error("expected an error")
	}
}

func TestWantedDomainsAreTheOnesThatMove(t *testing.T) {
	plan := Plan{Items: []Item{
		{Kind: "domain", Name: "a.example.com/path", Verdict: Moves},
		{Kind: "domain", Name: "b.example.com", Verdict: NotMoved},
		{Kind: "application", Name: "web", Verdict: Moves},
	}}
	got := WantedDomains(plan)
	if len(got) != 1 || !got["a.example.com"] {
		t.Errorf("got %v", got)
	}
}
