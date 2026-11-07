package agent

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	appk8s "github.com/meshploy/packages/server/k8s"
)

func TestIdentityIsMadeOnceAndKeptPrivate(t *testing.T) {
	id := Identity{Dir: t.TempDir() + "/cluster-agent"}
	created, err := id.Ensure()
	if err != nil || !created {
		t.Fatalf("first Ensure: %v, created=%v", err, created)
	}
	for _, p := range []string{id.KeyPath(), id.CertPath(), id.TokenPath()} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v, want 0600", p, st.Mode().Perm())
		}
	}
	if st, _ := os.Stat(id.Dir); st.Mode().Perm() != 0o700 {
		t.Errorf("dir is %v, want 0700", st.Mode().Perm())
	}
	token, err := id.Token()
	if err != nil || !strings.HasPrefix(token, TokenPrefix) {
		t.Fatalf("token %q: %v", token, err)
	}
	certPEM, _ := id.CertPEM()

	created, err = id.Ensure()
	if err != nil || created {
		t.Fatalf("second Ensure: %v, created=%v", err, created)
	}
	if again, _ := id.Token(); again != token {
		t.Error("a second Ensure changed the token")
	}
	if again, _ := id.CertPEM(); string(again) != string(certPEM) {
		t.Error("a second Ensure changed the certificate")
	}

	// A half-made identity is made again whole.
	os.Remove(id.KeyPath())
	created, err = id.Ensure()
	if err != nil || !created {
		t.Fatalf("Ensure after losing the key: %v, created=%v", err, created)
	}
	if _, err := id.TLSCertificate(); err != nil {
		t.Fatalf("certificate and key do not match after remaking: %v", err)
	}
	if again, _ := id.Token(); again == token {
		t.Error("a remade identity kept the old token")
	}
}

func TestIdentityCertificateNamesTheAgent(t *testing.T) {
	id := Identity{Dir: t.TempDir()}
	if _, err := id.Ensure(); err != nil {
		t.Fatal(err)
	}
	raw, _ := id.CertPEM()
	block, _ := pem.Decode(raw)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname(appk8s.ClusterAgentServerName); err != nil {
		t.Errorf("certificate does not name the agent: %v", err)
	}
}

func TestTokenFileIsChecked(t *testing.T) {
	id := Identity{Dir: t.TempDir()}
	for _, bad := range []string{"", "mcag-short", "mreg-" + strings.Repeat("a", 64), TokenPrefix + strings.Repeat("a", 63)} {
		os.WriteFile(id.TokenPath(), []byte(bad), 0o600)
		if _, err := id.Token(); err == nil {
			t.Errorf("token %q accepted", bad)
		}
	}
}

func TestTokenMatches(t *testing.T) {
	want := TokenPrefix + strings.Repeat("ab", 32)
	if !TokenMatches(want, want) {
		t.Error("the right token did not match")
	}
	for _, bad := range []string{"", want[:len(want)-1], want + "x", strings.ToUpper(want)} {
		if TokenMatches(bad, want) {
			t.Errorf("%q matched", bad)
		}
	}
	if TokenMatches("", "") {
		t.Error("an empty token matched an agent with none")
	}
}

func TestGateLetsOnlyTheAPIThrough(t *testing.T) {
	token := TokenPrefix + strings.Repeat("cd", 32)
	reached := 0
	g := Gate{From: []string{"100.64.0.1"}, Token: token, Next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++ })}
	cases := []struct {
		name, from, auth string
		ok               bool
	}{
		{"the API", "100.64.0.1:51000", "Bearer " + token, true},
		{"the API, IPv4 in IPv6", "[::ffff:100.64.0.1]:51000", "Bearer " + token, true},
		{"another machine with the token", "100.64.0.9:51000", "Bearer " + token, false},
		{"the API without a token", "100.64.0.1:51000", "", false},
		{"the API with a wrong token", "100.64.0.1:51000", "Bearer " + TokenPrefix + strings.Repeat("ef", 32), false},
		{"the token without Bearer", "100.64.0.1:51000", token, false},
		{"basic auth", "100.64.0.1:51000", "Basic " + token, false},
	}
	for _, c := range cases {
		before := reached
		r := httptest.NewRequest("GET", "/api/v1/pods", nil)
		r.RemoteAddr = c.from
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if got := reached > before; got != c.ok {
			t.Errorf("%s: let through=%v, want %v (%d)", c.name, got, c.ok, w.Code)
		}
		if !c.ok && w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", c.name, w.Code)
		}
	}
	if (Gate{Token: token, Next: g.Next}).From != nil {
		t.Fatal("unreachable")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "100.64.0.1:1"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	Gate{Token: token, Next: g.Next}.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Error("a gate with no API addresses let a request through")
	}
}
