package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/meshploy/packages/proxy/cache"
)

// The proxy's own 404 is marked, and a route's never is: the edge sends a
// bare domain with no route to its door, and must pass an app's 404 on.
func TestOnlyTheProxysOwn404IsMarked(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(app.Close)
	u, _ := url.Parse(app.URL)
	port, _ := strconv.Atoi(u.Port())
	c := cache.New(nil, 0)
	c.Set("app.example.com", []cache.TargetEntry{{TargetIP: u.Hostname(), TargetPort: port, Path: "/"}})
	h := NewHandler(c)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get(UnroutedHeader) != "1" {
		t.Fatalf("no route: code %d, %s %q", rec.Code, UnroutedHeader, rec.Header().Get(UnroutedHeader))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://app.example.com/missing", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get(UnroutedHeader) != "" {
		t.Fatalf("a route's 404: code %d, %s %q", rec.Code, UnroutedHeader, rec.Header().Get(UnroutedHeader))
	}
}
