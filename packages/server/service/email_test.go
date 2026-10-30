package service

import (
	"strings"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
)

// Every email wears one layout: the link is a button and written out, the
// brand signs it with its own colours, and nothing in it can become markup.
func TestOneLayoutForEveryEmail(t *testing.T) {
	cfg := meshdb.OrgEmailConfig{FromAddress: "no-reply@example.com", FromName: "Meshploy"}
	e := Email{Subject: "Sign in to notes", Heading: "Sign in to <notes>", Body: "The button signs you in.", Button: "Sign in",
		Link: "https://notes.example.com/_meshploy/auth?token=abc", Paste: true, Note: "The link works once, for 15 minutes.", After: "If you did not ask for this, ignore it."}
	e.wear(meshdb.DefaultBrand)
	var html strings.Builder
	if err := emailTemplate.Execute(&html, &e); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Sign in to &lt;notes&gt;", `href="https://notes.example.com/_meshploy/auth?token=abc"`, "copy this address", emailColour, "Powered by Meshploy", "works once"} {
		if !strings.Contains(html.String(), want) {
			t.Errorf("html lacks %q", want)
		}
	}
	text := e.text()
	for _, want := range []string{"Sign in: https://notes.example.com", "works once", "Powered by Meshploy - https://meshploy.com"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if _, err := e.message(cfg, "a@example.com", time.Now()); err != nil {
		t.Fatal(err)
	}

	b := Email{Heading: "Hello", Button: "Go", Link: "https://x"}
	b.wear(meshdb.Brand{Name: "Acme", Custom: true, Accent: "#ff0000", AccentForeground: "#000000"})
	if b.Colour != "#ff0000" || b.ButtonColour != "#ff0000" || b.ButtonText != "#000000" || b.Footer != "Sent by Acme." {
		t.Errorf("a custom brand: %+v", b)
	}
	tone := Email{Heading: "Failed", Colour: "#ef4444"}
	tone.wear(meshdb.Brand{Name: "Acme", Custom: true, PoweredBy: true, Accent: "#ff0000"})
	if tone.Colour != "#ef4444" || !strings.Contains(tone.Footer, "Powered by Meshploy") {
		t.Errorf("a tone keeps its colour over the brand's: %+v", tone)
	}
}
