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

func TestSandboxToolsAreRegistered(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	ms := New(client.New("http://127.0.0.1:0", "t"), "org-1")
	res := ms.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	b, _ := json.Marshal(res)
	for _, tool := range []string{"get_sandbox", "set_sandbox"} {
		if !strings.Contains(string(b), `"`+tool+`"`) {
			t.Errorf("%s is not on the MCP surface", tool)
		}
	}
}

// set_sandbox reaches a project by its slug and a level by its ID, sends the
// switch as asked, and hands the API's refusal back as the tool's error.
func TestSetSandbox(t *testing.T) {
	var got []string
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "p1", "name": "Shop", "slug": "shop"}})
		case strings.HasSuffix(r.URL.Path, "/sandbox") && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			got = append(got, r.URL.Path+" "+strings.TrimSpace(string(body)))
			if strings.Contains(r.URL.Path, "/projects/refused/") {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"detail":"no node can run sandboxed projects: prepare a node first"}`)
				return
			}
			_, _ = io.WriteString(w, `{"on":true,"required":false}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.Close)
	s := &srv{c: client.New(h.URL, "t"), orgID: "org-1"}

	call := func(ref string) *mcp.CallToolResult {
		var req mcp.CallToolRequest
		req.Params.Arguments = map[string]any{"project_id": ref, "on": true}
		res, err := s.handleSetSandbox(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call("shop"); res.IsError {
		t.Fatalf("by slug: %v", res.Content)
	}
	if res := call("level-1"); res.IsError {
		t.Fatalf("a level by ID: %v", res.Content)
	}
	want := []string{
		`/api/v1/orgs/org-1/projects/p1/sandbox {"on":true}`,
		`/api/v1/orgs/org-1/projects/level-1/sandbox {"on":true}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	res := call("refused")
	text, _ := json.Marshal(res.Content)
	if !res.IsError || !strings.Contains(string(text), "prepare a node first") {
		t.Errorf("a refusal should come back as the tool's error with the API's reason, got %s", text)
	}
}
