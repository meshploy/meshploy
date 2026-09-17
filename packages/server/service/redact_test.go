package service

import (
	"strings"
	"testing"
)

// Each case is a line a real build or job could print. What must go is gone,
// and the words around it stay, so the alert still says what failed.
func TestRedactSecrets(t *testing.T) {
	known := []string{"hunter2-but-longer", "true", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAxyzxyzxyzxyzxyz\n-----END RSA PRIVATE KEY-----"}
	cases := []struct{ in, gone, kept string }{
		{"connecting with hunter2-but-longer failed", "hunter2-but-longer", "failed"},
		{"DEBUG=true", "", "DEBUG=true"}, // too short to be treated as a secret
		{"fatal: could not read from https://oauth2:glpat-abcdefghijklmnopqrst@gitlab.com/x.git", "glpat-", "gitlab.com/x.git"},
		{"dial postgres://app:s3cretpass@db:5432/app refused", "s3cretpass", "refused"},
		{"Authorization: Bearer abc.def.ghijklmnop", "abc.def.ghijklmnop", "Authorization"},
		{"STRIPE_SECRET_KEY=sk_live_51Habcdefgh", "sk_live_51Habcdefgh", "STRIPE_SECRET_KEY"},
		{`"password": "correct horse"`, "correct", "password"},
		{"token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB leaked", "ghp_0123", "leaked"},
		{"AWS key AKIAABCDEFGHIJKLMNOP used", "AKIAABCDEFGHIJKLMNOP", "used"},
		{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTYifQ.abcdefghijk expired", "eyJhbGci", "expired"},
		{"key line MIIEowIBAAKCAQEAxyzxyzxyzxyzxyz printed", "MIIEowIBAAKCAQEA", "printed"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\nnext", "b3BlbnNzaC1rZXktdjEAAAAA", "next"},
		{"b3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----\nnext", "b3BlbnNzaC1rZXktdjEAAAAA", "next"},
		{"npm ERR! missing script: build", "", "npm ERR! missing script: build"},
		{"smtp auth: 535 Authentication Failed", "", "smtp auth: 535 Authentication Failed"},
	}
	for _, c := range cases {
		got := redactSecrets(c.in, known)
		if c.gone != "" && strings.Contains(got, c.gone) {
			t.Errorf("%q\n  -> %q still has %q", c.in, got, c.gone)
		}
		if !strings.Contains(got, c.kept) {
			t.Errorf("%q\n  -> %q lost %q", c.in, got, c.kept)
		}
	}
}

// The tail is filtered before it is cut, so a key whose start falls outside
// the kept lines does not leak its body.
func TestFailureTailFiltersBeforeCutting(t *testing.T) {
	log := "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("QUJDREVGR0hJSktMTU5PUFFSU1RVVldY\n", 12) + "-----END RSA PRIVATE KEY-----\nbuild failed"
	got := failureTail(log, nil)
	if strings.Contains(got, "QUJDREVGR0hJSktMTU5PUFFSU1RVVldY") || !strings.Contains(got, "build failed") {
		t.Errorf("tail = %q", got)
	}
}
