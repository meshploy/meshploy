package client_test

import (
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

func TestListProjects(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Project{
					{ID: "p1", Name: "Alpha", Slug: "alpha"},
					{ID: "p2", Name: "Beta", Slug: "beta"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	projects, err := c.ListProjects(testOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("want 2 projects, got %d", len(projects))
	}
}

func TestCreateProject(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Project{ID: "p3", Name: "Gamma", Slug: "gamma"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	p, err := c.CreateProject(testOrg, "Gamma")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Slug != "gamma" {
		t.Errorf("want slug %q, got %q", "gamma", p.Slug)
	}
}

func TestDeleteProject(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/projects/p1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteProject(testOrg, "p1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

func TestGetProjectBySlugOrID(t *testing.T) {
	projects := []client.Project{
		{ID: "p1", Name: "Alpha", Slug: "alpha"},
		{ID: "p2", Name: "Beta", Slug: "beta"},
	}
	handler := routeHandler{
		method: "GET",
		path:   "/api/v1/orgs/" + testOrg + "/projects",
		fn: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, projects)
		},
	}

	t.Run("resolve by ID", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		p, err := c.GetProjectBySlugOrID(testOrg, "p2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "Beta" {
			t.Errorf("want Beta, got %q", p.Name)
		}
	})

	t.Run("resolve by slug", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		p, err := c.GetProjectBySlugOrID(testOrg, "alpha")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.ID != "p1" {
			t.Errorf("want p1, got %q", p.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		_, err := c.GetProjectBySlugOrID(testOrg, "nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
