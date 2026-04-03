package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/meshploy/apps/proxy/internal/cache"
)

type Handler struct {
	cache *cache.Cache
}

func NewHandler(c *cache.Cache) *Handler {
	return &Handler{cache: c}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hostname := r.Host
	// Strip port if present (e.g. "app.domain.com:443" → "app.domain.com")
	if i := strings.IndexByte(hostname, ':'); i != -1 {
		hostname = hostname[:i]
	}

	entry, ok := h.cache.Get(hostname)
	if !ok {
		http.Error(w, `{"error":"route not found"}`, http.StatusNotFound)
		return
	}

	target, _ := url.Parse(fmt.Sprintf("http://%s:%d", entry.TargetIP, entry.TargetPort))
	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy: %s → %s error: %v", hostname, target.Host, err)
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
	}

	// Preserve the original Host so upstreams that are host-aware still work.
	r.Header.Set("X-Forwarded-Host", r.Host)
	proxy.ServeHTTP(w, r)
}
