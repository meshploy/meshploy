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

const testLinkKey = "link-key-for-tests"

type linkRig struct {
	t   *testing.T
	r   http.Handler
	svc *service.Services
	cfg *config.Config
}

// newLinkRig is the sign-in link routes behind production's chain, on an
// installation whose key is key (empty: links off).
func newLinkRig(t *testing.T, key string) *linkRig {
	t.Helper()
	database := newAuthzTestDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", Domain: "example.test", SignInLinkKey: key,
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
	if _, _, err := svc.Agents.CreateAgent(ctx, orgs[0].ID, "bot", db.RoleAdmin, "ci", nil, owner.ID); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(middleware.Auth(cfg.JWTSecret, svc.Sessions.Resolve, svc.Agents.ResolveToken, svc.CLILogins.ResolveToken, svc.OAuth.ResolveToken, nil))
	r.Use(middleware.RequireAuth)
	h.registerSignInLinkRoutes(humachi.New(r, huma.DefaultConfig("test", "1")))
	return &linkRig{t: t, r: r, svc: svc, cfg: cfg}
}

func (g *linkRig) call(path, key string, body any, out any) int {
	g.t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Host = "api.example.test"
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Sign-In-Link-Key", key)
	}
	rec := httptest.NewRecorder()
	g.r.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			g.t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

type madeLink struct {
	URL string `json:"url"`
}

func (g *linkRig) make(email, next string) (madeLink, int) {
	var l madeLink
	code := g.call("/api/v1/system/sign-in-links", testLinkKey, map[string]string{"email": email, "next": next}, &l)
	return l, code
}

func (g *linkRig) spend(token string) (string, int) {
	var out struct {
		Token string `json:"token"`
	}
	code := g.call("/api/v1/auth/sign-in-link", "", map[string]string{"token": token}, &out)
	return out.Token, code
}

func linkToken(t *testing.T, u string) string {
	t.Helper()
	_, tok, ok := strings.Cut(u, "#")
	if !ok || !strings.HasPrefix(tok, "mlnk-") {
		t.Fatalf("link %q carries no token in its fragment", u)
	}
	return tok
}

func TestSignInLinkOffWithoutKey(t *testing.T) {
	g := newLinkRig(t, "")
	if code := g.call("/api/v1/system/sign-in-links", "anything", map[string]string{"email": "owner@example.test"}, nil); code != http.StatusNotFound {
		t.Fatalf("links off: got %d, want 404", code)
	}
	// An empty key header must not match an empty configured key.
	if code := g.call("/api/v1/system/sign-in-links", "", map[string]string{"email": "owner@example.test"}, nil); code != http.StatusNotFound {
		t.Fatalf("links off, no key: got %d, want 404", code)
	}
}

func TestSignInLinkNeedsTheKey(t *testing.T) {
	g := newLinkRig(t, testLinkKey)
	for _, key := range []string{"", "wrong", testLinkKey + "x"} {
		if code := g.call("/api/v1/system/sign-in-links", key, map[string]string{"email": "owner@example.test"}, nil); code != http.StatusUnauthorized {
			t.Errorf("key %q: got %d, want 401", key, code)
		}
	}
}

func TestSignInLinkSignsInOnce(t *testing.T) {
	g := newLinkRig(t, testLinkKey)
	l, code := g.make("Owner@Example.test", "/projects")
	if code != http.StatusCreated {
		t.Fatalf("make: %d", code)
	}
	if !strings.HasPrefix(l.URL, "https://console.example.test/link?next=%2Fprojects#") {
		t.Fatalf("url %q: want the console's /link with next, token in the fragment", l.URL)
	}
	tok := linkToken(t, l.URL)
	session, code := g.spend(tok)
	if code != http.StatusOK || session == "" {
		t.Fatalf("spend: %d %q", code, session)
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(session, claims, func(*jwt.Token) (any, error) { return []byte(g.cfg.JWTSecret), nil }); err != nil {
		t.Fatalf("session token: %v", err)
	}
	var owner db.User
	g.svc.DB.First(&owner, "email = ?", "owner@example.test")
	if claims["uid"] != owner.ID.String() || claims["sid"] == nil {
		t.Fatalf("session is for %v (sid %v), want the owner %s", claims["uid"], claims["sid"], owner.ID)
	}
	if _, code := g.spend(tok); code != http.StatusUnauthorized {
		t.Fatalf("second spend: got %d, want 401", code)
	}
}

func TestSignInLinkLapses(t *testing.T) {
	g := newLinkRig(t, testLinkKey)
	l, _ := g.make("owner@example.test", "")
	if strings.Contains(l.URL, "?next=") {
		t.Fatalf("url %q: no next was asked for", l.URL)
	}
	g.svc.DB.Model(&db.SignInLink{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Second))
	if _, code := g.spend(linkToken(t, l.URL)); code != http.StatusUnauthorized {
		t.Fatalf("lapsed link: got %d, want 401", code)
	}
}

func TestSignInLinkRefusals(t *testing.T) {
	g := newLinkRig(t, testLinkKey)
	// Nobody, and agents (which have no address), get no link.
	for _, email := range []string{"nobody@example.test", "", " "} {
		if _, code := g.make(email, ""); code != http.StatusNotFound {
			t.Errorf("email %q: got %d, want 404", email, code)
		}
	}
	for _, next := range []string{"https://evil.test/", "//evil.test", "/\\evil.test", "projects", "/a\r\nb"} {
		if _, code := g.make("owner@example.test", next); code != http.StatusUnprocessableEntity {
			t.Errorf("next %q: got %d, want 422", next, code)
		}
	}
	for _, tok := range []string{"", "mlnk-", "mlnk-00", "not-a-link"} {
		if _, code := g.spend(tok); code != http.StatusUnauthorized {
			t.Errorf("token %q: got %d, want 401", tok, code)
		}
	}
}

func TestSignInLinkForSomeoneRemoved(t *testing.T) {
	g := newLinkRig(t, testLinkKey)
	if err := g.svc.DB.Create(&db.User{Username: "leaver", Email: "leaver@example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	l, code := g.make("leaver@example.test", "")
	if code != http.StatusCreated {
		t.Fatalf("make: %d", code)
	}
	g.svc.DB.Where("email = ?", "leaver@example.test").Delete(&db.User{})
	if _, code := g.spend(linkToken(t, l.URL)); code != http.StatusUnauthorized {
		t.Fatalf("link for a removed person: got %d, want 401", code)
	}
}
