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
