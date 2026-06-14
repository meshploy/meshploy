package service_test

import (
	"context"
	"testing"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPermissionFixture creates an org with one owner and one member, plus a project.
func setupPermissionFixture(t *testing.T) (svcs *service.Services, orgID, ownerID, memberID, projectID string) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)
	svcs = newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "owner",
		Email:    "owner@example.com",
		Password: "password123",
	})
	require.NoError(t, err)
	ownerID = owner.ID.String()

	// Grab the org created by Register
	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	orgID = orgs[0].ID.String()

	// Create a member user and add to the org
	member, err := svcs.Orgs.AcceptInvitation(ctx, func() string {
		inv, err2 := svcs.Orgs.CreateInvitation(ctx, orgs[0].ID, owner.ID, "member@example.com", meshdb.RoleMember)
		require.NoError(t, err2)
		return inv.Token
	}(), "member", "password123")
	require.NoError(t, err)
	memberID = member.ID.String()

	// Create a project
	project, err := svcs.Projects.Create(ctx, orgs[0].ID, "test-project", "test-project")
	require.NoError(t, err)
	projectID = project.ID.String()

	return
}

func TestPermissionGrant(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, _, memberID, projectID := setupPermissionFixture(t)

	oID := parseUUID(t, orgID)
	mID := parseUUID(t, memberID)
	pID := parseUUID(t, projectID)

	t.Run("grant creates a permission row", func(t *testing.T) {
		err := svcs.Permissions.Grant(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView)
		require.NoError(t, err)

		perms, err := svcs.Permissions.ListForUser(ctx, oID, mID)
		require.NoError(t, err)
		require.Len(t, perms, 1)
		assert.Equal(t, meshdb.ActionView, perms[0].Action)
		assert.Equal(t, meshdb.ResourceProject, perms[0].ResourceType)
	})

	t.Run("duplicate grant is idempotent", func(t *testing.T) {
		err := svcs.Permissions.Grant(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView)
		require.NoError(t, err)

		perms, err := svcs.Permissions.ListForUser(ctx, oID, mID)
		require.NoError(t, err)
		assert.Len(t, perms, 1, "duplicate grant must not create extra rows")
	})
}

func TestPermissionRevoke(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, _, memberID, projectID := setupPermissionFixture(t)

	oID := parseUUID(t, orgID)
	mID := parseUUID(t, memberID)
	pID := parseUUID(t, projectID)

	require.NoError(t, svcs.Permissions.Grant(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView))

	t.Run("revoke removes the permission row", func(t *testing.T) {
		err := svcs.Permissions.Revoke(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView)
		require.NoError(t, err)

		perms, err := svcs.Permissions.ListForUser(ctx, oID, mID)
		require.NoError(t, err)
		assert.Empty(t, perms)
	})

	t.Run("revoke on non-existent grant is a no-op", func(t *testing.T) {
		err := svcs.Permissions.Revoke(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionDeploy)
		require.NoError(t, err)
	})
}

func TestCheckAccess(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, ownerID, memberID, projectID := setupPermissionFixture(t)

	oID := parseUUID(t, orgID)
	owID := parseUUID(t, ownerID)
	mID := parseUUID(t, memberID)
	pID := parseUUID(t, projectID)

	t.Run("owner bypasses all checks", func(t *testing.T) {
		err := svcs.Permissions.CheckAccess(ctx, oID, owID, pID, meshdb.ResourceProject, meshdb.ActionDelete, nil)
		require.NoError(t, err)
	})

	t.Run("member denied without grant", func(t *testing.T) {
		err := svcs.Permissions.CheckAccess(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView, nil)
		require.Error(t, err)
	})

	t.Run("member allowed with direct grant", func(t *testing.T) {
		require.NoError(t, svcs.Permissions.Grant(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView))
		err := svcs.Permissions.CheckAccess(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView, nil)
		require.NoError(t, err)
	})

	t.Run("project-level grant cascades to resource", func(t *testing.T) {
		// member has project view — can view a hypothetical service within it
		fakeServiceID := parseUUID(t, "00000000-0000-0000-0000-000000000001")
		err := svcs.Permissions.CheckAccess(ctx, oID, mID, fakeServiceID, meshdb.ResourceService, meshdb.ActionView, &pID)
		require.NoError(t, err)
	})

	t.Run("project-level grant does not allow different action", func(t *testing.T) {
		fakeServiceID := parseUUID(t, "00000000-0000-0000-0000-000000000002")
		err := svcs.Permissions.CheckAccess(ctx, oID, mID, fakeServiceID, meshdb.ResourceService, meshdb.ActionDeploy, &pID)
		require.Error(t, err, "member has project view but not deploy — service deploy should be denied")
	})
}

func TestVisibleProjectIDs(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, ownerID, memberID, projectID := setupPermissionFixture(t)

	oID := parseUUID(t, orgID)
	owID := parseUUID(t, ownerID)
	mID := parseUUID(t, memberID)
	pID := parseUUID(t, projectID)

	t.Run("admin sees all projects (nil map)", func(t *testing.T) {
		ids, isAdmin, err := svcs.Permissions.VisibleProjectIDs(ctx, oID, owID)
		require.NoError(t, err)
		assert.True(t, isAdmin)
		assert.Nil(t, ids)
	})

	t.Run("member with no grants sees no projects", func(t *testing.T) {
		ids, isAdmin, err := svcs.Permissions.VisibleProjectIDs(ctx, oID, mID)
		require.NoError(t, err)
		assert.False(t, isAdmin)
		assert.Empty(t, ids)
	})

	t.Run("member with view grant sees the project", func(t *testing.T) {
		require.NoError(t, svcs.Permissions.Grant(ctx, oID, mID, pID, meshdb.ResourceProject, meshdb.ActionView))

		ids, isAdmin, err := svcs.Permissions.VisibleProjectIDs(ctx, oID, mID)
		require.NoError(t, err)
		assert.False(t, isAdmin)
		assert.True(t, ids[pID])
	})
}
