package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/meshploy/packages/client"
)

// detect_repository asks the API about the folder it is given and hands its
// answer on whole: the apps, and the workspace build an app needs.
func TestDetectRepositoryAsksAboutTheFolder(t *testing.T) {
	var sent map[string]any
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/org-1/detect-stack" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"summary": "Node.js", "folder": "apps/web", "workspace_build": true,
			"build_command": `pnpm --filter "web..." run build`,
			"apps":          []map[string]any{{"folder": "apps/web"}, {"folder": "apps/api"}},
		})
	}))
	defer h.Close()
	s := &srv{c: client.New(h.URL, "t"), orgID: "org-1"}

	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"repo": "https://github.com/acme/shop", "branch": "main", "root_dir": "apps/web"}
	res, err := s.handleDetectRepository(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("detect: %v %+v", err, res)
	}
	if sent["repo"] != "https://github.com/acme/shop" || sent["branch"] != "main" || sent["root_dir"] != "apps/web" {
		t.Errorf("sent %v", sent)
	}
	if _, ok := sent["git_integration_id"]; ok {
		t.Errorf("a public repository sent an integration: %v", sent)
	}
	text := res.Content[0].(mcp.TextContent).Text
	for _, want := range []string{`"workspace_build":true`, `"apps":[`, `pnpm --filter`} {
		if !strings.Contains(text, want) {
			t.Errorf("result lacks %s: %s", want, text)
		}
	}
}
