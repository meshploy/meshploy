package fleet

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	in := Token{URL: "https://connect.meshploy.com/", Secret: "s3cret"}
	out, err := ParseToken("  " + in.String() + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if out.URL != "https://connect.meshploy.com" || out.Secret != "s3cret" {
		t.Fatalf("got %+v", out)
	}
}

func TestTokenRefusesPlainHTTPAwayFromLoopback(t *testing.T) {
	if _, err := ParseToken(Token{URL: "http://localhost:4200", Secret: "x"}.String()); err != nil {
		t.Errorf("a local account plane over http is refused: %v", err)
	}
	_, err := ParseToken(Token{URL: "http://connect.example.com", Secret: "x"}.String())
	if err == nil || !strings.Contains(err.Error(), "not https") {
		t.Errorf("plain http to a remote host is accepted: %v", err)
	}
	for _, bad := range []string{"", "mlic-abc", "mflt-%%%", Token{URL: "https://x"}.String()} {
		if _, err := ParseToken(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestConnectSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now()
	h := SignConnect("lnk_1", priv, now)
	if err := VerifyConnect(h[HeaderLink], h[HeaderTime], h[HeaderSignature], pub, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyConnect("lnk_2", h[HeaderTime], h[HeaderSignature], pub, now); err == nil {
		t.Error("a signature for one link opens another")
	}
	if err := VerifyConnect(h[HeaderLink], h[HeaderTime], h[HeaderSignature], pub, now.Add(10*time.Minute)); err == nil {
		t.Error("an old signature is accepted")
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if err := VerifyConnect(h[HeaderLink], h[HeaderTime], h[HeaderSignature], other, now); err == nil {
		t.Error("another key's signature is accepted")
	}
}
