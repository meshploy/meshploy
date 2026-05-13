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

	reqPath := r.URL.Path
	if reqPath == "" {
		reqPath = "/"
	}

	entry, ok := h.cache.Get(hostname, reqPath)
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, notFoundPage, hostname)
		return
	}

	// Strip the matched path prefix before forwarding when requested.
	if entry.StripPath && entry.Path != "/" {
		stripped := strings.TrimPrefix(reqPath, entry.Path)
		if stripped == "" {
			stripped = "/"
		}
		r.URL.Path = stripped
		if r.URL.RawPath != "" {
			r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, entry.Path)
		}
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

const notFoundPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>No route — Meshploy</title>
  <style>
    *{box-sizing:border-box;margin:0;padding:0}
    body{background:#0a0a0a;color:#a1a1aa;font-family:ui-monospace,monospace;
         display:flex;align-items:center;justify-content:center;min-height:100vh;padding:2rem}
    .card{border:1px solid #27272a;border-radius:12px;padding:2.5rem 3rem;max-width:420px;width:100%%;text-align:center}
    .code{font-size:3rem;font-weight:700;color:#3f3f46;margin-bottom:1rem}
    h1{font-size:1rem;font-weight:600;color:#e4e4e7;margin-bottom:.5rem}
    p{font-size:.8rem;line-height:1.6;margin-bottom:1.5rem}
    .host{font-size:.75rem;background:#18181b;border:1px solid #27272a;border-radius:6px;
          padding:.35rem .75rem;display:inline-block;color:#71717a}
    a{color:#71717a;font-size:.75rem;text-decoration:none;border-bottom:1px solid #27272a}
    a:hover{color:#a1a1aa}
  </style>
</head>
<body>
  <div class="card">
    <div class="code">404</div>
    <h1>No route configured</h1>
    <p>There is no Meshploy service mapped to this hostname.</p>
    <div class="host">%s</div>
  </div>
</body>
</html>`
