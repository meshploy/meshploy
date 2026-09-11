package service_test

import (
	"context"
	"testing"

	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// Community runs one organization per server. A second one would have an owner
// who owns nothing on the server, so the API refuses it.
func TestOrgsCreateRefusesASecondOrganization(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "alice", Email: "alice@example.com", Password: "password123",
	})
	require.NoError(t, err)

	_, err = svcs.Orgs.Create(ctx, owner.ID, service.CreateOrgInput{Name: "second", Slug: "second"})
	require.ErrorIs(t, err, service.ErrSingleOrganization)

	first, err := svcs.Orgs.First(ctx)
	require.NoError(t, err)

	// A server left with none may make its organization again.
	require.NoError(t, svcs.Orgs.Delete(ctx, first.ID))
	again, err := svcs.Orgs.Create(ctx, owner.ID, service.CreateOrgInput{Name: "again", Slug: "again"})
	require.NoError(t, err)

	now, err := svcs.Orgs.First(ctx)
	require.NoError(t, err)
	require.Equal(t, again.ID, now.ID)
}
