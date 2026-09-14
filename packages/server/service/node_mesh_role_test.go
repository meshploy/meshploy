package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node moves into or out of the mesh-only role on the machine itself, by
// installing or removing K3s, so the API refuses both switches. Changes among
// the cluster roles work as before.
func TestMeshRoleCannotBeSwitchedFromTheAPI(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	org := meshdb.Organization{Name: "roles", Slug: "roles"}
	require.NoError(t, gdb.Create(&org).Error)

	node := func(name string, role meshdb.MeshRole) meshdb.Node {
		n := meshdb.Node{OrganizationID: org.ID, Name: name, TailscaleIP: "100.64.0." + name, K3sRole: meshdb.K3sRoleAgent, MeshRole: role}
		require.NoError(t, gdb.Create(&n).Error)
		return n
	}
	mesh := node("10", meshdb.MeshRoleMesh)
	worker := node("11", meshdb.MeshRoleWorkloadBuilder)

	_, err := svcs.Nodes.Update(ctx, mesh.ID, service.UpdateNodeInput{MeshRole: meshdb.MeshRoleWorkload})
	assert.ErrorIs(t, err, service.ErrMeshRoleSwitch, "mesh only into the cluster")
	_, err = svcs.Nodes.Update(ctx, worker.ID, service.UpdateNodeInput{MeshRole: meshdb.MeshRoleMesh})
	assert.ErrorIs(t, err, service.ErrMeshRoleSwitch, "cluster node to mesh only")

	updated, err := svcs.Nodes.Update(ctx, mesh.ID, service.UpdateNodeInput{MeshRole: meshdb.MeshRoleMesh, Name: "edge-box"})
	require.NoError(t, err, "keeping mesh while renaming is not a switch")
	assert.Equal(t, "edge-box", updated.Name)

	updated, err = svcs.Nodes.Update(ctx, worker.ID, service.UpdateNodeInput{MeshRole: meshdb.MeshRoleBuilder})
	require.NoError(t, err, "cluster roles still switch")
	assert.Equal(t, meshdb.MeshRoleBuilder, updated.MeshRole)
}

// Removing a mesh-only node needs no cluster: it has no node object.
func TestMeshOnlyNodeIsRemovedWithoutTheCluster(t *testing.T) {
	ctx := context.Background()
	e := setupRemoval(t)
	n := meshdb.Node{OrganizationID: e.org.ID, Name: "edge", TailscaleIP: "100.64.0.40", HeadscaleID: "40", K3sRole: meshdb.K3sRoleAgent, MeshRole: meshdb.MeshRoleMesh}
	require.NoError(t, e.db.Create(&n).Error)
	e.hs.set(func(f *fakeHeadscale) { f.peers["40"] = "100.64.0.40" })

	res, err := e.svcs.Nodes.Remove(ctx, n.ID)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, []string{"40"}, e.hs.deletedIDs())
}
