package client_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/meshploy/apps/cli/internal/client"
)

func TestListVolumes(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/volumes",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Volume{
					{ID: "v1", Name: "data", Slug: "data", StorageGB: 20},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	vols, err := c.ListVolumes(testOrg, testProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vols) != 1 {
		t.Errorf("want 1 volume, got %d", len(vols))
	}
	if vols[0].StorageGB != 20 {
		t.Errorf("want 20GB, got %d", vols[0].StorageGB)
	}
}

func TestCreateVolume(t *testing.T) {
	var body map[string]any
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/volumes",
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&body)
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Volume{ID: "v2", Name: "logs", StorageGB: 10})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	v, err := c.CreateVolume(testOrg, testProject, client.CreateVolumeBody{Name: "logs", StorageGB: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "logs" {
		t.Errorf("want name %q, got %q", "logs", v.Name)
	}
	if body["storage_gb"].(float64) != 10 {
		t.Errorf("want storage_gb=10 in body, got %v", body["storage_gb"])
	}
}

func TestGetVolume(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/volumes/v1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, client.Volume{ID: "v1", Name: "data", StorageGB: 20, Status: "ready"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	v, err := c.GetVolume(testOrg, testProject, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Status != "ready" {
		t.Errorf("want status ready, got %q", v.Status)
	}
}

func TestAttachVolume(t *testing.T) {
	var body map[string]string
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/volumes/v1/mounts",
			fn: func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&body)
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.VolumeMount{ID: "m1", VolumeID: "v1", ServiceID: testService, MountPath: "/data"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	m, err := c.AttachVolume(testOrg, testProject, "v1", client.AttachVolumeBody{
		ServiceID: testService,
		MountPath: "/data",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.MountPath != "/data" {
		t.Errorf("want mount path /data, got %q", m.MountPath)
	}
}

func TestDetachVolume(t *testing.T) {
	called := false
	srv := newServer(t, []routeHandler{
		{
			method: "DELETE",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/volumes/v1/mounts/m1",
			fn: func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	if err := c.DetachVolume(testOrg, testProject, "v1", "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DELETE endpoint was not called")
	}
}

func TestGetVolumeByName(t *testing.T) {
	volumes := []client.Volume{
		{ID: "v1", Name: "data", Slug: "data"},
		{ID: "v2", Name: "logs", Slug: "logs"},
	}
	handler := routeHandler{
		method: "GET",
		path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/volumes",
		fn: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, volumes)
		},
	}

	t.Run("by name", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		v, err := c.GetVolumeByName(testOrg, testProject, "logs")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.ID != "v2" {
			t.Errorf("want v2, got %q", v.ID)
		}
	})

	t.Run("by slug", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		v, err := c.GetVolumeByName(testOrg, testProject, "data")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.ID != "v1" {
			t.Errorf("want v1, got %q", v.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := newServer(t, []routeHandler{handler})
		defer srv.Close()
		c := client.New(srv.URL, "token")
		_, err := c.GetVolumeByName(testOrg, testProject, "ghost")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
