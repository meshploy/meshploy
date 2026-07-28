package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter is a per-IP token-bucket rate limiter.
// Stale entries (no activity for 10 min) are pruned automatically.
type IPRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	r        rate.Limit
	b        int
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewIPRateLimiter(r rate.Limit, burst int) *IPRateLimiter {
	rl := &IPRateLimiter{
		visitors: make(map[string]*visitor),
		r:        r,
		b:        burst,
	}
	go rl.cleanup()
	return rl
}

func (rl *IPRateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	v, ok := rl.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.r, rl.b)}
		rl.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

func (rl *IPRateLimiter) cleanup() {
	for range time.Tick(10 * time.Minute) {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// realIP extracts the client IP, respecting X-Real-IP and X-Forwarded-For set by Caddy.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i != -1 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// PathRateLimiter returns a middleware that applies different rate limiters
// to specific (method, path) pairs, passing all other requests through unthrottled.
func PathRateLimiter(rules map[string]*IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Method + " " + r.URL.Path
			if rl, ok := rules[key]; ok {
				if !rl.Allow(realIP(r)) {
					w.Header().Set("Content-Type", "application/problem+json")
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"title":"Too Many Requests","status":429,"detail":"rate limit exceeded, please slow down"}`))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
