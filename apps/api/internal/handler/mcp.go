package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/apps/api/internal/middleware"
	"github.com/meshploy/apps/api/internal/service"
	cliclient "github.com/meshploy/apps/cli/client"
	"github.com/meshploy/apps/cli/mcpserver"
)

// remoteExcludedTools are stripped from the MCP surface exposed over the public
// /mcp endpoint. They are operator / privilege-escalation / PII tools that must
// never be reachable by a remote agent token regardless of that agent's grants:
//   - node registration token = mesh + k3s enrolment credential
//   - system backups = instance-wide operations
//   - member/permission/invitation enumeration = discloses the org's humans
//   - db_query / db_schema = live exec-into-pod surface (RCE-adjacent)
//
// The local stdio server (meshploy mcp) keeps the full surface — that runs under
// a developer's own login on their own machine, a different trust model.
var remoteExcludedTools = []string{
	"get_node_registration_token",
	"get_system_backup",
	"list_system_backup_objects",
	"list_resource_permissions",
	"list_member_permissions",
	"list_org_members",
	"list_invitations",
	"db_query",
	"db_schema",
}

// MCPHandler serves the Model Context Protocol over Streamable HTTP at /mcp,
// authenticated by a Phase 1 agent token. Each request is handled statelessly
// under the calling agent's principal: tool calls are made against the API on
// localhost carrying the agent's own bearer token, so every operation is
// permission-scoped to exactly what the agent has been granted (the localhost
// shortcut — see the agent-first plan, Phase 2).
func (h *Handler) MCPHandler(w http.ResponseWriter, r *http.Request) {
	agentID, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "valid agent token required")
		return
	}

	// Remote MCP is for agent principals only. A human should use the local
	// stdio server (meshploy mcp). AgentOrg also yields the agent's single org.
	orgID, err := h.svc.Agents.AgentOrg(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, service.ErrAgentNotFound) {
			writeProblem(w, http.StatusForbidden, "remote MCP requires an agent token (magt-)")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "resolve agent org")
		return
	}

	rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

	// Self-directed client: the API calls itself on localhost carrying the
	// agent's token, so the existing tool code runs unchanged and every call
	// re-enters the auth + permission path as the agent.
	c := cliclient.New(fmt.Sprintf("http://127.0.0.1:%d", h.cfg.APIPort), rawToken)

	ms := mcpserver.New(c, orgID.String())
	ms.DeleteTools(remoteExcludedTools...)

	sh := mcpsdk.NewStreamableHTTPServer(ms,
		mcpsdk.WithStateLess(true),
		mcpsdk.WithEndpointPath("/mcp"),
	)
	sh.ServeHTTP(w, r)
}

// writeProblem writes an RFC 7807 problem+json response.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"title":%q,"status":%d,"detail":%q}`, http.StatusText(status), status, detail)
}
