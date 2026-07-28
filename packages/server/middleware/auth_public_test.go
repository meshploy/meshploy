package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func public(method, path string) bool {
	return isPublic(httptest.NewRequest(method, path, nil))
}

// Regression guard for the auth bypass this file previously carried.
//
// `publicRules` used to contain the prefix rule "GET /api/", annotated as being
// for the OpenAPI schema. It exempted EVERY GET under /api/ from RequireAuth —
// and Huma serves its spec at /openapi, /docs and /schemas, so it never served
// that purpose. Only per-handler checks stood between an unauthenticated caller
// and the data; a handler that omitted one served openly, which was observed
// against a running binary.
func TestNoBlanketApiExemption(t *testing.T) {
	shouldBeProtected := []string{
		"/api/v1/orgs",
		"/api/v1/entitlements",
		"/api/v1/templates",
		"/api/v1/orgs/11111111-1111-1111-1111-111111111111/agents",
		"/api/v1/orgs/11111111-1111-1111-1111-111111111111/cluster/join-token",
		"/api/v1/ee/hello",
		"/api/",
		"/api/v1/",
	}
	for _, p := range shouldBeProtected {
		if public(http.MethodGet, p) {
			t.Errorf("GET %s must require authentication", p)
		}
	}
}

// Each exemption exists because the route cannot carry an Authorization header.
func TestGenuinelyPublicRoutesStayPublic(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/api/v1/auth/status"},
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/register"},
		{http.MethodPost, "/api/v1/auth/totp"},
		{http.MethodPost, "/api/v1/auth/recovery"},
		{http.MethodPost, "/api/v1/nodes/self-register"},
		{http.MethodDelete, "/api/v1/nodes/self-deregister"},
		{http.MethodGet, "/api/v1/invitations/abc123"},
		{http.MethodPost, "/api/v1/invitations/abc123/accept"},
		{http.MethodPost, "/api/v1/webhooks/github/11111111-1111-1111-1111-111111111111"},
		{http.MethodPost, "/api/v1/webhooks/deploy/11111111-1111-1111-1111-111111111111"},
		{http.MethodGet, "/api/v1/github/callback"},
		{http.MethodGet, "/api/v1/github/app-callback"},
		{http.MethodGet, "/api/v1/gitlab/callback"},
		{http.MethodGet, "/api/v1/gitea/callback"},
		// WebSocket terminals — the browser API cannot set headers, so these
		// validate a JWT from ?token= themselves.
		{http.MethodGet, "/api/v1/orgs/o1/nodes/n1/terminal"},
		{http.MethodGet, "/api/v1/orgs/o1/projects/p1/services/s1/pods/pod1/terminal"},
	}
	for _, c := range cases {
		if !public(c.method, c.path) {
			t.Errorf("%s %s must stay public — it cannot send an Authorization header", c.method, c.path)
		}
	}
}

// Rules are method-scoped: exempting a path for one method must not exempt it
// for another.
func TestExemptionsAreMethodScoped(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodPost, "/health"},
		{http.MethodGet, "/api/v1/auth/login"},
		{http.MethodGet, "/api/v1/nodes/self-register"},
		{http.MethodGet, "/api/v1/webhooks/github/x"},
		{http.MethodDelete, "/api/v1/invitations/abc"},
		{http.MethodPost, "/api/v1/github/callback"},
	}
	for _, c := range cases {
		if public(c.method, c.path) {
			t.Errorf("%s %s must not be public — the rule is for a different method", c.method, c.path)
		}
	}
}

// Anchoring: a path that merely *contains* an exempt pattern is not exempt.
// The old matcher used strings.Contains for path-only rules, so any route whose
// path embedded "/terminal" or "/self-register" skipped authentication.
func TestPatternsAreAnchoredNotSubstrings(t *testing.T) {
	shouldBeProtected := []struct{ method, path string }{
		// "/terminal" is a suffix rule — an embedded occurrence must not match.
		{http.MethodGet, "/api/v1/orgs/o1/terminal/secrets"},
		{http.MethodGet, "/api/v1/terminals"},
		{http.MethodGet, "/mcp/terminal/x"},
		// "/self-register" is exact — neighbouring paths must not match.
		{http.MethodPost, "/api/v1/nodes/self-register/all"},
		{http.MethodPost, "/api/v1/evil/self-register"},
		// Prefix rules must not match a longer, different prefix.
		{http.MethodPost, "/api/v1/webhooksX/github/1"},
		{http.MethodGet, "/api/v1/invitationsX/abc"},
	}
	for _, c := range shouldBeProtected {
		if public(c.method, c.path) {
			t.Errorf("%s %s must require authentication — the pattern is anchored", c.method, c.path)
		}
	}
}

// Huma's own endpoints are not under /api/, which is why the old blanket rule
// never served its stated purpose. They are not exempt; expose them
// deliberately if that is ever wanted.
func TestHumaDocEndpointsAreNotSilentlyExempt(t *testing.T) {
	for _, p := range []string{"/docs", "/openapi.json", "/openapi.yaml", "/schemas/HealthOutputBody.json"} {
		if public(http.MethodGet, p) {
			t.Errorf("GET %s is not in the allowlist and must not be exempt by accident", p)
		}
	}
}
