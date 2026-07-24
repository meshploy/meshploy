package client_test

import (
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

func TestAuthorizationHeaderSent(t *testing.T) {
	var gotHeader string
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs",
			fn: func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get("Authorization")
				writeJSON(w, []any{})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "my-jwt-token")
	c.ListOrgs()

	if gotHeader != "Bearer my-jwt-token" {
		t.Errorf("want Authorization: Bearer my-jwt-token, got %q", gotHeader)
	}
}

func TestAuthorizationHeaderOmittedWhenEmpty(t *testing.T) {
	var gotHeader string
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/auth/login",
			fn: func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get("Authorization")
				writeJSON(w, map[string]any{"token": "jwt"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "") // no token
	c.Login("u@example.com", "pass")

	if gotHeader != "" {
		t.Errorf("expected no Authorization header for unauthenticated call, got %q", gotHeader)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("not valid json {"))
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	_, err := c.ListOrgs()
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestDoNoContent_ErrorOnHTTP4xx(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/nodes/gone",
			fn: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"title":"not found"}`, http.StatusNotFound)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	err := c.DeleteNode(testOrg, "gone")
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}

func TestLogin(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/auth/login",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"token": "jwt-abc", "totp_required": false})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "")
	out, err := c.Login("user@example.com", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Token != "jwt-abc" {
		t.Errorf("want token %q, got %q", "jwt-abc", out.Token)
	}
	if out.TOTPRequired {
		t.Error("TOTPRequired should be false")
	}
}

func TestLogin_TOTPRequired(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/auth/login",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"token": "", "totp_required": true, "mfa_token": "mfa-xyz"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "")
	out, err := c.Login("user@example.com", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.TOTPRequired {
		t.Error("expected TOTPRequired=true")
	}
	if out.MFAToken != "mfa-xyz" {
		t.Errorf("want mfa_token %q, got %q", "mfa-xyz", out.MFAToken)
	}
}

func TestLogin_BadCredentials(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/auth/login",
			fn: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"title":"unauthorized"}`, http.StatusUnauthorized)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "")
	_, err := c.Login("bad@example.com", "wrong")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListOrgs(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []map[string]string{
					{"id": "o1", "name": "Acme", "slug": "acme"},
					{"id": "o2", "name": "Beta", "slug": "beta"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	orgs, err := c.ListOrgs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orgs) != 2 {
		t.Errorf("want 2 orgs, got %d", len(orgs))
	}
	if orgs[0].Slug != "acme" {
		t.Errorf("want slug %q, got %q", "acme", orgs[0].Slug)
	}
}

func TestListNodes(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/nodes",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []map[string]string{
					{"id": "n1", "name": "gw", "k3s_role": "server", "status": "healthy"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	nodes, err := c.ListNodes(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].K3sRole != "server" {
		t.Errorf("want k3s_role=server, got %q", nodes[0].K3sRole)
	}
}

func TestDeleteNode(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/nodes/n1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteNode(testOrg, "n1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

func TestGetRegistrationToken(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/node-registration-token",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]string{"token": "mreg-abc123"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	tok, err := c.GetRegistrationToken(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "mreg-abc123" {
		t.Errorf("want %q, got %q", "mreg-abc123", tok)
	}
}

func TestRotateRegistrationToken(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/node-registration-token",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]string{"token": "mreg-newtoken"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	tok, err := c.RotateRegistrationToken(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "mreg-newtoken" {
		t.Errorf("want %q, got %q", "mreg-newtoken", tok)
	}
}
