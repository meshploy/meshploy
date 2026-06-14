package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateInvitation(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	org := orgs[0]

	t.Run("creates invitation with token and expiry", func(t *testing.T) {
		inv, err := svcs.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "bob@example.com", meshdb.RoleMember)
		require.NoError(t, err)
		assert.Len(t, inv.Token, 64, "token must be 64-char hex")
		assert.Equal(t, "bob@example.com", inv.Email)
		assert.Equal(t, meshdb.RoleMember, inv.Role)
		assert.True(t, inv.ExpiresAt.After(time.Now()), "expiry must be in the future")
		assert.Nil(t, inv.AcceptedAt)
	})
}

func TestGetInvitationByToken(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	org := orgs[0]

	inv, err := svcs.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "bob@example.com", meshdb.RoleMember)
	require.NoError(t, err)

	t.Run("valid token returns invitation with org name", func(t *testing.T) {
		result, err := svcs.Orgs.GetInvitationByToken(ctx, inv.Token)
		require.NoError(t, err)
		assert.Equal(t, "bob@example.com", result.Email)
		assert.Equal(t, org.Name, result.OrgName)
	})

	t.Run("invalid token returns error", func(t *testing.T) {
		_, err := svcs.Orgs.GetInvitationByToken(ctx, "notarealtoken")
		require.Error(t, err)
	})
}

func TestAcceptInvitation(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	org := orgs[0]

	inv, err := svcs.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "bob@example.com", meshdb.RoleMember)
	require.NoError(t, err)

	t.Run("creates user, adds to org, marks accepted", func(t *testing.T) {
		user, err := svcs.Orgs.AcceptInvitation(ctx, inv.Token, "bob", "securepass123")
		require.NoError(t, err)
		assert.Equal(t, "bob@example.com", user.Email)
		assert.Equal(t, "bob", user.Username)

		// Verify org membership
		members, err := svcs.Orgs.ListMembers(ctx, org.ID)
		require.NoError(t, err)
		var found bool
		for _, m := range members {
			if m.UserID == user.ID {
				assert.Equal(t, meshdb.RoleMember, m.Role)
				found = true
			}
		}
		assert.True(t, found, "new user must appear as org member")

		// Token must be consumed
		_, err = svcs.Orgs.GetInvitationByToken(ctx, inv.Token)
		require.Error(t, err, "accepted invitation must not be reusable")
	})

	t.Run("already-accepted token returns error", func(t *testing.T) {
		_, err := svcs.Orgs.AcceptInvitation(ctx, inv.Token, "bob2", "pass")
		require.Error(t, err)
	})
}

func TestListInvitations(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	org := orgs[0]

	inv1, err := svcs.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "pending1@example.com", meshdb.RoleMember)
	require.NoError(t, err)
	_, err = svcs.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "pending2@example.com", meshdb.RoleAdmin)
	require.NoError(t, err)

	// Accept one
	_, err = svcs.Orgs.AcceptInvitation(ctx, inv1.Token, "pending1", "pass123")
	require.NoError(t, err)

	t.Run("only pending invitations are returned", func(t *testing.T) {
		list, err := svcs.Orgs.ListInvitations(ctx, org.ID)
		require.NoError(t, err)
		assert.Len(t, list, 1, "accepted invitation must not appear in pending list")
		assert.Equal(t, "pending2@example.com", list[0].Email)
	})
}
