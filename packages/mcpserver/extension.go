package mcpserver

import (
	mcpsdk "github.com/mark3labs/mcp-go/server"

	"github.com/meshploy/packages/client"
)

// ExtensionContext carries everything a registered tool needs to reach the API.
// It is a struct rather than bare parameters so fields can be added later
// without breaking extensions that live in another repository.
type ExtensionContext struct {
	// Client talks to the Meshploy API. On the remote /mcp transport it is
	// already bound to the calling agent's bearer token, so an extension tool
	// inherits that agent's permissions with no extra work.
	Client *client.Client
	// OrgID is the organization the session is scoped to.
	OrgID string
}

// Extension point: additional MCP tools.
//
// The CE binary never imports the EE module, so toolHooks stays empty in CE
// builds — the same open-core pattern packages/db uses for RegisterMigration
// and packages/server/handler uses for RegisterRoutes. An EE package registers
// from its init():
//
//	func init() { mcpserver.RegisterTools(tools) }
//	func tools(ms *mcpsdk.MCPServer, ec mcpserver.ExtensionContext) {
//	    ms.AddTool(mcp.NewTool("ee_audit_search", ...), handler)
//	}
//
// Without this hook an EE feature is reachable over REST but invisible to the
// agent surface, so an agent could not use the capability the licence grants.
//
// Tools registered here are exposed on both transports: the local stdio server
// (meshploy mcp) and the gateway-served /mcp endpoint. The remote endpoint
// strips tools by name via its denylist, so an operator-grade EE tool must be
// added there too — being an extension does not exempt it.
var toolHooks []func(*mcpsdk.MCPServer, ExtensionContext)

// RegisterTools adds a callback that mounts additional tools. Callbacks run in
// registration order, after every CE tool is registered, so an extension can
// rely on the base surface already existing (and can override a CE tool by
// re-adding the same name).
func RegisterTools(fn func(*mcpsdk.MCPServer, ExtensionContext)) {
	toolHooks = append(toolHooks, fn)
}
