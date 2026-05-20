package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemberChecker returns nil if userID is a member of orgID, non-nil otherwise.
type MemberChecker func(ctx context.Context, orgID, userID uuid.UUID) error

// OrgMember returns a middleware that enforces the caller is a member of the
// org identified in the URL path (/api/v1/orgs/{orgId}/...).
//
// Unauthenticated requests and paths outside the org scope pass through
// unchanged — downstream handlers call requireUser() for auth enforcement.
// Membership results are cached per user+org for 30 seconds so the DB is not
// hit on every request. On any lookup error the request is rejected (fail closed).
func OrgMember(check MemberChecker) func(http.Handler) http.Handler {
	cache := &memberCache{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID, ok := orgIDFromPath(r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			userID, ok := UserFromContext(r.Context())
			if !ok {
				// No authenticated user — requireUser() in the handler will reject.
				next.ServeHTTP(w, r)
				return
			}

			cacheKey := userID.String() + ":" + orgID.String()
			if cache.get(cacheKey) {
				next.ServeHTTP(w, r)
				return
			}

			if err := check(r.Context(), orgID, userID); err != nil {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"title":"Forbidden","status":403,"detail":"not a member of this organization"}`))
				return
			}

			cache.set(cacheKey)
			next.ServeHTTP(w, r)
		})
	}
}

// InvalidateMember removes a user+org pair from the membership cache immediately.
// Call this after removing a member so they lose access within the current process
// without waiting for the 30-second TTL to expire.
func (c *memberCache) InvalidateMember(userID, orgID uuid.UUID) {
	c.m.Delete(userID.String() + ":" + orgID.String())
}

// orgIDFromPath extracts the org UUID from /api/v1/orgs/{orgId}/... paths.
func orgIDFromPath(path string) (uuid.UUID, bool) {
	const prefix = "/api/v1/orgs/"
	if !strings.HasPrefix(path, prefix) {
		return uuid.Nil, false
	}
	rest := path[len(prefix):]
	if i := strings.IndexByte(rest, '/'); i != -1 {
		rest = rest[:i]
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// ─── Cache ────────────────────────────────────────────────────────────────────

type memberEntry struct {
	expiresAt time.Time
}

type memberCache struct {
	m sync.Map
}

func (c *memberCache) get(key string) bool {
	v, ok := c.m.Load(key)
	if !ok {
		return false
	}
	e := v.(*memberEntry)
	if time.Now().After(e.expiresAt) {
		c.m.Delete(key)
		return false
	}
	return true
}

func (c *memberCache) set(key string) {
	c.m.Store(key, &memberEntry{expiresAt: time.Now().Add(30 * time.Second)})
}
