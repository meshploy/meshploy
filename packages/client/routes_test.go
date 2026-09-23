package client_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/meshploy/packages/client"
)

func TestListRoutes(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/routes",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Route{
					{ID: "r1", Hostname: "app.internal.example.com", TargetIP: "100.64.0.2", TargetPort: 3000},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	routes, err := c.ListRoutes(testOrg, testProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("want 1 route, got %d", len(routes))
	}
	if routes[0].TargetPort != 3000 {
		t.Errorf("want port 3000, got %d", routes[0].TargetPort)
	}
}

func TestCreateRoute(t *testing.T) {
	hostname := "new.internal.example.com"
	var captured map[string]any
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/routes",
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&captured)
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Route{
					ID:       "r2",
					Hostname: hostname,
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	body := client.CreateRouteBody{Hostname: &hostname}
	route, err := c.CreateRoute(testOrg, testProject, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Hostname != hostname {
		t.Errorf("want hostname %q, got %q", hostname, route.Hostname)
	}
}

func TestDeleteRoute(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/routes/r1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteRoute(testOrg, testProject, "r1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

// The API sends a custom hostname's proof fields; dropping them would leave
// the CLI and MCP unable to say why a new route gets no certificate.
func TestRouteCarriesItsOwnershipProof(t *testing.T) {
	var got client.Route
	if err := json.Unmarshal([]byte(`{"id":"r1","hostname":"store.customer.example","domain_id":null,
		"custom_domain_verified":false,"custom_domain_verify_token":"abc123"}`), &got); err != nil {
		t.Fatal(err)
	}
	if !got.IsCustomHostname() || !got.NeedsOwnershipProof() {
		t.Fatalf("a custom hostname without proof must say so: %+v", got)
	}
	if got.CustomDomainVerifyToken != "abc123" || got.VerifyRecordName() != "_meshploy-verify.store.customer.example" {
		t.Fatalf("record = %s %s", got.VerifyRecordName(), got.CustomDomainVerifyToken)
	}

	var sub client.Route
	_ = json.Unmarshal([]byte(`{"id":"r2","hostname":"shop.example.com","domain_id":"d1"}`), &sub)
	if sub.IsCustomHostname() || sub.NeedsOwnershipProof() {
		t.Fatal("a route on a base domain has nothing to prove")
	}
}

func TestVerifyRouteHostname(t *testing.T) {
	srv := newServer(t, []routeHandler{{
		method: "POST",
		path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/routes/r1/verify-hostname",
		fn: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"id": "r1", "hostname": "store.customer.example", "custom_domain_verified": true})
		},
	}})
	defer srv.Close()

	r, err := client.New(srv.URL, "token").VerifyRouteHostname(testOrg, testProject, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r.NeedsOwnershipProof() {
		t.Fatal("a verified hostname needs no proof")
	}
}
