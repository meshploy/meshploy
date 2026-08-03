package mcpserver

import (
	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/packages/client"
)

type srv struct {
	c     *client.Client
	orgID string
}

// New creates a configured MCP server backed by the Meshploy API.
func New(c *client.Client, orgID string) *mcpsdk.MCPServer {
	s := &srv{c: c, orgID: orgID}
	ms := mcpsdk.NewMCPServer(
		"meshploy", "0.1.0",
		mcpsdk.WithToolCapabilities(false),
		mcpsdk.WithInstructions(
			"Meshploy IDP manager. Most operations are project-scoped — "+
				"call list_resources(type=projects) to discover project IDs before working with services, jobs, stacks, volumes, or routes.",
		),
	)
	s.registerReadTools(ms)
	s.registerReadToolsExtended(ms)
	s.registerWriteTools(ms)
	s.registerWriteToolsExtended(ms)

	// Extension tools last, so an EE tool can rely on the CE surface existing.
	// toolHooks is empty in CE builds — nothing imports the EE module there.
	ec := ExtensionContext{Client: c, OrgID: orgID}
	for _, fn := range toolHooks {
		fn(ms, ec)
	}
	return ms
}

// jsonResult serialises data and wraps it in a tool result.
func jsonResult(data any) (*mcp.CallToolResult, error) {
	res, err := mcp.NewToolResultJSON(data)
	if err != nil {
		return mcp.NewToolResultError("failed to serialize result: " + err.Error()), nil
	}
	return res, nil
}
