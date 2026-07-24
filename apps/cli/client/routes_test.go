package client_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
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
