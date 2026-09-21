package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fakeHeadscale serves the two calls node removal makes: listing peers and
// deleting one. down fails every call, as an unreachable Headscale would.
type fakeHeadscale struct {
	mu            sync.Mutex
	down          bool
	notFoundAs500 bool              // how some Headscale versions answer a missing node
	peers         map[string]string // peer ID to mesh IP
	deleted       []string
}

func (f *fakeHeadscale) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/node":
		nodes := []map[string]any{}
		for id, ip := range f.peers {
			nodes = append(nodes, map[string]any{"id": id, "ipAddresses": []string{ip}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"nodes": nodes})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/node/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/node/")
		if _, ok := f.peers[id]; !ok {
			status := http.StatusNotFound
			if f.notFoundAs500 {
				status = http.StatusInternalServerError
			}
			http.Error(w, "node not found", status)
			return
		}
		delete(f.peers, id)
		f.deleted = append(f.deleted, id)
		_, _ = w.Write([]byte("{}"))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeHeadscale) set(fn func(*fakeHeadscale)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeHeadscale) deletedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

type removalEnv struct {
	svcs *service.Services
	db   *gorm.DB
	hs   *fakeHeadscale
	org  meshdb.Organization
}

func setupRemoval(t *testing.T) removalEnv {
	t.Helper()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	hs := &fakeHeadscale{peers: map[string]string{}}
	srv := httptest.NewServer(hs)
	t.Cleanup(srv.Close)
	svcs.Nodes.SetHeadscaleForTest(service.NewHeadscaleService(srv.URL, "test-key"))
	t.Cleanup(service.NoRemovalWaitsForTest())
	org := meshdb.Organization{Name: "nodes", Slug: "nodes"}
	require.NoError(t, gdb.Create(&org).Error)
	return removalEnv{svcs: svcs, db: gdb, hs: hs, org: org}
}

func (e removalEnv) node(t *testing.T, name, ip, headscaleID string) meshdb.Node {
	t.Helper()
	n := meshdb.Node{OrganizationID: e.org.ID, Name: name, TailscaleIP: ip, HeadscaleID: headscaleID, K3sRole: meshdb.K3sRoleAgent}
	require.NoError(t, e.db.Create(&n).Error)
	return n
}

func (e removalEnv) exists(t *testing.T, id any) (meshdb.Node, bool) {
	t.Helper()
	var n meshdb.Node
	err := e.db.First(&n, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return n, false
	}
	require.NoError(t, err)
	return n, true
}

// A removal Headscale cannot confirm leaves the node in place, marked, and
// finishes once Headscale is back.
func TestNodeRemovalWaitsForHeadscale(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)
	n := e.node(t, "enthesus-pc", "100.64.0.6", "7")
	e.hs.set(func(f *fakeHeadscale) { f.peers["7"] = "100.64.0.6"; f.down = true })

	res, err := e.svcs.Nodes.Remove(ctx, n.ID)
	require.NoError(t, err)
	assert.False(t, res.Removed)
	assert.Contains(t, res.Error, "Headscale")

	stored, ok := e.exists(t, n.ID)
	require.True(t, ok, "the node stays while Headscale still has its peer")
	assert.NotNil(t, stored.RemovalRequestedAt)
	assert.NotEmpty(t, stored.RemovalError)

	e.hs.set(func(f *fakeHeadscale) { f.down = false })
	e.svcs.Nodes.FinishPendingRemovals(ctx)
	_, ok = e.exists(t, n.ID)
	assert.False(t, ok, "removed once Headscale dropped the peer")
	assert.Equal(t, []string{"7"}, e.hs.deletedIDs())
}

// A node that never had its peer ID recorded is matched by mesh IP.
func TestNodeRemovalFindsThePeerByMeshIP(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)
	n := e.node(t, "worker", "100.64.0.9", "")
	e.hs.set(func(f *fakeHeadscale) { f.peers["12"] = "100.64.0.9" })

	res, err := e.svcs.Nodes.Remove(ctx, n.ID)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, []string{"12"}, e.hs.deletedIDs())
}

// A mesh IP another node now holds is never taken for a stale node's peer.
func TestNodeRemovalLeavesAnotherNodesPeer(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)
	stale := e.node(t, "old", "100.64.0.9", "")
	current := e.node(t, "new", "100.64.0.10", "12")
	e.hs.set(func(f *fakeHeadscale) { f.peers["12"] = "100.64.0.9" })

	res, err := e.svcs.Nodes.Remove(ctx, stale.ID)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Empty(t, e.hs.deletedIDs(), "peer 12 belongs to another node")
	_, ok := e.exists(t, current.ID)
	assert.True(t, ok)
}

// A peer Headscale no longer has counts as removed, however it says so.
func TestNodeRemovalTreatsAMissingPeerAsGone(t *testing.T) {
	ctx := context.Background()
	for _, as500 := range []bool{false, true} {
		e := setupRemoval(t)
		e.hs.set(func(f *fakeHeadscale) { f.notFoundAs500 = as500 })
		n := e.node(t, "gone", "100.64.0.20", "99")

		res, err := e.svcs.Nodes.Remove(ctx, n.ID)
		require.NoError(t, err)
		assert.True(t, res.Removed, "missing peer answered with 500: %v", as500)
	}
}

// Cancelling a waiting removal keeps the node as it was.
func TestNodeRemovalCanBeCancelled(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)
	n := e.node(t, "keep", "100.64.0.30", "30")
	e.hs.set(func(f *fakeHeadscale) { f.peers["30"] = "100.64.0.30"; f.down = true })

	res, err := e.svcs.Nodes.Remove(ctx, n.ID)
	require.NoError(t, err)
	require.False(t, res.Removed)

	require.NoError(t, e.svcs.Nodes.CancelRemoval(ctx, n.ID))
	stored, ok := e.exists(t, n.ID)
	require.True(t, ok)
	assert.Nil(t, stored.RemovalRequestedAt)
	assert.Empty(t, stored.RemovalError)
	assert.ErrorIs(t, e.svcs.Nodes.CancelRemoval(ctx, n.ID), service.ErrNoPendingRemoval)

	// The worker no longer touches it once Headscale is back.
	e.hs.set(func(f *fakeHeadscale) { f.down = false })
	e.svcs.Nodes.FinishPendingRemovals(ctx)
	_, ok = e.exists(t, n.ID)
	assert.True(t, ok)
	assert.Empty(t, e.hs.deletedIDs())
}

func TestGatewayNodeCannotBeRemoved(t *testing.T) {
	e := setupRemoval(t)
	gw := meshdb.Node{OrganizationID: e.org.ID, Name: "gateway", TailscaleIP: "100.64.0.1", K3sRole: meshdb.K3sRoleServer}
	require.NoError(t, e.db.Create(&gw).Error)
	_, err := e.svcs.Nodes.Remove(context.Background(), gw.ID)
	assert.ErrorIs(t, err, service.ErrGatewayNode)
}

// A port that forwarded to a removed node has nowhere to go, and the gateway
// must not keep listening on it: it would answer nothing, or - once a mesh
// address is handed to the next machine - answer as something else entirely.
func TestRemovingANodeWithdrawsThePortsThatForwardedToIt(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)
	node := e.node(t, "worker-1", "100.64.0.3", "8")
	e.hs.set(func(f *fakeHeadscale) { f.peers["8"] = "100.64.0.3" })

	project := meshdb.Project{OrganizationID: e.org.ID, Name: "shop", Slug: "shop"}
	require.NoError(t, e.db.Create(&project).Error)
	// One route pointing at the node, one at its address by hand, and one that
	// has nothing to do with it.
	byNode := meshdb.TCPRoute{OrganizationID: e.org.ID, ProjectID: project.ID, GatewayPort: 10000,
		NodeID: &node.ID, TargetIP: "100.64.0.3", TargetPort: 8302, Published: true,
		Status: meshdb.TCPRouteOpen, Zone: meshdb.TCPZonePublic, AllowedCIDRs: meshdb.StringArray{"10.0.0.0/8"}}
	byAddress := meshdb.TCPRoute{OrganizationID: e.org.ID, ProjectID: project.ID, GatewayPort: 10001,
		TargetIP: "100.64.0.3", TargetPort: 5432, Published: true, Status: meshdb.TCPRouteOpen, Zone: meshdb.TCPZonePublic}
	elsewhere := meshdb.TCPRoute{OrganizationID: e.org.ID, ProjectID: project.ID, GatewayPort: 10002,
		TargetIP: "100.64.0.9", TargetPort: 5432, Published: true, Status: meshdb.TCPRouteOpen, Zone: meshdb.TCPZonePublic}
	for _, r := range []*meshdb.TCPRoute{&byNode, &byAddress, &elsewhere} {
		require.NoError(t, e.db.Create(r).Error)
	}

	res, err := e.svcs.Nodes.Remove(ctx, node.ID)
	require.NoError(t, err)
	require.True(t, res.Removed, res.Error)

	// Kept, not deleted: the port, the zone and the allowlist are decisions
	// that are still good, and re-pointing the route is one edit.
	var after meshdb.TCPRoute
	require.NoError(t, e.db.First(&after, "id = ?", byNode.ID).Error)
	assert.False(t, after.Published)
	assert.Equal(t, meshdb.TCPRoutePaused, after.Status)
	assert.Equal(t, 10000, after.GatewayPort)
	assert.Equal(t, meshdb.StringArray{"10.0.0.0/8"}, after.AllowedCIDRs)
	// Nothing left pointing at a machine that is gone.
	assert.Empty(t, after.TargetIP)
	assert.Zero(t, after.TargetPort)
	assert.Contains(t, after.LastError, "worker-1")

	var aimedByHand meshdb.TCPRoute
	require.NoError(t, e.db.First(&aimedByHand, "id = ?", byAddress.ID).Error)
	assert.False(t, aimedByHand.Published, "a route aimed at the node's address is withdrawn too")

	var untouched meshdb.TCPRoute
	require.NoError(t, e.db.First(&untouched, "id = ?", elsewhere.ID).Error)
	assert.True(t, untouched.Published, "a route to another node is left alone")
	assert.Equal(t, "100.64.0.9", untouched.TargetIP)
}
