package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
	"gorm.io/gorm"
)

// oauthRig is the whole API on a real listener, so /mcp's calls back to the
// API on loopback land on the same router, behind the production chain.
type oauthRig struct {
	t       *testing.T
	srv     *httptest.Server
	svc     *service.Services
	db      *gorm.DB
	org     uuid.UUID
	owner   string // the owner's browser session
	member  string // a member's browser session
	agentID uuid.UUID
	agent   string // the agent's own token

	// The last connect's code exchange, to present again.
	lastCode, lastVerifier, lastClient string
}

func newOAuthRig(t *testing.T) *oauthRig {
	t.Helper()
	database := newAuthzTestDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", Domain: "example.test",
		FrontendURL: "http://localhost:5173", APIBaseURL: "http://localhost:4000"}
	svc := service.New(database, cfg)
	h := New(cfg, svc)
	ctx := context.Background()

	owner, err := svc.Auth.Register(ctx, service.RegisterInput{Username: "owner", Email: "owner@example.test", Password: "password123"})
	if err != nil {
		t.Fatal(err)
	}
	orgs, _ := svc.Orgs.ListForUser(ctx, owner.ID)
	org := orgs[0].ID
	if err := svc.Domains.CreateSeeded(ctx, org, "example.test", db.DNSModeDelegation); err != nil {
		t.Fatal(err)
	}
	mia := db.User{Username: "mia", Email: "mia@example.test", Kind: db.UserHuman}
	if err := database.Create(&mia).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Orgs.AddMember(ctx, org, service.AddMemberInput{Email: mia.Email, Role: db.RoleMember}); err != nil {
		t.Fatal(err)
	}
	agentRow, agentTok, err := svc.Agents.CreateAgent(ctx, org, "bot", db.RoleMember, "ci", nil, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := func(id uuid.UUID) string {
		sid, err := svc.Sessions.Start(ctx, id, service.Client{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"uid": id.String(), "sid": sid.String(), "exp": time.Now().Add(time.Hour).Unix(),
		}).SignedString([]byte(cfg.JWTSecret))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	r := chi.NewRouter()
	r.Use(middleware.Auth(cfg.JWTSecret, svc.Sessions.Resolve, svc.Agents.ResolveToken, svc.CLILogins.ResolveToken, svc.OAuth.ResolveToken, nil))
	r.Use(middleware.RequireAuth)
	r.Use(middleware.OrgMember(func(ctx context.Context, orgID, userID uuid.UUID) error {
		_, err := svc.Orgs.MemberRole(ctx, orgID, userID)
		return err
	}))
	h.Register(humachi.New(r, huma.DefaultConfig("test", "1")))
	h.RegisterRaw(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	cfg.APIPort = srv.Listener.Addr().(*net.TCPAddr).Port

	return &oauthRig{t: t, srv: srv, svc: svc, db: database, org: org, owner: session(owner.ID), member: session(mia.ID),
		agentID: agentRow.ID, agent: agentTok}
}

// do sends a request as the console's host, the way Caddy forwards one.
func (g *oauthRig) do(method, path, bearer, contentType string, body io.Reader, hdr map[string]string) *http.Response {
	g.t.Helper()
	req, _ := http.NewRequest(method, g.srv.URL+path, body)
	req.Host = "console.example.test"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (g *oauthRig) json(method, path, bearer string, in, out any) int {
	g.t.Helper()
	var buf bytes.Buffer
	if in != nil {
		_ = json.NewEncoder(&buf).Encode(in)
	}
	resp := g.do(method, path, bearer, "application/json", &buf, nil)
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			g.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (g *oauthRig) token(form url.Values, out any) int {
	g.t.Helper()
	resp := g.do("POST", "/api/v1/oauth/token", "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), nil)
	_ = json.NewDecoder(resp.Body).Decode(out)
	return resp.StatusCode
}

// mcp sends one JSON-RPC call to /mcp and returns the status and body.
func (g *oauthRig) mcp(bearer, method string, params any) (int, string) {
	g.t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp := g.do("POST", "/mcp", bearer, "application/json", bytes.NewReader(b),
		map[string]string{"Accept": "application/json, text/event-stream"})
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

type tokenPair struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Error   string `json:"error"`
}

const redirect = "http://127.0.0.1:33418/callback"

// connect registers a client and approves it as bearer (agentID empty for
// themselves), returning the client and the code exchange's tokens.
func (g *oauthRig) connect(bearer, agentID string) (string, tokenPair, int) {
	g.t.Helper()
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if code := g.json("POST", "/api/v1/oauth/register", "", map[string]any{
		"client_name": "Test client", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none",
	}, &reg); code != http.StatusCreated {
		g.t.Fatalf("register: %d", code)
	}
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	var decided struct {
		Redirect string `json:"redirect"`
	}
	status := g.json("POST", "/api/v1/oauth/authorize", bearer, map[string]any{
		"client_id": reg.ClientID, "redirect_uri": redirect, "code_challenge": challenge, "code_challenge_method": "S256",
		"state": "xyz", "org_id": g.org.String(), "agent_id": agentID, "approve": true,
	}, &decided)
	if status != http.StatusOK {
		return reg.ClientID, tokenPair{}, status
	}
	back, _ := url.Parse(decided.Redirect)
	if back.Query().Get("state") != "xyz" || !strings.HasPrefix(decided.Redirect, redirect) {
		g.t.Fatalf("redirect: %s", decided.Redirect)
	}
	code := back.Query().Get("code")
	var pair tokenPair
	if s := g.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"code_verifier": {verifier}, "client_id": {reg.ClientID}}, &pair); s != http.StatusOK {
		g.t.Fatalf("exchange: %d %+v", s, pair)
	}
	g.lastCode, g.lastVerifier, g.lastClient = code, verifier, reg.ClientID
	return reg.ClientID, pair, http.StatusOK
}

// A client finds how to connect from /mcp's 401, registers itself, a person
// approves it in the console, and it reaches /mcp with exactly that person's
// access; its token opens nothing else.
func TestAnMCPClientConnectsThroughOAuth(t *testing.T) {
	g := newOAuthRig(t)

	resp := g.do("POST", "/mcp", "", "application/json", strings.NewReader("{}"), nil)
	if resp.StatusCode != http.StatusUnauthorized ||
		resp.Header.Get("WWW-Authenticate") != `Bearer resource_metadata="https://console.example.test/.well-known/oauth-protected-resource"` {
		t.Fatalf("challenge: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	var prm struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	g.json("GET", "/.well-known/oauth-protected-resource", "", nil, &prm)
	if prm.Resource != "https://console.example.test/mcp" || len(prm.Servers) != 1 || prm.Servers[0] != "https://console.example.test" {
		t.Fatalf("resource metadata: %+v", prm)
	}
	var as map[string]any
	g.json("GET", "/.well-known/oauth-authorization-server", "", nil, &as)
	if as["authorization_endpoint"] != "https://console.example.test/oauth/authorize" || as["token_endpoint"] != "https://console.example.test/api/v1/oauth/token" {
		t.Fatalf("server metadata: %+v", as)
	}

	// The approval page needs a person in a browser, not a token.
	var reg struct {
		ClientID string `json:"client_id"`
	}
	g.json("POST", "/api/v1/oauth/register", "", map[string]any{"client_name": "Probe", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none"}, &reg)
	info := "/api/v1/oauth/authorize?client_id=" + reg.ClientID + "&redirect_uri=" + url.QueryEscape(redirect) + "&code_challenge=abc&code_challenge_method=S256"
	if s := g.json("GET", info, g.agent, nil, nil); s != http.StatusForbidden {
		t.Errorf("an agent token opened the approval page: %d", s)
	}
	if s := g.json("GET", strings.Replace(info, url.QueryEscape(redirect), url.QueryEscape("https://evil.example/cb"), 1), g.owner, nil, nil); s != http.StatusBadRequest {
		t.Errorf("an unregistered redirect URI was accepted: %d", s)
	}
	if s := g.json("POST", "/api/v1/oauth/register", "", map[string]any{"redirect_uris": []string{"http://evil.example/cb"}}, nil); s != http.StatusBadRequest {
		t.Errorf("a plain-http redirect to another machine was registered: %d", s)
	}

	_, pair, _ := g.connect(g.owner, "")

	// The token opens /mcp ...
	if s, body := g.mcp(pair.Access, "tools/list", map[string]any{}); s != http.StatusOK || !strings.Contains(body, "list_resources") || strings.Contains(body, `"db_query"`) {
		t.Fatalf("tools/list: %d %.300s", s, body)
	}
	if s, body := g.mcp(pair.Access, "tools/call", map[string]any{"name": "list_resources", "arguments": map[string]any{"type": "projects"}}); s != http.StatusOK || strings.Contains(body, `"isError":true`) {
		t.Fatalf("a tool call as the owner: %d %.500s", s, body)
	}
	// ... and nothing else, even with a guessed hop header.
	if s := g.json("GET", "/api/v1/orgs", pair.Access, nil, nil); s != http.StatusUnauthorized {
		t.Errorf("the token opened the API directly: %d", s)
	}
	resp = g.do("GET", "/api/v1/orgs", pair.Access, "", nil, map[string]string{middleware.MCPHopHeader: "guess"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the token opened the API with a guessed hop: %d", resp.StatusCode)
	}

	// A code presented a second time is refused, and ends the connection it
	// made: one of the two presenters is not the client.
	var again tokenPair
	if s := g.token(url.Values{"grant_type": {"authorization_code"}, "code": {g.lastCode}, "redirect_uri": {redirect},
		"code_verifier": {g.lastVerifier}, "client_id": {g.lastClient}}, &again); s != http.StatusBadRequest || again.Error != "invalid_grant" {
		t.Fatalf("replayed code: %d %+v", s, again)
	}
	if s, _ := g.mcp(pair.Access, "tools/list", map[string]any{}); s != http.StatusUnauthorized {
		t.Errorf("the connection outlived its code being replayed: %d", s)
	}

	// A refresh token is bound to the client it was issued to.
	var next tokenPair
	if s := g.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}, "client_id": {"wrong"}}, &next); s != http.StatusUnauthorized {
		t.Errorf("a refresh from another client: %d", s)
	}
}

func (g *oauthRig) mustConnections(bearer, query string) []service.OAuthConnection {
	g.t.Helper()
	var out []service.OAuthConnection
	if s := g.json("GET", "/api/v1/orgs/"+g.org.String()+"/oauth/connections"+query, bearer, nil, &out); s != http.StatusOK {
		g.t.Fatalf("connections: %d", s)
	}
	return out
}

// Refresh tokens rotate, a copied one ends the connection, and disconnecting
// stops the client at once.
func TestAConnectionRefreshesAndIsRevoked(t *testing.T) {
	g := newOAuthRig(t)
	clientID, pair, _ := g.connect(g.owner, "")

	var next tokenPair
	if s := g.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}, "client_id": {clientID}}, &next); s != http.StatusOK {
		t.Fatalf("refresh: %d %+v", s, next)
	}
	if s, _ := g.mcp(next.Access, "tools/list", map[string]any{}); s != http.StatusOK {
		t.Fatalf("the refreshed token: %d", s)
	}
	var replay tokenPair
	if s := g.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}, "client_id": {clientID}}, &replay); s != http.StatusBadRequest {
		t.Fatalf("a spent refresh token: %d", s)
	}
	if s, _ := g.mcp(next.Access, "tools/list", map[string]any{}); s != http.StatusUnauthorized {
		t.Errorf("the connection outlived a copied refresh token: %d", s)
	}

	_, pair, _ = g.connect(g.owner, "")
	conns := g.mustConnections(g.owner, "")
	if len(conns) < 2 || conns[0].RevokedAt != nil || conns[0].AsAgent != "" {
		t.Fatalf("connections: %+v", conns)
	}
	// Another member cannot end it; its owner can.
	if s := g.json("DELETE", "/api/v1/orgs/"+g.org.String()+"/oauth/connections/"+conns[0].ID.String(), g.member, nil, nil); s != http.StatusNotFound {
		t.Errorf("a member ended someone else's connection: %d", s)
	}
	if s := g.json("DELETE", "/api/v1/orgs/"+g.org.String()+"/oauth/connections/"+conns[0].ID.String(), g.owner, nil, nil); s != http.StatusNoContent {
		t.Fatalf("revoke: %d", s)
	}
	if s, _ := g.mcp(pair.Access, "tools/list", map[string]any{}); s != http.StatusUnauthorized {
		t.Errorf("a revoked connection still opened /mcp: %d", s)
	}
}

// A member connects as themselves, never as an agent; an owner may connect as
// one, and the client then acts as the agent.
func TestWhoAConnectionActsAs(t *testing.T) {
	g := newOAuthRig(t)
	if _, _, s := g.connect(g.member, g.agentID.String()); s != http.StatusForbidden {
		t.Errorf("a member connected as an agent: %d", s)
	}
	_, pair, _ := g.connect(g.member, "")
	if s, body := g.mcp(pair.Access, "tools/call", map[string]any{"name": "list_resources", "arguments": map[string]any{"type": "projects"}}); s != http.StatusOK || strings.Contains(body, `"isError":true`) {
		t.Fatalf("a member's tool call: %d %.300s", s, body)
	}
	if s := g.json("GET", "/api/v1/orgs/"+g.org.String()+"/oauth/connections?all=true", g.member, nil, nil); s != http.StatusForbidden {
		t.Errorf("a member listed everyone's connections: %d", s)
	}
	if s := g.json("GET", "/api/v1/orgs/"+g.org.String()+"/oauth/connections?agent_id="+g.agentID.String(), g.member, nil, nil); s != http.StatusForbidden {
		t.Errorf("a member listed an agent's connections: %d", s)
	}

	_, pair, s := g.connect(g.owner, g.agentID.String())
	if s != http.StatusOK {
		t.Fatalf("an owner connecting as the agent: %d", s)
	}
	conns := g.mustConnections(g.owner, "?agent_id="+g.agentID.String())
	if len(conns) != 1 || conns[0].AsAgent != "bot" {
		t.Fatalf("the agent's connections: %+v", conns)
	}
	if code, _ := g.mcp(pair.Access, "tools/list", map[string]any{}); code != http.StatusOK {
		t.Fatalf("as the agent: %d", code)
	}
	if all := g.mustConnections(g.owner, "?all=true"); len(all) != 2 {
		t.Errorf("every connection in the organisation: %+v", all)
	}
	var counts map[string]int
	g.json("GET", "/api/v1/orgs/"+g.org.String()+"/session-counts", g.owner, nil, &counts)
	if len(counts) != 2 {
		t.Errorf("sessions by member: %+v", counts)
	}
	if s := g.json("GET", "/api/v1/orgs/"+g.org.String()+"/cli-sessions", g.member, nil, nil); s != http.StatusForbidden {
		t.Errorf("a member listed the organisation's CLIs: %d", s)
	}
	// A member's signed-in CLI is listed, and counted with their assistants
	// and their console sign-in (the rig's own).
	var mia db.User
	g.db.First(&mia, "username = ?", "mia")
	cli := db.CLIToken{UserID: mia.ID, Host: "mia-laptop", TokenHash: "h", TokenPrefix: "mcli-h"}
	cli.ID = uuid.New()
	if err := g.db.Create(&cli).Error; err != nil {
		t.Fatal(err)
	}
	var clis []service.OrgCLISession
	if s := g.json("GET", "/api/v1/orgs/"+g.org.String()+"/cli-sessions", g.owner, nil, &clis); s != http.StatusOK || len(clis) != 1 || clis[0].Host != "mia-laptop" || clis[0].UserName != "mia" {
		t.Errorf("the organisation's CLIs: %d %+v", s, clis)
	}
	g.json("GET", "/api/v1/orgs/"+g.org.String()+"/session-counts", g.owner, nil, &counts)
	if counts[mia.ID.String()] != 3 {
		t.Errorf("mia's sessions, an assistant, a CLI and the console: %+v", counts)
	}

	// An admin logs out a member's CLI while the member is here alone; not
	// once they belong to another organisation too, where it acts as them.
	other := db.Organization{Name: "Other", Slug: "other"}
	g.db.Create(&other)
	g.db.Create(&db.OrganizationMember{OrganizationID: other.ID, UserID: mia.ID, Role: db.RoleMember})
	g.json("GET", "/api/v1/orgs/"+g.org.String()+"/cli-sessions", g.owner, nil, &clis)
	if len(clis) != 1 || !clis[0].Elsewhere {
		t.Errorf("elsewhere not reported: %+v", clis)
	}
	if s := g.json("DELETE", "/api/v1/orgs/"+g.org.String()+"/cli-sessions/"+cli.ID.String(), g.owner, nil, nil); s != http.StatusConflict {
		t.Errorf("logging out a CLI that acts in another organisation too: %d", s)
	}
	g.db.Where("organization_id = ?", other.ID).Delete(&db.OrganizationMember{})
	if s := g.json("DELETE", "/api/v1/orgs/"+g.org.String()+"/cli-sessions/"+cli.ID.String(), g.member, nil, nil); s != http.StatusForbidden {
		t.Errorf("a member logged out someone's CLI: %d", s)
	}
	if s := g.json("DELETE", "/api/v1/orgs/"+g.org.String()+"/cli-sessions/"+cli.ID.String(), g.owner, nil, nil); s != http.StatusNoContent {
		t.Errorf("an admin logging out a member's CLI: %d", s)
	}
	g.json("GET", "/api/v1/orgs/"+g.org.String()+"/cli-sessions", g.owner, nil, &clis)
	if len(clis) != 0 {
		t.Errorf("the logged-out CLI is still listed: %+v", clis)
	}
}
