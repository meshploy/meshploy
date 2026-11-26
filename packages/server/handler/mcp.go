package handler

import (
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/server"
	cliclient "github.com/meshploy/packages/client"
	"github.com/meshploy/packages/mcpserver"
	"github.com/meshploy/packages/server/middleware"
)

// MCPHandler serves the Model Context Protocol over Streamable HTTP at /mcp,
// to an agent token (magt-) or a client connected through OAuth (moat-), which
// acts as the person who approved it or the agent they chose. Each request is
// handled statelessly: tool calls are made against the API on localhost
// carrying the same credential, so every operation is permission-scoped to
// exactly what that principal has been granted. A request without a working
// credential is answered with the OAuth challenge a client follows to connect.
func (h *Handler) MCPHandler(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	ctx := r.Context()

	var orgID uuid.UUID
	hop := false
	switch {
	case strings.HasPrefix(rawToken, middleware.OAuthTokenPrefix):
		g, ok := h.svc.OAuth.Resolve(ctx, rawToken)
		if !ok {
			h.oauthChallenge(w, r, "the connection's token is unknown, expired or revoked")
			return
		}
		orgID, hop = g.OrganizationID, true
	case middleware.CredentialFromContext(ctx) == middleware.CredentialAgent:
		agentID, _ := middleware.UserFromContext(ctx)
		id, err := h.svc.Agents.AgentOrg(ctx, agentID)
		if err != nil {
			writeProblem(w, http.StatusForbidden, "the agent token does not resolve to one organization")
			return
		}
		orgID = id
	default:
		// No credential, or a person's own (a session or a CLI token): a person
		// connects a client through OAuth, and locally runs meshploy mcp.
		h.oauthChallenge(w, r, "connect through OAuth, or use an agent token (magt-)")
		return
	}

	// Self-directed client: the API calls itself on localhost carrying the
	// same token, so the existing tool code runs unchanged and every call
	// re-enters the auth + permission path as the principal. An OAuth token
	// is accepted there only with this process's hop secret.
	c := cliclient.New(fmt.Sprintf("http://127.0.0.1:%d", h.cfg.APIPort), rawToken)
	if hop {
		c.SetHeader(middleware.MCPHopHeader, middleware.MCPHop)
	}

	ms := mcpserver.New(c, orgID.String())
	ms.DeleteTools(mcpserver.RemoteExcluded...)

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
