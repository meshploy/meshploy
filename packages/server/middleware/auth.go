package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type contextKey string

const (
	userIDKey     contextKey = "userID"
	credentialKey contextKey = "credential"
)

// AgentTokenPrefix identifies a Meshploy agent token in the Authorization header.
const AgentTokenPrefix = "magt-"

// CLITokenPrefix identifies the token a CLI holds after a person approved its
// login in a browser.
const CLITokenPrefix = "mcli-"

// OAuthTokenPrefix identifies an access token issued to an MCP client that
// connected through OAuth.
const OAuthTokenPrefix = "moat-"

// MCPHopHeader carries MCPHop on the requests the /mcp handler makes to the API
// on a connection's behalf. An OAuth token is accepted only with it, so a
// token in a client's hands opens /mcp, whose tools exclude the operator ones,
// and not the rest of the API.
const MCPHopHeader = "X-Meshploy-Mcp-Hop"

// MCPHop is this process's hop secret: random at start, never sent anywhere
// but this process's own loopback.
var MCPHop = func() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}()

// Credential is the kind of credential a request was authenticated with.
type Credential string

const (
	// CredentialSession is a browser session: a JWT from signing in, two-factor
	// included.
	CredentialSession Credential = "session"
	CredentialAgent   Credential = "agent"
	CredentialCLI     Credential = "cli"
	// CredentialOAuth is an MCP client's connection, acting as the person who
	// approved it or the agent they chose.
	CredentialOAuth Credential = "oauth"
)

// AgentResolver resolves a plaintext token to the principal it acts as. The
// bool is false for any unknown/revoked/expired token. Supplied by the service
// layer (AgentService.ResolveToken, CLILoginService.ResolveToken) so
// middleware stays db-agnostic.
type AgentResolver func(ctx context.Context, rawToken string) (uuid.UUID, bool)

// Auth is a soft middleware - it sets the user ID in context if a valid Bearer
// credential is present, but does not block requests without one. Handlers that
// require authentication must call RequireUser.
//
// Three credential kinds are accepted, all via `Authorization: Bearer <cred>`:
//   - a JWT (human users), verified with secret;
//   - a magt- agent token, resolved to the same principal shape via resolveAgent;
//   - an mcli- CLI token, resolved via resolveCLI to the person who approved it;
//   - an moat- OAuth token, resolved via resolveOAuth, only on the /mcp
//     handler's own requests (MCPHopHeader).
//
// A resolved id is placed in ctx under the identical key a JWT uses, so every
// downstream permission check runs unchanged; the credential's kind is kept
// beside it (CredentialFromContext) for the few routes that need a browser
// session. A nil resolver disables that kind. agentFailLimiter, when non-nil,
// throttles repeated invalid token attempts per client IP (defence-in-depth;
// the token space is 256-bit so brute force is already infeasible).
func Auth(secret string, resolveAgent, resolveCLI, resolveOAuth AgentResolver, agentFailLimiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Authorization")
			if !strings.HasPrefix(raw, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			tokenStr := strings.TrimPrefix(raw, "Bearer ")

			// Token paths - resolve to a principal id and set the same ctx key.
			resolve, kind := AgentResolver(nil), Credential("")
			switch {
			case strings.HasPrefix(tokenStr, AgentTokenPrefix):
				resolve, kind = resolveAgent, CredentialAgent
			case strings.HasPrefix(tokenStr, CLITokenPrefix):
				resolve, kind = resolveCLI, CredentialCLI
			case strings.HasPrefix(tokenStr, OAuthTokenPrefix):
				if subtle.ConstantTimeCompare([]byte(r.Header.Get(MCPHopHeader)), []byte(MCPHop)) != 1 {
					// Not the /mcp handler's own hop: the request goes on with
					// no principal, so the token opens nothing here. /mcp
					// itself resolves it.
					next.ServeHTTP(w, r)
					return
				}
				resolve, kind = resolveOAuth, CredentialOAuth
			}
			if kind != "" {
				if resolve != nil {
					if id, ok := resolve(r.Context(), tokenStr); ok {
						ctx := context.WithValue(ContextWithUser(r.Context(), id), credentialKey, kind)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
				// Invalid agent token: throttle repeated failures, then fall
				// through unauthenticated (RequireAuth will return 401).
				if agentFailLimiter != nil && !agentFailLimiter.Allow(realIP(r)) {
					w.Header().Set("Content-Type", "application/problem+json")
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"title":"Too Many Requests","status":429,"detail":"too many invalid token attempts"}`))
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Only the method sessions are signed with, and never a token
			// without an expiry: one that cannot lapse would be good forever.
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
			if err != nil || !token.Valid {
				next.ServeHTTP(w, r)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			// The token a correct password earns before the second factor is
			// signed with the same secret and names the same person, but it is
			// only for the code step: as a session it would skip two-factor
			// sign-in entirely.
			if pending, _ := claims["mfa_pending"].(bool); pending {
				next.ServeHTTP(w, r)
				return
			}

			rawID, _ := claims["uid"].(string)
			userID, err := uuid.Parse(rawID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(ContextWithUser(r.Context(), userID), credentialKey, CredentialSession)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ContextWithUser returns a copy of ctx carrying userID as the authenticated
// principal. This is the only place the principal is written - Auth() uses it
// for both the JWT and agent-token paths, and tests use it to construct an
// authenticated context without going through HTTP.
func ContextWithUser(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// CredentialFromContext is the kind of credential the request was
// authenticated with; empty when it was not, or when the principal was put in
// context some other way (tests, internal calls).
func CredentialFromContext(ctx context.Context) Credential {
	c, _ := ctx.Value(credentialKey).(Credential)
	return c
}

// ContextWithCredential returns a copy of ctx saying which kind of credential
// authenticated it, for tests of routes that need a particular kind.
func ContextWithCredential(ctx context.Context, c Credential) context.Context {
	return context.WithValue(ctx, credentialKey, c)
}

// UserFromContext returns the authenticated user ID from the request context.
func UserFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

// matchKind says how a rule's path is compared. Every rule is anchored: there
// is no substring matching, because an unanchored rule silently exempts any
// route whose path happens to contain the pattern.
type matchKind int

const (
	matchExact  matchKind = iota // the whole path equals Path
	matchPrefix                  // the path starts with Path (which must end in "/")
	matchSuffix                  // the path ends with Path (for routes with variable segments)
)

// publicRule exempts one route from RequireAuth.
type publicRule struct {
	Method string // required — a rule that ignores method is almost always too broad
	Path   string
	Match  matchKind
	// Suffix, when set, is an additional constraint: the path must also end
	// with it. It anchors a rule at both ends, so a route with a variable
	// middle segment can be exempted without exempting its siblings.
	Suffix string
}

// RegisterPublicPath adds an exact route an extension serves without a
// session, because its request carries a credential of another kind - a
// one-time token, a signed link - as publicRules' entries are justified. Call
// it from init(), before the server starts. The CE binary registers none.
func RegisterPublicPath(method, path string) {
	publicRules = append(publicRules, publicRule{Method: method, Path: path, Match: matchExact})
}

// publicRules are the routes that do not require an authenticated principal.
//
// Every entry must be justified by the route being unable to carry an
// Authorization header - a bootstrap step, a third-party redirect, or a
// machine credential of a different kind. Anything a logged-in browser calls
// normally does NOT belong here.
//
// Rules are anchored and method-scoped. Never add a prefix rule under /api/:
// it exempts every route beneath it, leaving each handler's own check as the
// only line of defence.
var publicRules = []publicRule{
	{Method: "GET", Path: "/health", Match: matchExact},
	{Method: "GET", Path: "/api/v1/auth/status", Match: matchExact},
	{Method: "POST", Path: "/api/v1/auth/login", Match: matchExact},
	{Method: "POST", Path: "/api/v1/auth/register", Match: matchExact},

	// MFA second-factor steps - no Bearer token exists yet at this point.
	{Method: "POST", Path: "/api/v1/auth/totp", Match: matchExact},
	{Method: "POST", Path: "/api/v1/auth/recovery", Match: matchExact},

	// CLI login through a browser. The CLI has no credential yet: it finds the
	// server, starts a login, and polls with the device code it was given,
	// which is the credential. Approving is an ordinary authenticated call.
	{Method: "GET", Path: "/api/v1/system/login-info", Match: matchExact},
	{Method: "POST", Path: "/api/v1/cli/logins", Match: matchExact},
	{Method: "POST", Path: "/api/v1/cli/logins/token", Match: matchExact},

	// Remote MCP checks its own credential, so that a request without one is
	// answered with the OAuth challenge (WWW-Authenticate) a client follows to
	// connect, not a bare 401.
	{Method: "POST", Path: "/mcp", Match: matchExact},
	{Method: "GET", Path: "/mcp", Match: matchExact},
	{Method: "DELETE", Path: "/mcp", Match: matchExact},

	// OAuth for MCP clients: discovery, registering a client and trading a
	// code or refresh token, all made by a client with no session. A client's
	// own credential, a code with its PKCE verifier or a refresh token, is
	// checked by the handler. Approving is an ordinary authenticated call.
	{Method: "GET", Path: "/.well-known/oauth-protected-resource", Match: matchExact},
	{Method: "GET", Path: "/.well-known/oauth-protected-resource/mcp", Match: matchExact},
	{Method: "GET", Path: "/.well-known/oauth-authorization-server", Match: matchExact},
	{Method: "POST", Path: "/api/v1/oauth/register", Match: matchExact},
	{Method: "POST", Path: "/api/v1/oauth/token", Match: matchExact},
	{Method: "OPTIONS", Path: "/.well-known/oauth-protected-resource", Match: matchExact},
	{Method: "OPTIONS", Path: "/.well-known/oauth-authorization-server", Match: matchExact},
	{Method: "OPTIONS", Path: "/api/v1/oauth/register", Match: matchExact},
	{Method: "OPTIONS", Path: "/api/v1/oauth/token", Match: matchExact},

	// Node self-registration presents an mreg-/mprov- token, not a JWT. The
	// provisioning call comes earlier still, from a machine that is not on the
	// mesh yet and has nothing but the token.
	{Method: "POST", Path: "/api/v1/nodes/provision", Match: matchExact},

	// The installer. Not a secret - the Community edition's repository, its
	// images and this script are public - and the machine fetching it is a
	// blank server with a provisioning token and no session. Its sibling
	// /uninstall.sh stays authenticated: nothing fetches that without one.
	{Method: "GET", Path: "/install.sh", Match: matchExact},
	// Its Windows and macOS counterparts, for mesh-only nodes. Listed one by
	// one rather than by prefix, so nothing else under /join/ is ever public.
	{Method: "GET", Path: "/join/macos.sh", Match: matchExact},
	{Method: "GET", Path: "/join/windows.ps1", Match: matchExact},
	{Method: "POST", Path: "/api/v1/nodes/self-register", Match: matchExact},
	{Method: "DELETE", Path: "/api/v1/nodes/self-deregister", Match: matchExact},

	// WebSocket terminals redeem a single-use ?ticket= internally, because the
	// browser WebSocket API cannot set headers. The paths carry variable
	// segments, so they are matched by suffix. Minting a ticket is a normal
	// authenticated POST and is deliberately NOT listed here.
	{Method: "GET", Path: "/terminal", Match: matchSuffix},

	// Invitation accept flow - the invitee has no account yet. The invite token
	// in the path is the credential.
	{Method: "GET", Path: "/api/v1/invitations/", Match: matchPrefix},
	{Method: "POST", Path: "/api/v1/invitations/", Match: matchPrefix},

	// Inbound webhooks - validated by HMAC signature or per-service deploy token.
	{Method: "POST", Path: "/api/v1/webhooks/", Match: matchPrefix},

	// Template icons - catalog images rendered as <img src>, which cannot carry
	// an Authorization header. Anchored at both ends so only the icon route is
	// exempt: the catalog list and the template detail (which carries the
	// compose spec) still require authentication.
	{Method: "GET", Path: "/api/v1/templates/", Match: matchPrefix, Suffix: "/icon"},

	// Caddy's TLS "ask" endpoints. Caddy calls these before issuing a
	// certificate and its ask mechanism cannot attach an Authorization header,
	// so they meet the criterion above exactly. It also treats any non-2xx as
	// "deny", so behind authentication no certificate would ever be issued.
	//
	// Deliberately not gated on a source address: the API runs in a container
	// published on :4000 and Caddy proxies api.<DOMAIN> to that same port, so
	// the ask call and ordinary public traffic are indistinguishable by origin.
	// Deliberately not gated on a shared secret either: a key that drifts out of
	// sync on an upgrade would silently stop all certificate issuance again,
	// which is a worse failure than the disclosure. Both are read-only and
	// answer only "is this hostname configured here" - already observable by
	// requesting the hostname and seeing whether it serves.
	{Method: "GET", Path: "/api/v1/internal/domain-check", Match: matchExact},
	{Method: "GET", Path: "/api/v1/internal/ondemand-tls-check", Match: matchExact},

	// Git provider OAuth/App redirects. These arrive from the provider, so no
	// Authorization header can be attached; CSRF is covered by the `state`
	// parameter the handlers validate.
	{Method: "GET", Path: "/api/v1/github/app-callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/github/callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/gitlab/callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/gitea/callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/bitbucket/callback", Match: matchExact},
}

// RequireAuth is a fail-closed middleware that returns 401 for any request
// without an authenticated user in context, except for explicitly public paths.
// It must run after Auth() so the user has already been extracted from the token.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := UserFromContext(r.Context()); !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"valid Bearer token required"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isPublic reports whether a request is exempt from authentication.
//
// Fail closed: an unrecognised route is never public, and every rule must match
// both the method and an anchored portion of the path.
func isPublic(r *http.Request) bool {
	path := r.URL.Path
	for _, rule := range publicRules {
		if rule.Method != r.Method {
			continue
		}
		var matched bool
		switch rule.Match {
		case matchExact:
			matched = path == rule.Path
		case matchPrefix:
			matched = strings.HasPrefix(path, rule.Path)
		case matchSuffix:
			matched = strings.HasSuffix(path, rule.Path)
		}
		if matched && (rule.Suffix == "" || strings.HasSuffix(path, rule.Suffix)) {
			return true
		}
	}
	return false
}
