package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
)

// cliLoginRig is the CLI login routes behind the same chain production puts
// them behind: the auth middleware with every credential kind, then
// RequireAuth with its public routes.
type cliLoginRig struct {
	t       *testing.T
	r       http.Handler
	svc     *service.Services
	session string // a signed-in browser's JWT
	agent   string // an agent token in the same org
}

func newCLILoginRig(t *testing.T) *cliLoginRig {
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
	orgs, err := svc.Orgs.ListForUser(ctx, owner.ID)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("orgs: %v %v", orgs, err)
	}
	if err := svc.Domains.CreateSeeded(ctx, orgs[0].ID, "example.test", db.DNSModeDelegation); err != nil {
		t.Fatal(err)
	}
	_, agent, err := svc.Agents.CreateAgent(ctx, orgs[0].ID, "bot", db.RoleAdmin, "ci", nil, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := svc.Sessions.Start(ctx, owner.ID, service.Client{})
	if err != nil {
		t.Fatal(err)
	}
	session, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": owner.ID.String(), "sid": sid.String(), "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Auth(cfg.JWTSecret, svc.Sessions.Resolve, svc.Agents.ResolveToken, svc.CLILogins.ResolveToken, svc.OAuth.ResolveToken, nil))
	r.Use(middleware.RequireAuth)
	h.registerCLILoginRoutes(humachi.New(r, huma.DefaultConfig("test", "1")))
	return &cliLoginRig{t: t, r: r, svc: svc, session: session, agent: agent}
}

// call makes a request through api.example.test as bearer (none when empty)
// and decodes a JSON answer into out when given.
func (g *cliLoginRig) call(method, path, bearer string, body any, out any) int {
	g.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "api.example.test"
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	g.r.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			g.t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body.String())
		}
	}
	return rec.Code
}

type startedLogin struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	APIURL          string `json:"api_url"`
	Interval        int    `json:"interval"`
}

type polled struct {
	Status string `json:"status"`
	Token  string `json:"token"`
}

func (g *cliLoginRig) start(door string) startedLogin {
	g.t.Helper()
	var s startedLogin
	if code := g.call("POST", "/api/v1/cli/logins", "", map[string]string{"host": "laptop", "door": door}, &s); code != http.StatusCreated {
		g.t.Fatalf("start: %d", code)
	}
	return s
}

func (g *cliLoginRig) poll(device string) polled {
	g.t.Helper()
	var p polled
	if code := g.call("POST", "/api/v1/cli/logins/token", "", map[string]string{"device_code": device}, &p); code != http.StatusOK {
		g.t.Fatalf("poll: %d", code)
	}
	return p
}

// A CLI finds the server, starts a login with no credential, and collects a
// token once a person approves in the browser. Only a browser session may
// approve; the token then acts as that person until it is logged out.
func TestACLILogsInThroughTheBrowser(t *testing.T) {
	g := newCLILoginRig(t)

	var info struct {
		ConsoleURL string `json:"console_url"`
		APIURL     string `json:"api_url"`
	}
	if code := g.call("GET", "/api/v1/system/login-info", "", nil, &info); code != http.StatusOK ||
		info.ConsoleURL != "https://console.example.test" || info.APIURL != "https://api.example.test" {
		t.Fatalf("login-info: %d %+v", code, info)
	}

	s := g.start("")
	if s.VerificationURL != "https://console.example.test/cli-login?code="+s.UserCode || s.APIURL != "https://api.example.test" || s.Interval != 5 {
		t.Fatalf("start: %+v", s)
	}
	if p := g.poll(s.DeviceCode); p.Status != "pending" || p.Token != "" {
		t.Fatalf("before approval: %+v", p)
	}

	// The approval page reads it signed in, and only signed in.
	var shown struct{ Host string }
	if code := g.call("GET", "/api/v1/cli/logins/"+s.UserCode, g.session, nil, &shown); code != http.StatusOK || shown.Host != "laptop" {
		t.Fatalf("show: %d %+v", code, shown)
	}
	if code := g.call("GET", "/api/v1/cli/logins/"+s.UserCode, "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("show without a session: %d", code)
	}

	// An agent cannot approve: it would hand its access on to a machine.
	if code := g.call("POST", "/api/v1/cli/logins/"+s.UserCode+"/approve", g.agent, nil, nil); code != http.StatusForbidden {
		t.Fatalf("agent approving: %d", code)
	}
	// A person types the code as they like.
	typed := strings.ToLower(strings.ReplaceAll(s.UserCode, "-", ""))
	if code := g.call("POST", "/api/v1/cli/logins/"+typed+"/approve", g.session, nil, nil); code != http.StatusNoContent {
		t.Fatalf("approve: %d", code)
	}
	if code := g.call("POST", "/api/v1/cli/logins/"+s.UserCode+"/deny", g.session, nil, nil); code != http.StatusConflict {
		t.Fatalf("deny after approving: %d", code)
	}

	p := g.poll(s.DeviceCode)
	if p.Status != "approved" || !strings.HasPrefix(p.Token, "mcli-") {
		t.Fatalf("after approval: %+v", p)
	}
	if again := g.poll(s.DeviceCode); again.Status != "collected" || again.Token != "" {
		t.Fatalf("collected twice: %+v", again)
	}
	cli := p.Token

	var sessions []struct {
		ID      string
		Host    string
		Current bool
	}
	if code := g.call("GET", "/api/v1/me/cli-sessions", cli, nil, &sessions); code != http.StatusOK ||
		len(sessions) != 1 || sessions[0].Host != "laptop" || !sessions[0].Current {
		t.Fatalf("sessions as the CLI: %d %+v", code, sessions)
	}
	if code := g.call("GET", "/api/v1/me/cli-sessions", g.session, nil, &sessions); code != http.StatusOK || sessions[0].Current {
		t.Fatalf("sessions in the browser: %d %+v", code, sessions)
	}

	// A CLI cannot approve another machine either: two-factor was asked for in
	// the browser, not here.
	other := g.start("")
	if code := g.call("POST", "/api/v1/cli/logins/"+other.UserCode+"/approve", cli, nil, nil); code != http.StatusForbidden {
		t.Fatalf("CLI approving: %d", code)
	}
	if code := g.call("POST", "/api/v1/cli/logins/"+other.UserCode+"/deny", g.session, nil, nil); code != http.StatusNoContent {
		t.Fatalf("deny: %d", code)
	}
	if p := g.poll(other.DeviceCode); p.Status != "denied" || p.Token != "" {
		t.Fatalf("after denial: %+v", p)
	}

	// Logging out ends it at once.
	if code := g.call("DELETE", "/api/v1/cli/session", g.session, nil, nil); code != http.StatusBadRequest {
		t.Fatalf("logout from a browser session: %d", code)
	}
	if code := g.call("DELETE", "/api/v1/cli/session", cli, nil, nil); code != http.StatusNoContent {
		t.Fatalf("logout: %d", code)
	}
	if code := g.call("GET", "/api/v1/me/cli-sessions", cli, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", code)
	}
}

// A login nobody approves in time is told so, and cannot be approved late. A
// token unused for CLITokenIdle stops working; one revoked from the account
// stops at once.
func TestCLILoginsAndTokensLapse(t *testing.T) {
	g := newCLILoginRig(t)
	database := g.svc.DB

	late := g.start("")
	database.Model(&db.CLILogin{}).Where("user_code = ?", late.UserCode).Update("expires_at", time.Now().Add(-time.Minute))
	if p := g.poll(late.DeviceCode); p.Status != "expired" {
		t.Fatalf("expired login: %+v", p)
	}
	if code := g.call("POST", "/api/v1/cli/logins/"+late.UserCode+"/approve", g.session, nil, nil); code != http.StatusNotFound {
		t.Fatalf("approving late: %d", code)
	}
	if code := g.call("POST", "/api/v1/cli/logins/token", "", map[string]string{"device_code": "nothing"}, nil); code != http.StatusNotFound {
		t.Fatalf("polling an unknown code: %d", code)
	}

	token := func() string {
		s := g.start("")
		if code := g.call("POST", "/api/v1/cli/logins/"+s.UserCode+"/approve", g.session, nil, nil); code != http.StatusNoContent {
			t.Fatalf("approve: %d", code)
		}
		return g.poll(s.DeviceCode).Token
	}

	idle := token()
	if code := g.call("GET", "/api/v1/me/cli-sessions", idle, nil, nil); code != http.StatusOK {
		t.Fatalf("fresh token: %d", code)
	}
	long := time.Now().Add(-service.CLITokenIdle - time.Hour)
	database.Model(&db.CLIToken{}).Where("1 = 1").Updates(map[string]any{"last_used_at": long, "created_at": long})
	if code := g.call("GET", "/api/v1/me/cli-sessions", idle, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("idle token: %d", code)
	}

	revoked := token()
	var sessions []struct{ ID string }
	g.call("GET", "/api/v1/me/cli-sessions", g.session, nil, &sessions)
	if len(sessions) != 1 {
		t.Fatalf("live sessions: %+v", sessions)
	}
	if code := g.call("DELETE", "/api/v1/me/cli-sessions/"+sessions[0].ID, g.session, nil, nil); code != http.StatusNoContent {
		t.Fatalf("revoke: %d", code)
	}
	if code := g.call("GET", "/api/v1/me/cli-sessions", revoked, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", code)
	}
}

// The approval link names a console this server serves: the console itself,
// or a name an extension registered, and nothing else.
func TestACLILoginIsApprovedAtAConsoleThisServerServes(t *testing.T) {
	g := newCLILoginRig(t)
	if code := g.call("POST", "/api/v1/cli/logins", "", map[string]string{"host": "laptop", "door": "elsewhere"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown door: %d", code)
	}
	service.RegisterConsoleName("door")
	if s := g.start("door"); !strings.HasPrefix(s.VerificationURL, "https://door.example.test/cli-login?code=") {
		t.Fatalf("registered door: %+v", s)
	}
}
