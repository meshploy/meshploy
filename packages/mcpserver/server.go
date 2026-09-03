package mcpserver

import (
	"encoding/json"
	"reflect"
	"strings"

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
	s.registerConfigFileTools(ms)

	// Extension tools last, so an EE tool can rely on the CE surface existing.
	// toolHooks is empty in CE builds — nothing imports the EE module there.
	ec := ExtensionContext{Client: c, OrgID: orgID}
	for _, fn := range toolHooks {
		fn(ms, ec)
	}
	return ms
}

// jsonResult serialises data and wraps it in a tool result.
//
// MCP defines structuredContent as a JSON OBJECT. Anything else -- an array, a
// null, a bare string or number -- is rejected outright by a spec-compliant
// client: every list_* tool failed with "expected record, received array" and
// was unusable, on both the remote endpoint and the local stdio server.
//
// The rule is applied to the SERIALISED form rather than to Go kinds, so it
// holds for every shape a tool can return, including a typed nil pointer that
// marshals to null. The text content keeps the value's natural JSON, which is
// what a model actually reads, so nothing becomes harder to follow.
func jsonResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return mcp.NewToolResultError("failed to serialize result: " + err.Error()), nil
	}
	text := string(b)

	// A nil slice marshals to "null"; a caller listing something that has no
	// entries should see an empty list, not a null.
	if text == "null" && isNilSlice(data) {
		text = "[]"
	}

	var structured any = data
	switch {
	case strings.HasPrefix(text, "{"):
		// Already an object — pass it through unchanged.
	case strings.HasPrefix(text, "["):
		var items []json.RawMessage
		_ = json.Unmarshal([]byte(text), &items)
		structured = map[string]any{"items": json.RawMessage(text), "count": len(items)}
	default:
		structured = map[string]any{"result": json.RawMessage(text)}
	}
	return mcp.NewToolResultStructured(structured, text), nil
}

// isNilSlice reports whether data is a nil slice, which marshals to null.
func isNilSlice(data any) bool {
	rv := reflect.ValueOf(data)
	return rv.IsValid() && rv.Kind() == reflect.Slice && rv.IsNil()
}
