package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

// The old edge is handed the visitor Caddy saw, not Caddy's loopback address:
// it trusts this hop's forwarded headers and keeps X-Real-Ip, so an app behind
// it logs the same address it did before the edge moved.
func TestTheOldEdgeSeesTheVisitor(t *testing.T) {
	var got http.Header
	var host string
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, host = r.Header.Clone(), r.Host
	}))
	defer old.Close()
	u, _ := url.Parse(old.URL)

	h := &Handler{}
	req := httptest.NewRequest("GET", "http://app.example.com/x", nil)
	req.RemoteAddr = "127.0.0.1:50000" // Caddy
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("X-Forwarded-Proto", "https")
	h.forwardToFallback(httptest.NewRecorder(), req, "app.example.com", u.Host)

	if host != "app.example.com" {
		t.Errorf("Host = %q, want the visitor's", host)
	}
	for k, want := range map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Real-Ip": "203.0.113.7",
		"X-Forwarded-Proto": "https", "X-Forwarded-Host": "app.example.com"} {
		if v := got.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if strings.Contains(got.Get("X-Forwarded-For"), "127.0.0.1") {
		t.Errorf("Caddy's address leaked into X-Forwarded-For: %q", got.Get("X-Forwarded-For"))
	}
}

// A routed request carries the same: the visitor, not Caddy, as a workload
// moved from Dokploy was told by Traefik.
func TestARoutedWorkloadSeesTheVisitor(t *testing.T) {
	var got http.Header
	var host, path string
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, host, path = r.Header.Clone(), r.Host, r.URL.Path
	}))
	defer app.Close()
	u, _ := url.Parse(app.URL)
	target, _ := url.Parse("http://" + u.Host)

	req := httptest.NewRequest("GET", "http://app.example.com/api/items", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("X-Forwarded-Proto", "https")
	(&httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { forwardedRequest(pr, target) }}).
		ServeHTTP(httptest.NewRecorder(), req)

	if host != "app.example.com" || path != "/api/items" {
		t.Errorf("Host %q path %q", host, path)
	}
	if got.Get("X-Forwarded-For") != "203.0.113.7" || got.Get("X-Real-Ip") != "203.0.113.7" {
		t.Errorf("X-Forwarded-For %q, X-Real-Ip %q", got.Get("X-Forwarded-For"), got.Get("X-Real-Ip"))
	}
}

// Only Caddy, on loopback, is believed about who the visitor is.
func TestAMeshCallerCannotNameAnotherVisitor(t *testing.T) {
	req := httptest.NewRequest("GET", "http://app.example.com/", nil)
	req.RemoteAddr = "100.64.0.9:41000"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	if v := visitor(req); v != "100.64.0.9" {
		t.Errorf("visitor = %q, want the caller itself", v)
	}
}
