package handler

import (
	"testing"

	cliclient "github.com/meshploy/apps/cli/client"
	"github.com/meshploy/apps/cli/mcpserver"
)

// TestRemoteExcludedToolsAreRealAndRemoved verifies that every tool named in
// remoteExcludedTools actually exists on the full MCP surface (so a typo can't
// silently leave a dangerous tool exposed) and that DeleteTools removes them —
// while leaving the core deploy tools intact. No running API/cluster required:
// mcpserver.New only registers tool closures; listing never calls the client.
func TestRemoteExcludedToolsAreRealAndRemoved(t *testing.T) {
	c := cliclient.New("http://127.0.0.1:1", "unused")
	ms := mcpserver.New(c, "00000000-0000-0000-0000-000000000000")

	// Every excluded name must be a real, registered tool before deletion.
	for _, name := range remoteExcludedTools {
		if ms.GetTool(name) == nil {
			t.Errorf("remoteExcludedTools lists %q but no such tool is registered — typo or renamed tool", name)
		}
	}

	ms.DeleteTools(remoteExcludedTools...)

	// After deletion none of them are reachable on the remote surface.
	for _, name := range remoteExcludedTools {
		if ms.GetTool(name) != nil {
			t.Errorf("tool %q survived DeleteTools and is still exposed remotely", name)
		}
	}

	// Core deploy tools must remain — the remote surface is narrowed, not gutted.
	for _, keep := range []string{"list_resources", "create_service", "deploy_service", "create_stack", "apply_stack"} {
		if ms.GetTool(keep) == nil {
			t.Errorf("expected core tool %q to remain on the remote surface", keep)
		}
	}
}
