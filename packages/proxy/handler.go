package proxy

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/meshploy/packages/proxy/cache"
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
		// A hostname the platform being migrated from still serves goes to
		// its edge, as it arrived: the Host kept, and the X-Forwarded-Proto
		// Caddy set, so that edge serves it rather than redirecting to HTTPS.
		if upstream, found := h.cache.FallbackFor(hostname); found {
			h.forwardToFallback(w, r, hostname, upstream)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, notFoundPage, hostname)
		return
	}

	if runGates(w, r, Target{Host: hostname, RouteID: entry.RouteID, ServiceID: entry.ServiceID,
		ProjectID: entry.ProjectID, OrgID: entry.OrgID}) {
		return
	}

	// Redirect target - respond immediately without proxying.
	if entry.RedirectHostname != "" {
		code := entry.RedirectCode
		if code == 0 {
			code = http.StatusMovedPermanently
		}
		location := "https://" + entry.RedirectHostname + r.URL.RequestURI()
		http.Redirect(w, r, location, code)
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

	scheme := "http"
	if entry.TargetTLS {
		scheme = "https"
	}
	target, _ := url.Parse(fmt.Sprintf("%s://%s:%d", scheme, entry.TargetIP, entry.TargetPort))
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { forwardedRequest(pr, target) }}
	if entry.TargetTLS {
		// The target is named by address, so its certificate has no name to be
		// verified against - it is typically self-signed, on this machine's
		// loopback or across the mesh, both of which are already private. TLS
		// here is for the target's sake, because it speaks nothing else.
		proxy.Transport = tlsBackendTransport
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy: %s → %s error: %v", hostname, target.Host, err)
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, r)
}

// forwardToFallback hands a request to the old platform's edge.
func (h *Handler) forwardToFallback(w http.ResponseWriter, r *http.Request, hostname, upstream string) {
	target := &url.URL{Scheme: "http", Host: upstream}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) { forwardedRequest(pr, target) },
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy: %s → old edge %s error: %v", hostname, upstream, err)
			http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

// forwardedRequest is what a workload, or the old edge, is handed: the
// request as the visitor sent it, its Host kept for host-aware upstreams, and
// who the visitor was, the way Traefik told an app moved from Dokploy -
// X-Forwarded-For and X-Real-Ip both the visitor. Caddy, in front, set
// X-Forwarded-For to the visitor alone; the default director added Caddy's
// loopback address to it and set no X-Real-Ip, so an app that logs or limits
// by address saw one address for everyone. The old edge trusts this hop's
// forwarded headers, so it keeps X-Real-Ip rather than naming the proxy.
func forwardedRequest(pr *httputil.ProxyRequest, target *url.URL) {
	pr.SetURL(target)
	pr.Out.Host = pr.In.Host
	client := visitor(pr.In)
	pr.Out.Header.Set("X-Forwarded-For", client)
	pr.Out.Header.Set("X-Real-Ip", client)
	pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
	proto := pr.In.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	pr.Out.Header.Set("X-Forwarded-Proto", proto)
}

// visitor is who sent the request. Caddy, on this machine's loopback, says who
// in X-Forwarded-For; anything else reaching the proxy - a machine on the
// mesh, through PROXY_BIND - is its own visitor, and what it claims about
// others is not taken.
func visitor(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
	}
	return host
}

// tlsBackendTransport is shared, so connections to TLS targets are pooled the
// way the default transport pools plain ones.
var tlsBackendTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: time.Second,
	TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
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
