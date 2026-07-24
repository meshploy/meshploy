package client_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

// ─── GetEnvVars ───────────────────────────────────────────────────────────────

func TestGetServiceByName(t *testing.T) {
	services := []client.Service{
		{ID: "svc-1", Name: "web"},
		{ID: "svc-2", Name: "worker"},
	}
	handler := routeHandler{
		method: "GET",
		path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services",
		fn: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, services)
		},
	}

	t.Run("by name", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		s, err := c.GetServiceByName(testOrg, testProject, "worker")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.ID != "svc-2" {
			t.Errorf("want svc-2, got %q", s.ID)
		}
	})

	t.Run("by ID", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		s, err := c.GetServiceByName(testOrg, testProject, "svc-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Name != "web" {
			t.Errorf("want web, got %q", s.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		_, err := c.GetServiceByName(testOrg, testProject, "ghost")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetEnvVars(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService + "/env-vars",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]string{"env_vars": "DB_URL=postgres://\nPORT=5432\n"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	got, err := c.GetEnvVars(testOrg, testProject, testService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "DB_URL=postgres://") {
		t.Errorf("expected DB_URL in response, got: %q", got)
	}
}

func TestGetEnvVars_HTTPError(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService + "/env-vars",
			fn: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"title":"forbidden"}`, http.StatusForbidden)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	_, err := c.GetEnvVars(testOrg, testProject, testService)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── SetEnvVars ───────────────────────────────────────────────────────────────

func TestSetEnvVars(t *testing.T) {
	var captured map[string]any
	srv := newServer(t, []routeHandler{
		{
			method: "PATCH",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService,
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&captured)
				w.WriteHeader(http.StatusOK)
				writeJSON(w, map[string]string{"id": testService})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.SetEnvVars(testOrg, testProject, testService, "KEY=val\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured["env_vars"] != "KEY=val\n" {
		t.Errorf("body env_vars mismatch: %v", captured["env_vars"])
	}
}

// ─── GetBuildConfig ───────────────────────────────────────────────────────────

func TestGetBuildConfig(t *testing.T) {
	want := client.BuildConfig{
		ID:        "bc-1",
		ServiceID: testService,
		Builder:   "nixpacks",
		GitRepo:   "https://github.com/org/repo",
		Branch:    "main",
		AutoDeploy: true,
	}
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService + "/build-config",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, want)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	got, err := c.GetBuildConfig(testOrg, testProject, testService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Builder != want.Builder {
		t.Errorf("Builder: want %q, got %q", want.Builder, got.Builder)
	}
	if got.GitRepo != want.GitRepo {
		t.Errorf("GitRepo: want %q, got %q", want.GitRepo, got.GitRepo)
	}
	if got.AutoDeploy != want.AutoDeploy {
		t.Errorf("AutoDeploy: want %v, got %v", want.AutoDeploy, got.AutoDeploy)
	}
}

// ─── UpdateBuildConfig ────────────────────────────────────────────────────────

func TestUpdateBuildConfig(t *testing.T) {
	newBranch := "develop"
	var capturedBody map[string]any
	srv := newServer(t, []routeHandler{
		{
			method: "PATCH",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService + "/build-config",
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&capturedBody)
				writeJSON(w, client.BuildConfig{
					ID:      "bc-1",
					Builder: "nixpacks",
					Branch:  newBranch,
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	got, err := c.UpdateBuildConfig(testOrg, testProject, testService, client.UpdateBuildConfigBody{
		Branch: &newBranch,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Branch != newBranch {
		t.Errorf("Branch: want %q, got %q", newBranch, got.Branch)
	}
	// only the branch field should be in the body (omitempty)
	if _, ok := capturedBody["git_repo"]; ok {
		t.Errorf("git_repo should be omitted from patch body")
	}
}

// ─── GetBuildEnvVars ──────────────────────────────────────────────────────────

func TestGetBuildEnvVars(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService + "/build-config/env-vars",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]string{"build_env_vars": "NODE_ENV=production\n"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	got, err := c.GetBuildEnvVars(testOrg, testProject, testService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "NODE_ENV=production") {
		t.Errorf("expected NODE_ENV in response, got: %q", got)
	}
}

// ─── SetBuildEnvVars ──────────────────────────────────────────────────────────

func TestSetBuildEnvVars(t *testing.T) {
	var capturedBody map[string]any
	srv := newServer(t, []routeHandler{
		{
			method: "PUT",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/services/" + testService + "/build-config/env-vars",
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&capturedBody)
				w.WriteHeader(http.StatusOK)
				writeJSON(w, map[string]string{"build_env_vars": "NODE_ENV=test\n"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.SetBuildEnvVars(testOrg, testProject, testService, "NODE_ENV=test\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["build_env_vars"] != "NODE_ENV=test\n" {
		t.Errorf("body build_env_vars mismatch: %v", capturedBody["build_env_vars"])
	}
}
