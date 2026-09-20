package client_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meshploy/packages/client"
)

// The API requires a slug and refuses a create without one, which meant no
// caller of this library could make a project: not `meshploy project create`,
// not the MCP tool, not the migration.
func TestCreateProjectSendsASlug(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["slug"] == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"detail":"expected required property slug to be present"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "p1", "name": got["name"], "slug": got["slug"]})
	}))
	defer srv.Close()

	p, err := client.New(srv.URL, "t").CreateProject("org", "My Project")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if got["name"] != "My Project" || got["slug"] != "my-project" {
		t.Errorf("sent %v", got)
	}
	if p.Slug != "my-project" {
		t.Errorf("project = %+v", p)
	}
}

// The slug is a Kubernetes namespace, so it may hold only lower-case letters,
// digits and hyphens - and an empty result would be refused, so it has a
// fallback rather than being sent blank.
func TestProjectSlugMatchesWhatKubernetesAccepts(t *testing.T) {
	for in, want := range map[string]string{
		"My Project":      "my-project",
		"docai_db":        "docai-db",
		"  Spaced  Out  ": "spaced-out",
		"UPPER.case":      "upper-case",
		"weird!!chars$$":  "weirdchars",
		"!!!":             "project",
		"":                "project",
		"a--b":            "a-b",
	} {
		if got := client.ProjectSlug(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// The API takes a zone and a list of targets. The friendly body names one
// target inline, and the client translates - which is what `meshploy route
// create` and the migration both needed and neither got.
func TestCreateRouteSendsZoneAndTargets(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1"})
	}))
	defer srv.Close()

	host, svc, paused := "app.example.com", "svc-1", false
	if _, err := client.New(srv.URL, "t").CreateRoute("org", "proj", client.CreateRouteBody{
		Hostname: &host, ServiceID: &svc, Published: &paused,
	}); err != nil {
		t.Fatal(err)
	}

	if got["zone"] != "public" {
		t.Errorf("zone = %v, want a default", got["zone"])
	}
	if got["published"] != false {
		t.Errorf("published = %v, want the route paused", got["published"])
	}
	targets, _ := got["targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("targets = %v", got["targets"])
	}
	target := targets[0].(map[string]any)
	if target["service_id"] != "svc-1" || target["path"] != "/" {
		t.Errorf("target = %v", target)
	}
	// A service target resolves its own port; sending one would be refused.
	if _, ok := target["port"]; ok {
		t.Errorf("a service target should carry no port: %v", target)
	}
	if _, ok := got["service_id"]; ok {
		t.Errorf("the target must not be at the top level: %v", got)
	}
}

// An address target is the one that needs a port, and may need TLS.
func TestCreateRouteWithAnAddressTarget(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1"})
	}))
	defer srv.Close()

	host, ip, port := "app.example.com", "127.0.0.1", 8443
	if _, err := client.New(srv.URL, "t").CreateRoute("org", "proj", client.CreateRouteBody{
		Hostname: &host, TargetIP: &ip, TargetPort: &port, TargetTLS: true,
	}); err != nil {
		t.Fatal(err)
	}
	target := got["targets"].([]any)[0].(map[string]any)
	if target["target_ip"] != "127.0.0.1" || target["port"] != float64(8443) || target["target_tls"] != true {
		t.Errorf("target = %v", target)
	}
}
