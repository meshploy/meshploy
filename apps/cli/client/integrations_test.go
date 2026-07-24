package client_test

import (
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

// ─── Git integrations ─────────────────────────────────────────────────────────

func TestListGitIntegrations(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/git-integrations",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.GitIntegration{
					{ID: "gi1", Name: "github-main", Provider: "github", Connected: true},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	integrations, err := c.ListGitIntegrations(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(integrations) != 1 {
		t.Errorf("want 1 integration, got %d", len(integrations))
	}
	if !integrations[0].Connected {
		t.Error("expected integration to be connected")
	}
}

func TestCreatePATIntegration(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/git-integrations",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.GitIntegration{
					ID:         "gi2",
					Name:       "gitlab-selfhosted",
					Provider:   "gitlab",
					AuthMethod: "pat",
					Connected:  true,
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	gi, err := c.CreatePATIntegration(testOrg, "gitlab", "gitlab-selfhosted", "https://gitlab.corp.com", "", "glpat-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gi.Provider != "gitlab" {
		t.Errorf("want provider gitlab, got %q", gi.Provider)
	}
}

func TestDeleteGitIntegration(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/git-integrations/gi1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteGitIntegration(testOrg, "gi1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

// ─── Registry integrations ────────────────────────────────────────────────────

func TestListRegistryIntegrations(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/registry-integrations",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.RegistryIntegration{
					{ID: "ri1", Name: "dockerhub", Provider: "dockerhub"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	registries, err := c.ListRegistryIntegrations(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(registries) != 1 {
		t.Errorf("want 1 registry, got %d", len(registries))
	}
}

func TestCreateRegistryIntegration(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/registry-integrations",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.RegistryIntegration{
					ID:       "ri2",
					Name:     "ghcr",
					Provider: "ghcr",
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	ri, err := c.CreateRegistryIntegration(testOrg, client.CreateRegistryBody{
		Name:     "ghcr",
		Provider: "ghcr",
		Username: "user",
		Password: "token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.Provider != "ghcr" {
		t.Errorf("want provider ghcr, got %q", ri.Provider)
	}
}

func TestDeleteRegistryIntegration(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/registry-integrations/ri1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteRegistryIntegration(testOrg, "ri1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

// ─── Storage integrations ─────────────────────────────────────────────────────

func TestListStorageIntegrations(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/storage-integrations",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.StorageIntegration{
					{ID: "si1", Name: "backups-s3", Provider: "s3", Bucket: "my-backups"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	stores, err := c.ListStorageIntegrations(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stores) != 1 {
		t.Errorf("want 1 storage integration, got %d", len(stores))
	}
	if stores[0].Bucket != "my-backups" {
		t.Errorf("want bucket my-backups, got %q", stores[0].Bucket)
	}
}

func TestCreateStorageIntegration(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/storage-integrations",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.StorageIntegration{
					ID:     "si2",
					Name:   "r2-store",
					Bucket: "cf-bucket",
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	si, err := c.CreateStorageIntegration(testOrg, client.CreateStorageBody{
		Name:            "r2-store",
		Provider:        "r2",
		Bucket:          "cf-bucket",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if si.Bucket != "cf-bucket" {
		t.Errorf("want bucket cf-bucket, got %q", si.Bucket)
	}
}

func TestDeleteStorageIntegration(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/storage-integrations/si1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteStorageIntegration(testOrg, "si1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}
