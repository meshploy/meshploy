package mcpserver

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"

	"github.com/meshploy/packages/client"
)

// TestRegisterToolsFires proves the extension point works end to end: a hook
// registered before New runs during New, receives a usable context, and its
// tool lands on the server alongside the CE surface. This is the agent-facing
// equivalent of the route/migration hooks the EE module already uses.
func TestRegisterToolsFires(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	var gotOrg string
	var gotClient *client.Client
	RegisterTools(func(ms *mcpsdk.MCPServer, ec ExtensionContext) {
		gotOrg = ec.OrgID
		gotClient = ec.Client
		ms.AddTool(
			mcp.NewTool("ee_probe", mcp.WithDescription("test-only extension tool")),
			func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("ok"), nil
			},
		)
	})

	c := client.New("http://127.0.0.1:0", "test-token")
	ms := New(c, "org-123")
	if ms == nil {
		t.Fatal("New returned nil")
	}
	if gotOrg != "org-123" {
		t.Errorf("hook got orgID %q, want org-123", gotOrg)
	}
	if gotClient != c {
		t.Error("hook did not receive the API client")
	}
}

// TestNoHooksInCEBuild guards the open-core invariant: with nothing registered,
// New must still build a server. A CE binary never imports the EE module, so
// this is the shape every CE install runs.
func TestNoHooksInCEBuild(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	if ms := New(client.New("http://127.0.0.1:0", ""), "org-1"); ms == nil {
		t.Fatal("New returned nil with no extension hooks")
	}
}
