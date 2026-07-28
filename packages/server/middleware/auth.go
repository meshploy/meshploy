package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type contextKey string

const userIDKey contextKey = "userID"

// AgentTokenPrefix identifies a Meshploy agent token in the Authorization header.
const AgentTokenPrefix = "magt-"

// AgentResolver resolves a plaintext agent token to the agent's principal id.
// The bool is false for any unknown/revoked/expired token. Supplied by the
// service layer (AgentService.ResolveToken) so middleware stays db-agnostic.
type AgentResolver func(ctx context.Context, rawToken string) (uuid.UUID, bool)

// Auth is a soft middleware — it sets the user ID in context if a valid Bearer
// credential is present, but does not block requests without one. Handlers that
// require authentication must call RequireUser.
//
// Two credential kinds are accepted, both via `Authorization: Bearer <cred>`:
//   - a JWT (human users), verified with secret;
//   - a magt- agent token, resolved to the same principal shape via resolveAgent.
//
// A resolved agent id is placed in ctx under the identical key a JWT uses, so
// every downstream permission check runs unchanged. resolveAgent may be nil
// (agent auth disabled). agentFailLimiter, when non-nil, throttles repeated
// invalid agent-token attempts per client IP (defence-in-depth; the token space
// is 256-bit so brute force is already infeasible).
func Auth(secret string, resolveAgent AgentResolver, agentFailLimiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Authorization")
			if !strings.HasPrefix(raw, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			tokenStr := strings.TrimPrefix(raw, "Bearer ")

			// Agent token path — resolve to a principal id and set the same ctx key.
			if strings.HasPrefix(tokenStr, AgentTokenPrefix) {
				if resolveAgent != nil {
					if agentID, ok := resolveAgent(r.Context(), tokenStr); ok {
						ctx := ContextWithUser(r.Context(), agentID)
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

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				next.ServeHTTP(w, r)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			rawID, _ := claims["uid"].(string)
			userID, err := uuid.Parse(rawID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := ContextWithUser(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ContextWithUser returns a copy of ctx carrying userID as the authenticated
// principal. This is the only place the principal is written — Auth() uses it
// for both the JWT and agent-token paths, and tests use it to construct an
// authenticated context without going through HTTP.
func ContextWithUser(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
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
}

// publicRules are the routes that do not require an authenticated principal.
//
// Every entry must be justified by the route being unable to carry an
// Authorization header — a bootstrap step, a third-party redirect, or a
// machine credential of a different kind. Anything a logged-in browser calls
// normally does NOT belong here.
//
// Rules are anchored and method-scoped. A previous version carried
// `"GET /api/"` as a prefix rule (annotated "OpenAPI schema served by Huma"),
// which exempted *every GET under /api/* from authentication — and it did not
// even serve its stated purpose, since Huma mounts its spec at /openapi, /docs
// and /schemas. Only per-handler checks were holding the line; a handler that
// omitted one served unauthenticated. That was verified against a running
// binary, not theorised.
var publicRules = []publicRule{
	{Method: "GET", Path: "/health", Match: matchExact},
	{Method: "GET", Path: "/api/v1/auth/status", Match: matchExact},
	{Method: "POST", Path: "/api/v1/auth/login", Match: matchExact},
	{Method: "POST", Path: "/api/v1/auth/register", Match: matchExact},

	// MFA second-factor steps — no Bearer token exists yet at this point.
	{Method: "POST", Path: "/api/v1/auth/totp", Match: matchExact},
	{Method: "POST", Path: "/api/v1/auth/recovery", Match: matchExact},

	// Node self-registration presents an mreg-/mprov- token, not a JWT.
	{Method: "POST", Path: "/api/v1/nodes/self-register", Match: matchExact},
	{Method: "DELETE", Path: "/api/v1/nodes/self-deregister", Match: matchExact},

	// WebSocket terminals validate a JWT from the ?token= query parameter
	// internally, because the browser WebSocket API cannot set headers. The
	// paths carry variable segments, so they are matched by suffix.
	{Method: "GET", Path: "/terminal", Match: matchSuffix},

	// Invitation accept flow — the invitee has no account yet. The invite token
	// in the path is the credential.
	{Method: "GET", Path: "/api/v1/invitations/", Match: matchPrefix},
	{Method: "POST", Path: "/api/v1/invitations/", Match: matchPrefix},

	// Inbound webhooks — validated by HMAC signature or per-service deploy token.
	{Method: "POST", Path: "/api/v1/webhooks/", Match: matchPrefix},

	// Git provider OAuth/App redirects. These arrive from the provider, so no
	// Authorization header can be attached; CSRF is covered by the `state`
	// parameter the handlers validate.
	{Method: "GET", Path: "/api/v1/github/app-callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/github/callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/gitlab/callback", Match: matchExact},
	{Method: "GET", Path: "/api/v1/gitea/callback", Match: matchExact},
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
		switch rule.Match {
		case matchExact:
			if path == rule.Path {
				return true
			}
		case matchPrefix:
			if strings.HasPrefix(path, rule.Path) {
				return true
			}
		case matchSuffix:
			if strings.HasSuffix(path, rule.Path) {
				return true
			}
		}
	}
	return false
}
