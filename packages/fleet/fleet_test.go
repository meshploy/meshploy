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

func TestCallSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now()
	c := Call{ID: "c1", Account: "acc", App: "Claude", Method: "POST", Path: "/api/v1/projects", Body: []byte(`{"name":"x"}`), Expires: now.Add(30 * time.Second)}
	SignCall("lnk", &c, priv)
	if err := VerifyCall("lnk", c, pub, now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Call){
		"body":    func(c *Call) { c.Body = []byte(`{"name":"y"}`) },
		"path":    func(c *Call) { c.Path = "/api/v1/nodes" },
		"account": func(c *Call) { c.Account = "other" },
		"app":     func(c *Call) { c.App = "Cursor" },
		"email":   func(c *Call) { c.Email = "someone@example.com" },
	} {
		d := c
		change(&d)
		if VerifyCall("lnk", d, pub, now) == nil {
			t.Errorf("a call with a changed %s verifies", name)
		}
	}
	if VerifyCall("other-link", c, pub, now) == nil {
		t.Error("a call for one link verifies on another")
	}
	if VerifyCall("lnk", c, pub, now.Add(time.Minute)) == nil {
		t.Error("an expired call verifies")
	}
	// A managed install's call names its person by email, which is signed.
	m := c
	m.Email = "ada@example.com"
	SignCall("lnk", &m, priv)
	if err := VerifyCall("lnk", m, pub, now); err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"eve@example.com", ""} {
		d := m
		d.Email = other
		if VerifyCall("lnk", d, pub, now) == nil {
			t.Errorf("a call for ada verifies with the email %q", other)
		}
	}
	far := Call{ID: "c2", Expires: now.Add(time.Hour)}
	SignCall("lnk", &far, priv)
	if VerifyCall("lnk", far, pub, now) == nil {
		t.Error("a call that lasts an hour verifies")
	}
}

func TestPersonSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	p := Person{Code: "abc", Account: "acc", Email: "a@example.com"}
	SignPerson(TypePersonLinked, "lnk", &p, priv)
	if err := VerifyPerson(TypePersonLinked, "lnk", p, pub); err != nil {
		t.Fatal(err)
	}
	if VerifyPerson(TypePersonUnlinked, "lnk", p, pub) == nil {
		t.Error("a link frame's signature passes as an unlink")
	}
	p.Account = "other"
	if VerifyPerson(TypePersonLinked, "lnk", p, pub) == nil {
		t.Error("a changed account verifies")
	}
}
