package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/meshploy/packages/client"
)

// stackAPI stands in for the API: it records the last write body and answers
// the lookups the handlers make on the way there.
func stackAPI(t *testing.T, body *map[string]any) *srv {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/org-1/projects/p1/stacks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]client.Stack{{ID: "st1", Name: "infra"}})
			return
		}
		record(t, r, body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client.Stack{ID: "st1", Name: "infra"})
	})
	mux.HandleFunc("/api/v1/orgs/org-1/projects/p1/apply", func(w http.ResponseWriter, r *http.Request) {
		record(t, r, body)
		_ = json.NewEncoder(w).Encode(client.ApplyResult{Stack: &client.Stack{ID: "st1", Name: "infra"}})
	})
	mux.HandleFunc("/api/v1/orgs/org-1/projects/p1/stacks/st1", func(w http.ResponseWriter, r *http.Request) {
		record(t, r, body)
		_ = json.NewEncoder(w).Encode(client.Stack{ID: "st1", Name: "infra"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &srv{c: client.New(ts.URL, "t"), orgID: "org-1"}
}

func record(t *testing.T, r *http.Request, into *map[string]any) {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	*into = m
}

func callTool(t *testing.T, fn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned a transport error: %v", err)
	}
	return res
}

// A stack's variables hold its secrets, so an agent that can write them must
// still not be able to read them back: nothing the tools return carries them.
func TestUpdateStackKeepsWhatItWasNotSent(t *testing.T) {
	var body map[string]any
	s := stackAPI(t, &body)

	res := callTool(t, s.handleUpdateStack, map[string]any{
		"project_id": "p1", "stack_id": "infra",
		"variables": "NEO4J_PASSWORD=s3cr3t",
	})
	if res.IsError {
		t.Fatalf("update failed: %+v", res.Content)
	}
	if _, ok := body["spec"]; ok {
		t.Error("a variables-only update sent a spec, which would overwrite the stored one")
	}
	vars, ok := body["variables"].(map[string]any)
	if !ok || vars["NEO4J_PASSWORD"] != "s3cr3t" {
		t.Errorf("variables not sent: %v", body)
	}
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), "s3cr3t") {
		t.Error("the tool result echoes a secret back to the agent")
	}
}

func TestUpdateStackNeedsSomethingToChange(t *testing.T) {
	var body map[string]any
	s := stackAPI(t, &body)

	res := callTool(t, s.handleUpdateStack, map[string]any{"project_id": "p1", "stack_id": "infra"})
	if !res.IsError {
		t.Fatal("want a tool error when nothing was sent to change")
	}
	if body != nil {
		t.Error("an empty update still reached the API")
	}
}

// A repo with no mode is stored as an inline stack that never syncs, so the
// tool fills the mode in rather than creating something that silently does
// nothing.
func TestCreateStackDefaultsGitMode(t *testing.T) {
	var body map[string]any
	s := stackAPI(t, &body)

	res := callTool(t, s.handleCreateStack, map[string]any{
		"project_id": "p1", "name": "infra",
		"git_repo": "https://github.com/meshploy/meshploy-templates",
	})
	if res.IsError {
		t.Fatalf("create failed: %+v", res.Content)
	}
	if body["git_mode"] != "file" {
		t.Errorf("git_mode = %v, want file", body["git_mode"])
	}
}

// The one-shot path has to carry everything the server cannot get elsewhere:
// the values the manifest interpolates, and the files its configs name.
func TestApplyManifestCarriesVariablesAndFiles(t *testing.T) {
	var body map[string]any
	s := stackAPI(t, &body)

	res := callTool(t, s.handleApplyManifest, map[string]any{
		"project_id": "p1", "name": "infra",
		"spec":      "services:\n  db:\n    image: postgres:16\n",
		"variables": "DB_PASSWORD=s3cr3t",
		"files":     map[string]any{"./realm.json": `{"realm":"procureflow"}`},
	})
	if res.IsError {
		t.Fatalf("apply failed: %+v", res.Content)
	}
	vars, _ := body["variables"].(map[string]any)
	if vars["DB_PASSWORD"] != "s3cr3t" {
		t.Errorf("variables not sent: %v", body["variables"])
	}
	files, _ := body["files"].(map[string]any)
	if files["./realm.json"] != `{"realm":"procureflow"}` {
		t.Errorf("files not sent: %v", body["files"])
	}
}

// A file whose content arrives as something other than text would otherwise be
// dropped silently, leaving a config the agent believes it wrote.
func TestApplyManifestRejectsNonTextFile(t *testing.T) {
	var body map[string]any
	s := stackAPI(t, &body)

	res := callTool(t, s.handleApplyManifest, map[string]any{
		"project_id": "p1", "name": "infra", "spec": "services: {}",
		"files": map[string]any{"./realm.json": map[string]any{"realm": "procureflow"}},
	})
	if !res.IsError {
		t.Fatal("want a tool error for a file that is not text")
	}
}
