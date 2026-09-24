package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Windows or macOS machine can join the mesh and nothing else: K3s has no
// Windows agent, and macOS cannot be a Kubernetes node. The join scripts ask
// for mesh only, but the API is what holds the line.
func TestANonLinuxMachineJoinsAsMeshOnly(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)

	t.Run("a Mac is recorded as mesh only, even when it asks for nothing", func(t *testing.T) {
		tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "mac", nil)
		require.NoError(t, err)
		node, secret, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "studio", "100.64.0.20", "", meshdb.NodeOSDarwin)
		require.NoError(t, err)
		require.NotEmpty(t, secret)
		assert.Equal(t, meshdb.MeshRoleMesh, node.MeshRole)
		assert.Equal(t, meshdb.NodeOSDarwin, node.OS)

		var stored meshdb.Node
		require.NoError(t, gdb.First(&stored, "id = ?", node.ID).Error)
		assert.Equal(t, meshdb.NodeOSDarwin, stored.OS, "the OS is persisted, not only returned")
		assert.Equal(t, meshdb.MeshRoleMesh, stored.MeshRole)
	})

	// Refused before anything happens, so the same token still works once the
	// command is corrected: a refusal must not cost the operator a token.
	t.Run("a Windows machine asking for a cluster role is refused, and the token survives", func(t *testing.T) {
		tok, row, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "pc", nil)
		require.NoError(t, err)

		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "gaming-pc", "100.64.0.21", meshdb.MeshRoleWorkloadBuilder, meshdb.NodeOSWindows)
		require.ErrorIs(t, err, service.ErrNonLinuxClusterRole)

		var count int64
		require.NoError(t, gdb.Model(&meshdb.Node{}).Where("name = ?", "gaming-pc").Count(&count).Error)
		assert.Zero(t, count, "no node is created for a refused machine")
		require.NoError(t, gdb.First(row, "id = ?", row.ID).Error)
		assert.Nil(t, row.UsedAt, "the token is not spent by a refusal")

		node, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "gaming-pc", "100.64.0.21", meshdb.MeshRoleMesh, meshdb.NodeOSWindows)
		require.NoError(t, err, "the same token works once the role is right")
		assert.Equal(t, meshdb.NodeOSWindows, node.OS)
	})

	t.Run("the same rule holds for the org's registration token", func(t *testing.T) {
		tok, err := svcs.Nodes.GenerateRegistrationToken(ctx, orgID)
		require.NoError(t, err)
		_, err = svcs.Nodes.RegisterWithToken(ctx, tok, "laptop", "100.64.0.22", meshdb.MeshRoleBuilder, meshdb.NodeOSDarwin)
		require.ErrorIs(t, err, service.ErrNonLinuxClusterRole)

		node, err := svcs.Nodes.RegisterWithToken(ctx, tok, "laptop", "100.64.0.22", "", meshdb.NodeOSDarwin)
		require.NoError(t, err)
		assert.Equal(t, meshdb.MeshRoleMesh, node.MeshRole)
	})

	// install.sh sent no OS before it was recorded, and runs only on Linux, so
	// a node that says nothing is Linux and keeps every role it could have.
	t.Run("no OS means Linux, with the cluster roles it always had", func(t *testing.T) {
		tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "linux", nil)
		require.NoError(t, err)
		node, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "worker", "100.64.0.23", meshdb.MeshRoleWorkload, "")
		require.NoError(t, err)
		assert.Equal(t, meshdb.NodeOSLinux, node.OS)
		assert.Equal(t, meshdb.MeshRoleWorkload, node.MeshRole)
	})
}

// A Windows node runs windows_exporter, whose metrics Meshploy does not read
// yet. The node page must say so, not show a machine with no CPU and no memory.
func TestWindowsNodeMetricsAreUnsupportedRatherThanEmpty(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	org := meshdb.Organization{Name: "win", Slug: "win"}
	require.NoError(t, gdb.Create(&org).Error)

	n := meshdb.Node{OrganizationID: org.ID, Name: "pc", TailscaleIP: "100.64.0.30", K3sRole: meshdb.K3sRoleAgent,
		MeshRole: meshdb.MeshRoleMesh, OS: meshdb.NodeOSWindows}
	require.NoError(t, gdb.Create(&n).Error)

	_, err := svcs.Nodes.GetNodeMetrics(ctx, n.ID)
	assert.ErrorIs(t, err, service.ErrNodeMetricsUnsupported)
}
