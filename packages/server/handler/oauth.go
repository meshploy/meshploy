package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
)

// OAuth for MCP clients. Meshploy is both the MCP server (/mcp) and the
// authorization server it names: a client that gets a 401 from /mcp follows
// WWW-Authenticate to the protected-resource metadata, then the authorization
// server's, registers itself, sends a person to the console's /oauth/authorize
// page to approve, and trades the code for tokens here.

// origins are the addresses a request's metadata names: the host the client
// used (it must find the same resource it asked for), and the console the
// person approves on. A host that is not one of the platform's own names gets
// the primary's.
func (h *Handler) origins(ctx context.Context, host string) (self, console string) {
	host = strings.ToLower(host)
	name := host
	if i := strings.LastIndexByte(name, ':'); i >= 0 && !strings.Contains(name[i:], "]") {
		name = name[:i]
	}
	if name == "localhost" || net.ParseIP(name) != nil {
		// Local development: the API answers on its own port, plain HTTP.
		return "http://" + host, h.cfg.FrontendURL
	}
	label, _, _ := strings.Cut(name, ".")
	sub := "console"
	if service.IsConsoleName(label) {
		sub = label
	}
	console = h.svc.Domains.PlatformURLFor(ctx, name, sub)
	self = console
	if label == "api" {
		self = h.svc.Domains.PlatformURLFor(ctx, name, "api")
	}
	return self, console
}

// allowAnyOrigin lets a browser-based MCP client make the discovery,
// registration and token calls. None of them reads a cookie.
func allowAnyOrigin(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError is RFC 6749's error response.
func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func (h *Handler) registerOAuthRaw(r interface {
	HandleFunc(pattern string, fn http.HandlerFunc)
}) {
	r.HandleFunc("/.well-known/oauth-protected-resource", h.oauthProtectedResource)
	r.HandleFunc("/.well-known/oauth-protected-resource/mcp", h.oauthProtectedResource)
	r.HandleFunc("/.well-known/oauth-authorization-server", h.oauthServerMetadata)
	r.HandleFunc("/api/v1/oauth/register", h.oauthRegister)
	r.HandleFunc("/api/v1/oauth/token", h.oauthToken)
}

// oauthProtectedResource is RFC 9728's metadata for /mcp.
func (h *Handler) oauthProtectedResource(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	self, _ := h.origins(r.Context(), r.Host)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 self + "/mcp",
		"authorization_servers":    []string{self},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Meshploy",
	})
}

// oauthServerMetadata is RFC 8414's authorization server metadata.
func (h *Handler) oauthServerMetadata(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	self, console := h.origins(r.Context(), r.Host)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                self,
		"authorization_endpoint":                console + "/oauth/authorize",
		"token_endpoint":                        self + "/api/v1/oauth/token",
		"registration_endpoint":                 self + "/api/v1/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"mcp"},
	})
}

// oauthRegister is RFC 7591 dynamic client registration.
func (h *Handler) oauthRegister(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body is not a JSON client registration")
		return
	}
	method := in.TokenEndpointAuthMethod
	if method == "" {
		method = "client_secret_basic" // RFC 7591's default
	}
	switch method {
	case "none", "client_secret_post", "client_secret_basic":
	default:
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "token_endpoint_auth_method must be none, client_secret_post or client_secret_basic")
		return
	}
	c, secret, err := h.svc.OAuth.Register(r.Context(), service.OAuthClientInput{ClientName: in.ClientName, RedirectURIs: in.RedirectURIs, AuthMethod: method})
	if errors.Is(err, service.ErrOAuthRedirects) {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		return
	}
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not register the client")
		return
	}
	out := map[string]any{
		"client_id":                  c.ClientID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.ClientName,
		"redirect_uris":              in.RedirectURIs,
		"token_endpoint_auth_method": method,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	writeJSON(w, http.StatusCreated, out)
}

// oauthToken is RFC 6749's token endpoint: a code with its PKCE verifier, or
// a refresh token, for a new pair.
func (h *Handler) oauthToken(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the body is not a form")
		return
	}
	clientID, secret, basic := r.BasicAuth()
	if !basic {
		clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	} else {
		// RFC 6749 2.3.1: both parts are form-encoded inside Basic.
		clientID, _ = url.QueryUnescape(clientID)
		secret, _ = url.QueryUnescape(secret)
	}
	var tokens *service.OAuthTokens
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		tokens, err = h.svc.OAuth.Exchange(r.Context(), clientID, secret, r.PostForm.Get("code"),
			r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
	case "refresh_token":
		tokens, err = h.svc.OAuth.Refresh(r.Context(), clientID, secret, r.PostForm.Get("refresh_token"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		return
	}
	switch {
	case errors.Is(err, service.ErrOAuthSecret):
		w.Header().Set("WWW-Authenticate", `Basic realm="meshploy"`)
		oauthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	case errors.Is(err, service.ErrOAuthGrant):
		oauthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	case err != nil:
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.Access,
		"token_type":    "Bearer",
		"expires_in":    tokens.ExpiresIn,
		"refresh_token": tokens.Refresh,
		"scope":         "mcp",
	})
}

// ── The console's approval page ─────────────────────────────────────────────

type OAuthAuthorizeQuery struct {
	ClientID            string `query:"client_id" required:"true"`
	RedirectURI         string `query:"redirect_uri" required:"true"`
	CodeChallenge       string `query:"code_challenge"`
	CodeChallengeMethod string `query:"code_challenge_method"`
}

type OAuthAuthorizeInfoOutput struct {
	Body struct {
		ClientName string `json:"client_name"`
		// RedirectHost is where the person is sent back to, so they can see
		// which application they are letting in.
		RedirectHost string `json:"redirect_host"`
	}
}

type OAuthDecideInput struct {
	Body struct {
		ClientID            string `json:"client_id"`
		RedirectURI         string `json:"redirect_uri"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		State               string `json:"state,omitempty"`
		OrgID               string `json:"org_id,omitempty"`
		// AgentID connects the client as that agent; empty connects it as the
		// person approving.
		AgentID string `json:"agent_id,omitempty"`
		Approve bool   `json:"approve"`
	}
}

type OAuthDecideOutput struct {
	Body struct {
		// Redirect is where the browser goes next: the client's redirect URI
		// with the code, or with error=access_denied.
		Redirect string `json:"redirect"`
	}
}

func oauthRequestError(err error) error {
	switch {
	case errors.Is(err, service.ErrOAuthClient), errors.Is(err, service.ErrOAuthRedirect), errors.Is(err, service.ErrOAuthPKCE):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, service.ErrAgentNotFound):
		return huma.Error404NotFound("that agent is not in this organisation")
	}
	return err
}

// requireBrowser is the person signed in to the console in a browser: only
// they approve a connection, never a token acting for them.
func requireBrowser(ctx context.Context) (uuid.UUID, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if middleware.CredentialFromContext(ctx) != middleware.CredentialSession {
		return uuid.Nil, huma.Error403Forbidden("a connection is approved by a person signed in to the console in a browser")
	}
	return userID, nil
}

func (h *Handler) OAuthAuthorizeInfo(ctx context.Context, in *OAuthAuthorizeQuery) (*OAuthAuthorizeInfoOutput, error) {
	if _, err := requireBrowser(ctx); err != nil {
		return nil, err
	}
	c, err := h.svc.OAuth.Client(ctx, service.OAuthAuthorize{ClientID: in.ClientID, RedirectURI: in.RedirectURI,
		CodeChallenge: in.CodeChallenge, ChallengeMethod: in.CodeChallengeMethod})
	if err != nil {
		return nil, oauthRequestError(err)
	}
	out := &OAuthAuthorizeInfoOutput{}
	out.Body.ClientName = c.ClientName
	if u, err := url.Parse(in.RedirectURI); err == nil {
		out.Body.RedirectHost = u.Host
	}
	return out, nil
}

func (h *Handler) OAuthDecide(ctx context.Context, in *OAuthDecideInput) (*OAuthDecideOutput, error) {
	userID, err := requireBrowser(ctx)
	if err != nil {
		return nil, err
	}
	req := service.OAuthAuthorize{ClientID: in.Body.ClientID, RedirectURI: in.Body.RedirectURI,
		CodeChallenge: in.Body.CodeChallenge, ChallengeMethod: in.Body.CodeChallengeMethod}
	// The request is checked before anything is sent to its redirect URI,
	// which has to be one the client registered.
	if _, err := h.svc.OAuth.Client(ctx, req); err != nil {
		return nil, oauthRequestError(err)
	}
	back, _ := url.Parse(in.Body.RedirectURI)
	q := back.Query()
	if in.Body.State != "" {
		q.Set("state", in.Body.State)
	}
	out := &OAuthDecideOutput{}
	if !in.Body.Approve {
		q.Set("error", "access_denied")
		back.RawQuery = q.Encode()
		out.Body.Redirect = back.String()
		return out, nil
	}
	orgID, err := parseUUID(in.Body.OrgID)
	if err != nil {
		return nil, err
	}
	role, err := h.svc.Orgs.MemberRole(ctx, orgID, userID)
	if err != nil {
		return nil, huma.Error403Forbidden("not a member of this organization")
	}
	var agentID *uuid.UUID
	if in.Body.AgentID != "" {
		if role != db.RoleOwner && role != db.RoleAdmin {
			return nil, huma.Error403Forbidden(service.ErrOAuthAgent.Error())
		}
		id, err := parseUUID(in.Body.AgentID)
		if err != nil {
			return nil, err
		}
		agentID = &id
	}
	code, err := h.svc.OAuth.Approve(ctx, req, orgID, userID, agentID)
	if err != nil {
		return nil, oauthRequestError(err)
	}
	q.Set("code", code)
	back.RawQuery = q.Encode()
	out.Body.Redirect = back.String()
	return out, nil
}

// ── Connections ─────────────────────────────────────────────────────────────

type OAuthConnectionsInput struct {
	OrgID   string `path:"orgId"`
	UserID  string `query:"user_id" doc:"Connections this person approved"`
	AgentID string `query:"agent_id" doc:"Connections acting as this agent"`
	All     bool   `query:"all" doc:"Every connection in the organisation (owners and admins)"`
}

type OAuthConnectionsOutput struct {
	Body []service.OAuthConnection
}

type OrgSessionsInput struct {
	OrgID  string `path:"orgId"`
	UserID string `query:"user_id" doc:"One member's"`
}

type OrgCLISessionsOutput struct {
	Body []service.OrgCLISession
}

type SessionCountsOutput struct {
	Body map[string]int
}

type OAuthConnectionInput struct {
	OrgID        string `path:"orgId"`
	ConnectionID string `path:"connectionId"`
}

// ListOAuthConnections lists the caller's own connections, or, for an owner or
// admin, anyone's or an agent's.
func (h *Handler) ListOAuthConnections(ctx context.Context, in *OAuthConnectionsInput) (*OAuthConnectionsOutput, error) {
	callerID, orgID, _, err := h.checkOrgMemberAccess(ctx, in.OrgID, "")
	if err != nil {
		return nil, err
	}
	admin := h.enforceAdminRole(ctx, orgID, callerID) == nil
	var by, as *uuid.UUID
	switch {
	case in.AgentID != "":
		id, err := parseUUID(in.AgentID)
		if err != nil {
			return nil, err
		}
		as = &id
	case in.UserID != "":
		id, err := parseUUID(in.UserID)
		if err != nil {
			return nil, err
		}
		by = &id
	case in.All:
	default:
		by = &callerID
	}
	if !admin && (as != nil || by == nil || *by != callerID) {
		return nil, huma.Error403Forbidden("only an owner or admin sees other people's connections")
	}
	rows, err := h.svc.OAuth.Connections(ctx, orgID, by, as)
	if err != nil {
		return nil, err
	}
	return &OAuthConnectionsOutput{Body: rows}, nil
}

// OrgCLISessions lists the CLIs signed in as an organisation's members, for
// its owners and admins, saying which belong to someone who is also in
// another organisation, where only they can log it out.
func (h *Handler) OrgCLISessions(ctx context.Context, in *OrgSessionsInput) (*OrgCLISessionsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
	if err != nil {
		return nil, err
	}
	var user *uuid.UUID
	if in.UserID != "" {
		id, err := parseUUID(in.UserID)
		if err != nil {
			return nil, err
		}
		user = &id
	}
	rows, err := h.svc.CLILogins.OrgSessions(ctx, orgID, user)
	if err != nil {
		return nil, err
	}
	return &OrgCLISessionsOutput{Body: rows}, nil
}

type OrgCLISessionInput struct {
	OrgID     string `path:"orgId"`
	SessionID string `path:"sessionId"`
}

// RevokeOrgCLISession logs out a member's CLI, for an owner or admin, while
// that member belongs to this organisation alone.
func (h *Handler) RevokeOrgCLISession(ctx context.Context, in *OrgCLISessionInput) (*struct{}, error) {
	_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.SessionID)
	if err != nil {
		return nil, err
	}
	switch err := h.svc.CLILogins.RevokeInOrg(ctx, orgID, id); {
	case errors.Is(err, service.ErrTokenNotFound):
		return nil, huma.Error404NotFound("no such CLI session in this organisation")
	case errors.Is(err, service.ErrCLIElsewhere):
		return nil, huma.Error409Conflict(err.Error())
	case err != nil:
		return nil, err
	}
	return nil, nil
}

func (h *Handler) SessionCounts(ctx context.Context, in *OrgSessionsInput) (*SessionCountsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
	if err != nil {
		return nil, err
	}
	counts, err := h.svc.OAuth.SessionCounts(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(counts))
	for id, n := range counts {
		out[id.String()] = n
	}
	return &SessionCountsOutput{Body: out}, nil
}

func (h *Handler) RevokeOAuthConnection(ctx context.Context, in *OAuthConnectionInput) (*struct{}, error) {
	callerID, orgID, connID, err := h.checkOrgMemberAccess(ctx, in.OrgID, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	admin := h.enforceAdminRole(ctx, orgID, callerID) == nil
	if err := h.svc.OAuth.Revoke(ctx, orgID, connID, callerID, admin); err != nil {
		if errors.Is(err, service.ErrOAuthConnection) {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, err
	}
	return nil, nil
}

func (h *Handler) registerOAuthRoutes(api huma.API) {
	sec := []map[string][]string{{"bearer": {}}}
	huma.Register(api, huma.Operation{OperationID: "oauth-authorize-info", Method: http.MethodGet, Path: "/api/v1/oauth/authorize",
		Summary: "What an MCP client asking to connect is, for the approval page", Tags: []string{"OAuth"}, Security: sec}, h.OAuthAuthorizeInfo)
	huma.Register(api, huma.Operation{OperationID: "oauth-authorize", Method: http.MethodPost, Path: "/api/v1/oauth/authorize",
		Summary: "Approve or deny an MCP client, as yourself or as an agent; answers where to send the browser", Tags: []string{"OAuth"},
		Security: sec}, h.OAuthDecide)
	huma.Register(api, huma.Operation{OperationID: "list-oauth-connections", Method: http.MethodGet, Path: "/api/v1/orgs/{orgId}/oauth/connections",
		Summary: "MCP clients connected through OAuth: your own, or anyone's or an agent's for an owner or admin", Tags: []string{"OAuth"},
		Security: sec}, h.ListOAuthConnections)
	huma.Register(api, huma.Operation{OperationID: "org-cli-sessions", Method: http.MethodGet, Path: "/api/v1/orgs/{orgId}/cli-sessions",
		Summary: "CLIs signed in as the organisation's members (owners and admins)", Tags: []string{"OAuth"}, Security: sec}, h.OrgCLISessions)
	huma.Register(api, huma.Operation{OperationID: "revoke-org-cli-session", Method: http.MethodDelete, Path: "/api/v1/orgs/{orgId}/cli-sessions/{sessionId}",
		Summary: "Sign out a member's CLI (owners and admins), while they belong to this organisation alone", Tags: []string{"OAuth"},
		Security: sec, DefaultStatus: http.StatusNoContent}, h.RevokeOrgCLISession)
	huma.Register(api, huma.Operation{OperationID: "session-counts", Method: http.MethodGet, Path: "/api/v1/orgs/{orgId}/session-counts",
		Summary: "Connected sessions per member: assistants connected here, and signed-in CLIs", Tags: []string{"OAuth"}, Security: sec}, h.SessionCounts)
	huma.Register(api, huma.Operation{OperationID: "revoke-oauth-connection", Method: http.MethodDelete, Path: "/api/v1/orgs/{orgId}/oauth/connections/{connectionId}",
		Summary: "Disconnect an MCP client: your own, or any in the organisation for an owner or admin", Tags: []string{"OAuth"},
		Security: sec, DefaultStatus: http.StatusNoContent}, h.RevokeOAuthConnection)
}

// oauthChallenge is the 401 /mcp answers a request without a working
// credential: where to find how to connect.
func (h *Handler) oauthChallenge(w http.ResponseWriter, r *http.Request, detail string) {
	self, _ := h.origins(r.Context(), r.Host)
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+self+`/.well-known/oauth-protected-resource"`)
	writeProblem(w, http.StatusUnauthorized, detail)
}
