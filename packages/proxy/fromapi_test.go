package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/cache"
	"github.com/meshploy/packages/proxy/edge"
	"github.com/meshploy/packages/proxy/saved"
)

// fakeAPI hands out a snapshot as the API's edge endpoints do, holding a
// request until its version moves, and keeps the TCP reports it is sent.
type fakeAPI struct {
	t      *testing.T
	nodeID uuid.UUID
	secret string

	mu      sync.Mutex
	snap    edge.Snapshot
	changed chan struct{}
	down    bool
	reports []edge.TCPStatus
	asked   []url.Values
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	a := &fakeAPI{t: t, nodeID: uuid.New(), secret: "mnode-edge", changed: make(chan struct{})}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	return a, srv
}

func (a *fakeAPI) set(f func(*edge.Snapshot)) {
	a.mu.Lock()
	f(&a.snap)
	a.snap.Versions.Routes++
	close(a.changed)
	a.changed = make(chan struct{})
	a.mu.Unlock()
}

func (a *fakeAPI) setDown(down bool) {
	a.mu.Lock()
	a.down = down
	a.mu.Unlock()
}

func (a *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	down := a.down
	a.mu.Unlock()
	if down {
		http.Error(w, "down", http.StatusBadGateway)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+a.secret || r.Header.Get(edge.NodeHeader) != a.nodeID.String() {
		http.Error(w, "not this cluster's edge", http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case edge.SnapshotPath:
		q := r.URL.Query()
		a.mu.Lock()
		a.asked = append(a.asked, q)
		snap, changed := a.snap, a.changed
		a.mu.Unlock()
		if q.Has("routes") && q.Get("routes") == strconv.FormatInt(snap.Versions.Routes, 10) {
			wait, _ := strconv.Atoi(q.Get("wait"))
			select {
			case <-changed:
			case <-time.After(time.Duration(wait) * time.Second):
				w.WriteHeader(http.StatusNoContent)
				return
			case <-r.Context().Done():
				return
			}
			a.mu.Lock()
			snap = a.snap
			a.mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(snap)
	case edge.TCPStatusPath:
		var body struct{ Routes []edge.TCPStatus }
		_ = json.NewDecoder(r.Body).Decode(&body)
		a.mu.Lock()
		a.reports = append(a.reports, body.Routes...)
		a.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (a *fakeAPI) reported(id uuid.UUID, status db.TCPRouteStatus) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.reports {
		if r.ID == id && r.Status == status {
			return true
		}
	}
	return false
}

func backend(t *testing.T, says string) (string, int) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, says) }))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	return host, p
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func ask(h http.Handler, host string, html bool) (int, string) {
	r := httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
	if html {
		r.Header.Set("Accept", "text/html")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within 5s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A proxy with no database serves what the API hands its edge: routes, TCP
// ports, the brand on its own pages, a stopped service; it follows a change
// within a moment, reports its ports back, keeps serving while the API is
// down, and a proxy started while the API is down serves the saved copy.
func TestAProxyWithoutADatabaseServesTheAPIsSnapshot(t *testing.T) {
	api, srv := newFakeAPI(t)
	org, stoppedSvc := uuid.New(), uuid.New()
	ip1, port1 := backend(t, "first")
	ip2, port2 := backend(t, "second")
	tcpIP, tcpTarget := backend(t, "over tcp")
	gw := freePort(t)
	tcpRoute := db.TCPRoute{GatewayPort: gw, TargetIP: tcpIP, TargetPort: tcpTarget, Published: true, Status: db.TCPRoutePending}
	tcpRoute.ID = uuid.New()
	api.set(func(s *edge.Snapshot) {
		s.Routes = cache.State{Routes: map[string][]cache.TargetEntry{
			"app.acme.test":  {{Path: "/", TargetIP: ip1, TargetPort: port1, OrgID: org}},
			"down.acme.test": {{Path: "/", TargetIP: "127.0.0.1", TargetPort: freePort(t), ServiceID: stoppedSvc, OrgID: org}},
		}}
		s.TCP = []db.TCPRoute{tcpRoute}
		s.Pages = &edge.Pages{
			Brands:          map[uuid.UUID]string{org: "Acme Cloud"},
			Domains:         map[string]uuid.UUID{"acme.test": org},
			StoppedServices: map[uuid.UUID]bool{stoppedSvc: true},
		}
	})

	dir, err := saved.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &edge.Client{Base: srv.URL, NodeID: api.nodeID, Secret: api.secret}
	h, follower := fromAPI(client, dir, "127.0.0.1")
	follower.Wait = 2 * time.Second
	follower.Retry = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { follower.Run(ctx); close(done) }()

	eventually(t, "the route served", func() bool { c, b := ask(h, "app.acme.test", false); return c == 200 && b == "first" })
	if c, b := ask(h, "nothing.acme.test", true); c != http.StatusNotFound || !strings.Contains(b, "Acme Cloud") {
		t.Errorf("a host under the org's domain with no route: %d, brand missing:\n%s", c, b)
	}
	if c, b := ask(h, "down.acme.test", true); c != http.StatusServiceUnavailable || !strings.Contains(b, "Stopped") || !strings.Contains(b, "Acme Cloud") {
		t.Errorf("a stopped service: %d\n%s", c, b)
	}
	eventually(t, "the TCP port open", func() bool {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(gw))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b) == "over tcp"
	})
	eventually(t, "the TCP port reported open", func() bool { return api.reported(tcpRoute.ID, db.TCPRouteOpen) })

	// A change reaches the edge while its request is held.
	api.set(func(s *edge.Snapshot) {
		s.Routes.Routes["app.acme.test"] = []cache.TargetEntry{{Path: "/", TargetIP: ip2, TargetPort: port2, OrgID: org}}
	})
	start := time.Now()
	eventually(t, "the change served", func() bool { _, b := ask(h, "app.acme.test", false); return b == "second" })
	if time.Since(start) > 2*time.Second {
		t.Errorf("the change took %s", time.Since(start))
	}

	// The API down: what is served stays.
	api.setDown(true)
	time.Sleep(300 * time.Millisecond)
	if _, b := ask(h, "app.acme.test", false); b != "second" {
		t.Errorf("with the API down the route answered %q", b)
	}
	cancel()
	<-done

	// A proxy started while the API is down serves what the first one saved.
	h2, f2 := fromAPI(client, dir, "127.0.0.1")
	f2.Retry = 50 * time.Millisecond
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() { f2.Run(ctx2); close(done2) }()
	defer func() { cancel2(); <-done2 }()
	eventually(t, "the saved route served", func() bool { _, b := ask(h2, "app.acme.test", false); return b == "second" })
	if c, b := ask(h2, "down.acme.test", true); c != http.StatusServiceUnavailable || !strings.Contains(b, "Acme Cloud") {
		t.Errorf("the saved pages: %d\n%s", c, b)
	}
	eventually(t, "the saved TCP port open", func() bool {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(gw))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	})
}

// Every few seconds the whole snapshot is asked for even when nothing moved,
// which is how a brand or a stopped service reaches the edge.
func TestTheEdgeAsksForTheWholeSnapshotRegardless(t *testing.T) {
	api, srv := newFakeAPI(t)
	api.set(func(s *edge.Snapshot) { s.Routes = cache.State{Routes: map[string][]cache.TargetEntry{}} })
	client := &edge.Client{Base: srv.URL, NodeID: api.nodeID, Secret: api.secret}
	served := make(chan *edge.Snapshot, 16)
	f := &edge.Follower{Client: client, Full: 300 * time.Millisecond, Wait: time.Second, Parts: edge.Parts{
		Serve:   func(s *edge.Snapshot) { served <- s },
		Restore: func() { t.Error("restored with the API up") },
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go f.Run(ctx)
	for i := 0; i < 2; i++ {
		select {
		case <-served:
		case <-ctx.Done():
			t.Fatalf("served %d whole snapshots, want 2", i)
		}
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	full := 0
	for _, q := range api.asked {
		if !q.Has("routes") {
			full++
		}
	}
	if full < 2 {
		t.Errorf("asked for the whole snapshot %d times", full)
	}
}

// Refused credentials are not taken for an answer: what is served stays, and
// the saved copy is served when it is the first answer.
func TestRefusedCredentialsServeTheSavedCopy(t *testing.T) {
	api, srv := newFakeAPI(t)
	api.set(func(s *edge.Snapshot) {})
	client := &edge.Client{Base: srv.URL, NodeID: api.nodeID, Secret: "mnode-wrong"}
	restored := make(chan bool, 1)
	f := &edge.Follower{Client: client, Retry: 20 * time.Millisecond, Parts: edge.Parts{
		Serve:   func(*edge.Snapshot) { t.Error("served with the wrong secret") },
		Restore: func() { restored <- true },
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go f.Run(ctx)
	select {
	case <-restored:
	case <-ctx.Done():
		t.Fatal("the saved copy was not served")
	}
	if _, err := client.Fetch(context.Background(), nil, 0); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("fetch with the wrong secret: %v", err)
	}
}
