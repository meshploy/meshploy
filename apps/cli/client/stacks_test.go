package client_test

import (
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

func TestListStacks(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Stack{
					{ID: "st1", Name: "prod-stack", Status: "healthy"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	stacks, err := c.ListStacks(testOrg, testProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stacks) != 1 {
		t.Errorf("want 1 stack, got %d", len(stacks))
	}
}

func TestCreateStack(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Stack{ID: "st2", Name: "dev-stack"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	s, err := c.CreateStack(testOrg, testProject, client.CreateStackBody{Name: "dev-stack", Spec: "services: []"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "dev-stack" {
		t.Errorf("want name %q, got %q", "dev-stack", s.Name)
	}
}

func TestGetStack(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks/st1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, client.Stack{ID: "st1", Name: "prod-stack", Status: "healthy"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	s, err := c.GetStack(testOrg, testProject, "st1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Status != "healthy" {
		t.Errorf("want status healthy, got %q", s.Status)
	}
}

func TestApplyStack(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks/st1/apply",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, client.ApplyResult{
					Created: []string{"svc-a"},
					Updated: []string{"svc-b"},
					Deleted: []string{},
					Errors:  []string{},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	result, err := c.ApplyStack(testOrg, testProject, "st1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Created) != 1 || result.Created[0] != "svc-a" {
		t.Errorf("unexpected created: %v", result.Created)
	}
}

func TestDeleteStack(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks/st1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteStack(testOrg, testProject, "st1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

func TestListStackServices(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks/st1/services",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Service{
					{ID: "svc-a", Name: "web"},
					{ID: "svc-b", Name: "worker"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	svcs, err := c.ListStackServices(testOrg, testProject, "st1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 2 {
		t.Errorf("want 2 services, got %d", len(svcs))
	}
}

func TestGetStackByName(t *testing.T) {
	stacks := []client.Stack{
		{ID: "st1", Name: "prod-stack"},
		{ID: "st2", Name: "dev-stack"},
	}
	handler := routeHandler{
		method: "GET",
		path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/stacks",
		fn: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, stacks)
		},
	}

	t.Run("by name", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		s, err := c.GetStackByName(testOrg, testProject, "dev-stack")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.ID != "st2" {
			t.Errorf("want st2, got %q", s.ID)
		}
	})

	t.Run("by ID", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		s, err := c.GetStackByName(testOrg, testProject, "st1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Name != "prod-stack" {
			t.Errorf("want prod-stack, got %q", s.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		_, err := c.GetStackByName(testOrg, testProject, "ghost")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
