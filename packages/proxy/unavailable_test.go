package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/proxy/cache"
)

// A route whose target does not answer tells a browser whether its service
// was stopped or is not answering, on a page; any other caller gets the JSON
// it always has.
func TestAStoppedServiceSaysSo(t *testing.T) {
	// A port nothing listens on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	stopped, silent := uuid.New(), uuid.New()
	c := cache.New(nil, 0)
	c.Set("stopped.example.com", []cache.TargetEntry{{TargetIP: "127.0.0.1", TargetPort: port, Path: "/", ServiceID: stopped}})
	c.Set("silent.example.com", []cache.TargetEntry{{TargetIP: "127.0.0.1", TargetPort: port, Path: "/", ServiceID: silent}})
	h := NewHandler(c)
	h.stopped = func(_ context.Context, id uuid.UUID) bool { return id == stopped }

	get := func(host, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://"+host+"/", nil)
		r.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	browser := "text/html,application/xhtml+xml,*/*;q=0.8"

	rec := get("stopped.example.com", browser)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "<h1>Stopped</h1>") {
		t.Errorf("stopped, a browser: %d\n%s", rec.Code, rec.Body.String())
	}
	rec = get("silent.example.com", browser)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "<h1>Not answering</h1>") {
		t.Errorf("not answering, a browser: %d\n%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "%!") {
		t.Errorf("the page renders wrong:\n%s", rec.Body.String())
	}
	rec = get("stopped.example.com", "application/json")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), `"upstream unavailable"`) {
		t.Errorf("an API caller: %d %s", rec.Code, rec.Body.String())
	}
}
