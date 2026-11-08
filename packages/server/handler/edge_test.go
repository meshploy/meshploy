package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/edge"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
)

const edgeSecret = "mnode-0123456789abcdef"

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// edgeFixture is an agent-mode API with its edge node registered, a project
// with a published route, a paused one, a TCP port and a stopped service.
type edgeFixture struct {
	h       *Handler
	gdb     *gorm.DB
	edge    db.Node
	worker  db.Node
	project db.Project
	stopped uuid.UUID
	tcp     db.TCPRoute
}

func newEdgeFixture(t *testing.T, clusterAgent bool) *edgeFixture {
	t.Helper()
	gdb := newAuthzTestDB(t)
	f := &edgeFixture{gdb: gdb, h: New(nil, service.New(gdb, &config.Config{ClusterAgent: clusterAgent}))}
	org := db.Organization{Name: "Acme", Slug: "acme"}
	must(t, gdb.Create(&org).Error)
	f.edge = db.Node{OrganizationID: org.ID, Name: "edge", TailscaleIP: "100.64.0.2", K3sRole: db.K3sRoleServer, Status: "online",
		NodeSecretHash: sha(edgeSecret), ClusterAgentCert: "-----BEGIN CERTIFICATE-----", ClusterAgentPort: 7443}
	must(t, gdb.Create(&f.edge).Error)
	f.worker = db.Node{OrganizationID: org.ID, Name: "worker", TailscaleIP: "100.64.0.3", K3sRole: db.K3sRoleAgent, Status: "online",
		NodeSecretHash: sha("mnode-worker")}
	must(t, gdb.Create(&f.worker).Error)
	f.project = db.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop"}
	must(t, gdb.Create(&f.project).Error)
	svc := db.Service{ProjectID: f.project.ID, Name: "web", Type: db.ServiceTypeApplication, Status: db.ServiceStopped}
	must(t, gdb.Create(&svc).Error)
	f.stopped = svc.ID
	for _, r := range []struct {
		host      string
		published bool
	}{{"app.acme.test", true}, {"paused.acme.test", false}} {
		route := db.Route{OrganizationID: org.ID, ProjectID: f.project.ID, Hostname: r.host}
		must(t, gdb.Create(&route).Error)
		// Published defaults to true, so a false is written after.
		must(t, gdb.Model(&route).Update("published", r.published).Error)
		must(t, gdb.Create(&db.RouteTarget{RouteID: route.ID, Path: "/", ServiceID: &svc.ID, TargetIP: "100.64.0.3", TargetPort: 30080}).Error)
	}
	f.tcp = db.TCPRoute{OrganizationID: org.ID, ProjectID: f.project.ID, GatewayPort: 15432, TargetIP: "100.64.0.3", TargetPort: 31432, Published: true}
	must(t, gdb.Create(&f.tcp).Error)
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *edgeFixture) get(t *testing.T, node uuid.UUID, secret, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, edge.SnapshotPath+query, nil)
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if node != uuid.Nil {
		req.Header.Set(edge.NodeHeader, node.String())
	}
	w := httptest.NewRecorder()
	f.h.EdgeSnapshot(w, req)
	return w
}

func (f *edgeFixture) snapshot(t *testing.T, query string) (*edge.Snapshot, int) {
	t.Helper()
	w := f.get(t, f.edge.ID, edgeSecret, query)
	if w.Code != http.StatusOK {
		return nil, w.Code
	}
	var s edge.Snapshot
	must(t, json.Unmarshal(w.Body.Bytes(), &s))
	return &s, w.Code
}

// Only the node holding the cluster agent, with its own secret, on an API
// that reaches its cluster through one, is answered. Everyone else is told
// 401 and nothing else, the snapshot and the report alike.
func TestOnlyTheEdgeIsHandedTheSnapshot(t *testing.T) {
	f := newEdgeFixture(t, true)
	cases := map[string]struct {
		node   uuid.UUID
		secret string
	}{
		"no credentials":           {uuid.Nil, ""},
		"no node":                  {uuid.Nil, edgeSecret},
		"no secret":                {f.edge.ID, ""},
		"a wrong secret":           {f.edge.ID, "mnode-wrong"},
		"another node's secret":    {f.edge.ID, "mnode-worker"},
		"a worker, its own secret": {f.worker.ID, "mnode-worker"},
		"an unknown node":          {uuid.New(), edgeSecret},
		"a registration token":     {f.edge.ID, "mreg-0123"},
	}
	for name, c := range cases {
		if w := f.get(t, c.node, c.secret, ""); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "app.acme.test") {
			t.Errorf("%s: %d %s, want 401 and nothing", name, w.Code, w.Body)
		}
		req := httptest.NewRequest(http.MethodPost, edge.TCPStatusPath, strings.NewReader(`{"routes":[{"id":"`+f.tcp.ID.String()+`","status":"failed","last_error":"x"}]}`))
		req.Header.Set("Authorization", "Bearer "+c.secret)
		req.Header.Set(edge.NodeHeader, c.node.String())
		w := httptest.NewRecorder()
		f.h.EdgeTCPStatus(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s reporting: %d, want 401", name, w.Code)
		}
	}
	var tcp db.TCPRoute
	must(t, f.gdb.First(&tcp, "id = ?", f.tcp.ID).Error)
	if tcp.Status == db.TCPRouteFailed {
		t.Error("a refused report was recorded")
	}

	// The edge being removed is no longer answered.
	now := time.Now()
	must(t, f.gdb.Model(&db.Node{}).Where("id = ?", f.edge.ID).Update("removal_requested_at", now).Error)
	if w := f.get(t, f.edge.ID, edgeSecret, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("an edge being removed: %d, want 401", w.Code)
	}
	must(t, f.gdb.Model(&db.Node{}).Where("id = ?", f.edge.ID).Update("removal_requested_at", nil).Error)

	// Its agent revoked, it is not the edge.
	must(t, f.gdb.Model(&db.Node{}).Where("id = ?", f.edge.ID).Update("cluster_agent_cert", "").Error)
	if w := f.get(t, f.edge.ID, edgeSecret, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("an edge whose agent was revoked: %d, want 401", w.Code)
	}
}

// An API that holds its own cluster has no edge to answer, whoever asks.
func TestACommunityAPIAnswersNoEdge(t *testing.T) {
	f := newEdgeFixture(t, false)
	if w := f.get(t, f.edge.ID, edgeSecret, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("%d, want 401", w.Code)
	}
}

// The snapshot is what the proxy beside the database would serve: published
// routes only, the TCP ports, the mesh gate's state and the stopped service.
func TestTheSnapshotIsWhatTheEdgeServes(t *testing.T) {
	f := newEdgeFixture(t, true)
	s, code := f.snapshot(t, "")
	if code != http.StatusOK {
		t.Fatalf("%d", code)
	}
	if e := s.Routes.Routes["app.acme.test"]; len(e) != 1 || e[0].TargetIP != "100.64.0.3" || e[0].TargetPort != 30080 || e[0].ServiceID != f.stopped {
		t.Errorf("published route: %+v", e)
	}
	if _, ok := s.Routes.Routes["paused.acme.test"]; ok {
		t.Error("a paused route is in the snapshot")
	}
	if len(s.TCP) != 1 || s.TCP[0].GatewayPort != 15432 {
		t.Errorf("tcp: %+v", s.TCP)
	}
	if s.Mesh == nil {
		t.Error("no mesh gate state")
	}
	if !s.Pages.Stopped(context.Background(), f.stopped) {
		t.Error("the stopped service is not said to be stopped")
	}
	if raw := f.get(t, f.edge.ID, edgeSecret, "").Body.String(); strings.Contains(raw, "BEGIN CERTIFICATE") || strings.Contains(raw, f.edge.NodeSecretHash) {
		t.Error("the snapshot carries the node's credentials")
	}
}

// Held with the versions the edge has, the request answers 204 when nothing
// moved, at once when they already differ, and early when a change lands
// while it waits.
func TestTheSnapshotIsHeldUntilSomethingMoves(t *testing.T) {
	f := newEdgeFixture(t, true)
	s, _ := f.snapshot(t, "")
	v := s.Versions
	q := func(v edge.Versions, wait int) string {
		return "?routes=" + itoa(v.Routes) + "&mesh=" + itoa(v.Mesh) + "&wait=" + itoa(int64(wait))
	}

	start := time.Now()
	if _, code := f.snapshot(t, q(v, 1)); code != http.StatusNoContent {
		t.Fatalf("unchanged: %d, want 204", code)
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Error("an unchanged snapshot was not held")
	}

	if _, code := f.snapshot(t, q(edge.Versions{Routes: v.Routes - 1, Mesh: v.Mesh}, 20)); code != http.StatusOK {
		t.Fatalf("behind: %d, want the snapshot", code)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = f.gdb.Model(&db.Route{}).Where("hostname = ?", "paused.acme.test").Update("published", true).Error
	}()
	start = time.Now()
	got, code := f.snapshot(t, q(v, 20))
	if code != http.StatusOK {
		t.Fatalf("changed while held: %d", code)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("answered after %s, want soon after the change", time.Since(start))
	}
	if _, ok := got.Routes.Routes["paused.acme.test"]; !ok || got.Versions == v {
		t.Errorf("the change is not in what was answered: %+v", got.Versions)
	}

	for _, bad := range []string{"?routes=x&mesh=1", "?routes=1", "?routes=1&mesh=1&wait=-1"} {
		if w := f.get(t, f.edge.ID, edgeSecret, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, w.Code)
		}
	}
}

// The edge's report sets a TCP route's status and error, and nothing else.
func TestTheEdgeReportsItsTCPPorts(t *testing.T) {
	f := newEdgeFixture(t, true)
	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, edge.TCPStatusPath, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+edgeSecret)
		req.Header.Set(edge.NodeHeader, f.edge.ID.String())
		w := httptest.NewRecorder()
		f.h.EdgeTCPStatus(w, req)
		return w.Code
	}
	if code := post(`{"routes":[{"id":"` + f.tcp.ID.String() + `","status":"failed","last_error":"address in use"}]}`); code != http.StatusNoContent {
		t.Fatalf("report: %d", code)
	}
	var got db.TCPRoute
	must(t, f.gdb.First(&got, "id = ?", f.tcp.ID).Error)
	if got.Status != db.TCPRouteFailed || got.LastError != "address in use" || got.TargetPort != 31432 || !got.Published {
		t.Errorf("recorded: %+v", got)
	}
	for _, bad := range []string{
		`{"routes":[{"id":"` + f.tcp.ID.String() + `","status":"pending"}]}`,
		`{"routes":[{"id":"` + f.tcp.ID.String() + `","status":"open","target_port":1}]}`,
		`not json`,
	} {
		if code := post(bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, code)
		}
	}
	must(t, f.gdb.First(&got, "id = ?", f.tcp.ID).Error)
	if got.TargetPort != 31432 || got.Status != db.TCPRouteFailed {
		t.Errorf("a refused report changed the route: %+v", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
