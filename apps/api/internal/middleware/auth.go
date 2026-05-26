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

// Auth is a soft middleware — it sets the user ID in context if a valid Bearer
// token is present, but does not block requests without one.
// Handlers that require authentication must call RequireUser.
func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Authorization")
			if !strings.HasPrefix(raw, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			tokenStr := strings.TrimPrefix(raw, "Bearer ")
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

			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext returns the authenticated user ID from the request context.
func UserFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

// publicPaths are routes that do not require a JWT. Checked by RequireAuth.
// Terminal paths handle their own JWT validation from the ?token= query param.
var publicPaths = []string{
	"POST /api/v1/auth/login",
	"POST /api/v1/auth/register",
	// MFA second-factor steps — no Bearer token exists yet at this point.
	"POST /api/v1/auth/totp",
	"POST /api/v1/auth/recovery",
	// Node self-registration uses mreg- tokens, not JWTs.
	"/self-register",
	"/self-deregister",
	// WebSocket terminals validate JWT from ?token= internally.
	"/terminal",
	// Inbound webhooks — validated by HMAC signature or per-service deploy token.
	"POST /api/v1/webhooks/",
	// OpenAPI schema served by Huma.
	"GET /api/",
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

func isPublic(r *http.Request) bool {
	methodPath := r.Method + " " + r.URL.Path
	for _, p := range publicPaths {
		if strings.HasPrefix(p, r.Method+" ") {
			// Method-prefixed rule — exact match on method+path prefix.
			if strings.HasPrefix(methodPath, p) {
				return true
			}
		} else {
			// Path-only rule — match anywhere in the path.
			if strings.Contains(r.URL.Path, p) {
				return true
			}
		}
	}
	return false
}
