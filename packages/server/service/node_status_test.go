package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node's stored status follows its mesh peer: a server whose peer has left
// reads offline, an agent whose peer is connected reads online, and a row that
// never joined the mesh keeps what it was given.
func TestNodeStatusFollowsTheMesh(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)

	edge := meshdb.Node{OrganizationID: e.org.ID, Name: "edge", TailscaleIP: "100.64.0.2", HeadscaleID: "2",
		K3sRole: meshdb.K3sRoleServer, Status: meshdb.NodeOnline}
	worker := meshdb.Node{OrganizationID: e.org.ID, Name: "worker", TailscaleIP: "100.64.0.3",
		K3sRole: meshdb.K3sRoleAgent, Status: meshdb.NodeOffline}
	unlinked := meshdb.Node{OrganizationID: e.org.ID, Name: "unlinked", TailscaleIP: "100.64.0.9",
		K3sRole: meshdb.K3sRoleAgent, Status: meshdb.NodeOnline}
	for _, n := range []*meshdb.Node{&edge, &worker, &unlinked} {
		require.NoError(t, e.db.Create(n).Error)
	}
	e.hs.set(func(f *fakeHeadscale) {
		f.peers["2"] = "100.64.0.2"
		f.peers["3"] = "100.64.0.3"
		f.online = map[string]bool{"2": false, "3": true}
	})

	e.svcs.Nodes.CheckOnlineForTest(ctx)

	status := func(n meshdb.Node) meshdb.NodeStatus {
		stored, ok := e.exists(t, n.ID)
		require.True(t, ok)
		return stored.Status
	}
	assert.Equal(t, meshdb.NodeOffline, status(edge), "a server whose peer left reads offline")
	assert.Equal(t, meshdb.NodeOnline, status(worker), "a connected agent reads online")
	assert.Equal(t, meshdb.NodeOnline, status(unlinked), "a row never on the mesh is left as it was")

	e.hs.set(func(f *fakeHeadscale) { f.online["2"] = true })
	e.svcs.Nodes.CheckOnlineForTest(ctx)
	assert.Equal(t, meshdb.NodeOnline, status(edge), "back on the mesh, back online")
}
