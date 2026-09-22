package service_test

import (
	"context"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Provisioning is the call a blank server makes before it can join anything.
//
// What it must get right is what it hands over and what it refuses, because it
// is public: the token in the request is the only credential, and it will have
// travelled in a command line.
func TestProvisionRefusesEverythingItShould(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	orgID := seedOrg(t, db, "acme", nil)

	t.Run("an unknown token", func(t *testing.T) {
		_, err := svcs.Nodes.Provision(ctx, "mprov-nothing")
		require.Error(t, err)
	})

	t.Run("a token whose machine already registered", func(t *testing.T) {
		tok, row, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "used", nil)
		require.NoError(t, err)
		now := time.Now()
		require.NoError(t, db.Model(row).Update("used_at", &now).Error)

		_, err = svcs.Nodes.Provision(ctx, tok)
		require.ErrorContains(t, err, "already used")
	})

	t.Run("an expired token", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "old", &past)
		require.NoError(t, err)

		_, err = svcs.Nodes.Provision(ctx, tok)
		require.ErrorContains(t, err, "expired")
	})

	// Without Headscale there is no mesh to join, and saying so beats handing
	// back a response with an empty key in it.
	t.Run("a gateway with no mesh", func(t *testing.T) {
		tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "no-mesh", nil)
		require.NoError(t, err)

		_, err = svcs.Nodes.Provision(ctx, tok)
		require.ErrorContains(t, err, "no mesh")
	})
}

// The role is decided when the token is minted, not asked of the machine. That
// is the whole point of a one-liner with nothing in it but a token.
func TestAProvisioningTokenCarriesTheRoleItWasMintedFor(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	orgID := seedOrg(t, db, "acme", nil)

	_, row, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "builder box", nil, meshdb.MeshRoleBuilder)
	require.NoError(t, err)
	assert.Equal(t, meshdb.MeshRoleBuilder, row.MeshRole)

	// And a token minted without one keeps the column empty rather than
	// guessing, so the default is applied where it is read.
	_, plain, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "plain", nil)
	require.NoError(t, err)
	assert.Empty(t, plain.MeshRole)
}

// Registration still consumes the token, and provisioning must not: the machine
// provisions, joins the mesh, and only then registers.
func TestProvisioningDoesNotConsumeTheToken(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	orgID := seedOrg(t, db, "acme", nil)

	tok, row, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "worker", nil)
	require.NoError(t, err)

	// Provisioning cannot run here (no Headscale), so stand in for its one
	// lasting effect and check registration is still possible afterwards.
	now := time.Now()
	require.NoError(t, db.Model(row).Update("provisioned_at", &now).Error)

	node, secret, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "worker-1", "100.64.0.2", meshdb.MeshRoleWorkload)
	require.NoError(t, err, "a provisioned token must still be able to register its node")
	require.NotEmpty(t, secret)
	assert.Equal(t, meshdb.MeshRoleWorkload, node.MeshRole)

	// And now it is spent, for both.
	_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "worker-2", "100.64.0.3", meshdb.MeshRoleWorkload)
	require.ErrorContains(t, err, "already used")
	_, err = svcs.Nodes.Provision(ctx, tok)
	require.ErrorContains(t, err, "already used")
}

// A provisioning token joins a machine to the mesh and is meant to be pasted
// within the minute. One generated and then abandoned - a wrong role picked, a
// tab closed - used to stay valid for ever, unlisted and unrevokable.
func TestAProvisioningTokenExpiresEvenWhenNobodySaidSo(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	orgID := seedOrg(t, db, "acme", nil)

	_, row, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "forgot to say", nil)
	require.NoError(t, err)
	require.NotNil(t, row.ExpiresAt, "a token with no expiry given must still get one")
	assert.WithinDuration(t, time.Now().Add(time.Hour), *row.ExpiresAt, time.Minute)

	// A caller who genuinely wants longer is still obeyed.
	far := time.Now().Add(30 * 24 * time.Hour)
	_, long, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "a month", &far)
	require.NoError(t, err)
	assert.WithinDuration(t, far, *long.ExpiresAt, time.Second)
}
