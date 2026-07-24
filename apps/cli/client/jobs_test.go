package client_test

import (
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/client"
)

func TestListJobs(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Job{
					{ID: "j1", Name: "daily-report", IsCron: true, Schedule: "0 2 * * *"},
					{ID: "j2", Name: "one-off", IsCron: false},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	jobs, err := c.ListJobs(testOrg, testProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("want 2 jobs, got %d", len(jobs))
	}
}

func TestCreateJob(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Job{ID: "j3", Name: "migrate", IsCron: false})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	j, err := c.CreateJob(testOrg, testProject, client.CreateJobBody{Name: "migrate", Image: "myapp:latest"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if j.Name != "migrate" {
		t.Errorf("want name %q, got %q", "migrate", j.Name)
	}
}

func TestGetJob(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs/j1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, client.Job{ID: "j1", Name: "daily-report", IsCron: true})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	j, err := c.GetJob(testOrg, testProject, "j1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !j.IsCron {
		t.Error("expected IsCron=true")
	}
}

func TestTriggerJob(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs/j1/trigger",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, client.JobRun{ID: "run-1", Status: "running"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	run, err := c.TriggerJob(testOrg, testProject, "j1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != "running" {
		t.Errorf("want status %q, got %q", "running", run.Status)
	}
}

func TestDeleteJob(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs/j1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DeleteJob(testOrg, testProject, "j1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

func TestListJobRuns(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs/j1/runs",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.JobRun{
					{ID: "run-1", Status: "succeeded"},
					{ID: "run-2", Status: "failed"},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	runs, err := c.ListJobRuns(testOrg, testProject, "j1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("want 2 runs, got %d", len(runs))
	}
}

func TestGetJobByName(t *testing.T) {
	jobs := []client.Job{
		{ID: "j1", Name: "daily-report"},
		{ID: "j2", Name: "cleanup"},
	}
	handler := routeHandler{
		method: "GET",
		path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/jobs",
		fn: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, jobs)
		},
	}

	t.Run("by name", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		j, err := c.GetJobByName(testOrg, testProject, "cleanup")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if j.ID != "j2" {
			t.Errorf("want j2, got %q", j.ID)
		}
	})

	t.Run("by ID", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		j, err := c.GetJobByName(testOrg, testProject, "j1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if j.Name != "daily-report" {
			t.Errorf("want daily-report, got %q", j.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		_, err := c.GetJobByName(testOrg, testProject, "ghost")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
