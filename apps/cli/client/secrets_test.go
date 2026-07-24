package client_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

func TestListSecrets(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/secrets",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Secret{
					{ID: "s1", Name: "DB_PASS"},
					{ID: "s2", Name: "API_KEY"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	secrets, err := c.ListSecrets(testOrg, testProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secrets) != 2 {
		t.Errorf("want 2 secrets, got %d", len(secrets))
	}
}

func TestSetSecret(t *testing.T) {
	var body map[string]string
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/secrets",
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&body)
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Secret{ID: "s3", Name: "NEW_KEY"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	s, err := c.SetSecret(testOrg, testProject, "NEW_KEY", "secret-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "NEW_KEY" {
		t.Errorf("want name NEW_KEY, got %q", s.Name)
	}
	if body["name"] != "NEW_KEY" || body["value"] != "secret-value" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestDeleteSecret(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/secrets/s1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteSecret(testOrg, testProject, "s1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}
