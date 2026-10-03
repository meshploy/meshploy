package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/proxy/cache"
	"github.com/meshploy/packages/proxy/meshgate"
)

// withGates runs a test with only the given gates registered.
func withGates(t *testing.T, gs ...Gate) {
	t.Helper()
	gatesMu.Lock()
	saved := gates
	gates = gs
	gatesMu.Unlock()
	t.Cleanup(func() {
		gatesMu.Lock()
		gates = saved
		gatesMu.Unlock()
	})
}

// routedHandler serves app.example.com from a test upstream.
func routedHandler(t *testing.T, e cache.TargetEntry) (*Handler, *int) {
	t.Helper()
	hits := new(int)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Write([]byte("app"))
	}))
	t.Cleanup(app.Close)
	u, _ := url.Parse(app.URL)
	e.TargetIP = u.Hostname()
	e.TargetPort, _ = strconv.Atoi(u.Port())
	if e.Path == "" {
		e.Path = "/"
	}
	c := cache.New(nil, 0)
	c.Set("app.example.com", []cache.TargetEntry{e})
	return NewHandler(c), hits
}

// With no gate registered - Community - a request reaches its app as before.
func TestNoGateServesAsBefore(t *testing.T) {
	withGates(t)
	h, hits := routedHandler(t, cache.TargetEntry{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://app.example.com/", nil))
	if rec.Code != 200 || rec.Body.String() != "app" || *hits != 1 {
		t.Fatalf("code %d body %q hits %d", rec.Code, rec.Body.String(), *hits)
	}
}

// A gate sees whose route it is; one that lets the request through leaves the
// next to decide, and the first to answer wins without the app being reached.
func TestTheFirstGateToAnswerWins(t *testing.T) {
	want := cache.TargetEntry{RouteID: uuid.New(), ServiceID: uuid.New(), ProjectID: uuid.New(), OrgID: uuid.New()}
	var seen []Target
	var third bool
	withGates(t,
		func(w http.ResponseWriter, r *http.Request, tg Target) bool { seen = append(seen, tg); return false },
		func(w http.ResponseWriter, r *http.Request, tg Target) bool {
			http.Redirect(w, r, "https://app.example.com/_meshploy/auth", http.StatusFound)
			return true
		},
		func(w http.ResponseWriter, r *http.Request, tg Target) bool { third = true; return false },
	)
	h, hits := routedHandler(t, want)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://app.example.com:443/x", nil))

	if rec.Code != http.StatusFound || *hits != 0 || third {
		t.Fatalf("code %d hits %d third %v", rec.Code, *hits, third)
	}
	got := seen[0]
	if got.Host != "app.example.com" || got.RouteID != want.RouteID || got.ServiceID != want.ServiceID ||
		got.ProjectID != want.ProjectID || got.OrgID != want.OrgID {
		t.Fatalf("gate saw %+v", got)
	}
}

// A gate that panics has not let the request through: it answers 502.
func TestAPanickingGateFailsClosed(t *testing.T) {
	withGates(t, func(w http.ResponseWriter, r *http.Request, tg Target) bool { panic("boom") })
	h, hits := routedHandler(t, cache.TargetEntry{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://app.example.com/", nil))
	if rec.Code != http.StatusBadGateway || *hits != 0 {
		t.Fatalf("code %d hits %d", rec.Code, *hits)
	}
}

// Gates see only matched routes: an unknown host is still the 404 page, with
// no gate asked.
func TestGatesSkipUnknownHosts(t *testing.T) {
	asked := false
	withGates(t, func(w http.ResponseWriter, r *http.Request, tg Target) bool { asked = true; return true })
	h, _ := routedHandler(t, cache.TargetEntry{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://other.example.com/", nil))
	if rec.Code != http.StatusNotFound || asked {
		t.Fatalf("code %d asked %v", rec.Code, asked)
	}
}

// An internal route answers only the callers the mesh gate allows, and a
// public route never asks it.
func TestInternalRoutesAskTheMeshGate(t *testing.T) {
	withGates(t)
	org := uuid.New()
	enforced := func() *meshgate.Gate {
		g := meshgate.New(nil)
		g.Set(&meshgate.Snapshot{Enforced: true})
		return g
	}
	for _, c := range []struct {
		name     string
		internal bool
		from     string
		want     int
	}{
		{"internal, from a machine nobody knows", true, "100.64.0.9:5000", http.StatusForbidden},
		{"internal, from the gateway itself", true, "127.0.0.1:5000", http.StatusOK},
		{"public, from a machine nobody knows", false, "100.64.0.9:5000", http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, hits := routedHandler(t, cache.TargetEntry{Internal: c.internal, RouteID: uuid.New(), OrgID: org})
			h.CheckInternal(enforced())
			req := httptest.NewRequest(http.MethodGet, "http://app.example.com/", nil)
			req.RemoteAddr = c.from
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d", rec.Code, c.want)
			}
			if (c.want == http.StatusOK) != (*hits == 1) {
				t.Errorf("app hits %d", *hits)
			}
		})
	}
}
