package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
)

// After a provider's OAuth callback the browser is sent back to a console. It
// should be the one the person came from - their session belongs to that
// console's address - and never one a forged Host header names.
func TestCallbackReturnsToTheConsoleItCameThroughAndNowhereElse(t *testing.T) {
	database := newAuthzTestDB(t)
	cfg := &config.Config{Domain: "old.test", FrontendURL: "https://console.old.test"}
	svc := service.New(database, cfg)
	h := New(cfg, svc)
	ctx := context.Background()

	org := &db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	if err := database.Create(org).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Domains.CreateSeeded(ctx, org.ID, "old.test", db.DNSModeDelegation); err != nil {
		t.Fatal(err)
	}
	next, err := svc.Domains.Create(ctx, org.ID, "new.test", db.DNSModeDelegation)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(next).Update("verified", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Domains.SetPrimary(ctx, next.ID); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ host, want string }{
		// Arrived through the old primary, which still serves: back to its console.
		{"api.old.test", "https://console.old.test"},
		{"console.old.test:443", "https://console.old.test"},
		{"api.new.test", "https://console.new.test"},
		// A name this gateway does not serve the platform on is not a place to
		// send anyone: the primary's console instead.
		{"api.evil.example", "https://console.new.test"},
		{"", "https://console.new.test"},
	} {
		r := httptest.NewRequest("GET", "/api/v1/gitea/callback", nil)
		r.Host = c.host
		if got := h.consoleAfterCallback(r); got != c.want {
			t.Errorf("Host %q: redirect to %q, want %q", c.host, got, c.want)
		}
	}
}
