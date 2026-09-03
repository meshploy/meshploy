package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/meshploy/packages/client"
)

// The six config-file tools have to actually reach the surface. Registration is
// a single call that is easy to omit when adding a tool group, and the failure
// is silent: the server starts fine and an agent simply cannot see them.
func TestConfigFileToolsAreRegistered(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	ms := New(client.New("http://127.0.0.1:0", "t"), "org-1")

	res := ms.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	listing := string(b)

	for _, name := range []string{
		"list_config_files", "create_config_file", "update_config_file",
		"delete_config_file", "attach_config_file", "detach_config_file",
	} {
		if !strings.Contains(listing, `"`+name+`"`) {
			t.Errorf("tool %q is not on the MCP surface", name)
		}
	}
}

// A config file's body is never returned by the API, and the agent-facing type
// must not grow a field that implies otherwise -- a content field would be
// silently empty, which reads as "the file is empty" rather than "not shown".
func TestMCPConfigFileCarriesNoContent(t *testing.T) {
	b, err := json.Marshal(toMCPConfigFile(client.ConfigFile{
		ID: "f1", Name: "zot config", Path: "/etc/zot/config.json", Size: 412,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), "content") {
		t.Fatalf("agent-facing config file exposes content: %s", b)
	}
	// A file attached to nothing must serialise as [], not null: an agent
	// reading null cannot tell "no services" from "not reported".
	if !strings.Contains(string(b), `"services":[]`) {
		t.Errorf("empty attachment list should be [], got %s", b)
	}
}

// A relative path is rejected before the request is made rather than after, so
// the agent gets a usable message instead of a 400 from the API.
func TestCreateConfigFileRejectsRelativePath(t *testing.T) {
	s := &srv{c: client.New("http://127.0.0.1:0", "t"), orgID: "org-1"}
	req := mcp.CallToolRequest{}
	req.Params.Name = "create_config_file"
	req.Params.Arguments = map[string]any{
		"project_id": "p1", "name": "cfg", "path": "etc/zot/config.json", "content": "x",
	}
	res, err := s.handleCreateConfigFile(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned a transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("want a tool error for a relative path")
	}
}
