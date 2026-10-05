package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// An edition's derived grants (here a stand-in "crew" source) are read like
// direct ones by every check, are not touched by the console's direct grant
// and revoke, show on the Access page as one line for their source, and go,
// with the edition told, when their resource or their person goes.
func TestDerivedGrantsAreReadLikeDirectOnes(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, _, gdb := setupAgentFixture(t)

	crew := uuid.New()
	forgotResources, forgotMembers := 0, 0
	service.RegisterGrantSource("crew", service.GrantSource{
		Label: "crew",
		Name:  func(context.Context, uuid.UUID) string { return "Platform" },
		Link:  func(id uuid.UUID) string { return "/crews/" + id.String() },
	})
	service.RegisterResourceForgetHook(func(context.Context, *gorm.DB, meshdb.ResourceType, uuid.UUID) error { forgotResources++; return nil })
	service.RegisterMemberForgetHook(func(context.Context, *gorm.DB, uuid.UUID, uuid.UUID) error { forgotMembers++; return nil })

	people := make([]meshdb.User, 2)
	for i, name := range []string{"ravi", "mia"} {
		people[i] = meshdb.User{Username: name, Email: name + "@example.com", Kind: meshdb.UserHuman}
		require.NoError(t, gdb.Create(&people[i]).Error)
		_, err := svcs.Orgs.AddMember(ctx, orgID, service.AddMemberInput{Email: people[i].Email, Role: meshdb.RoleMember})
		require.NoError(t, err)
	}
	shop := meshdb.Project{OrganizationID: orgID, Name: "shop", Slug: "shop"}
	require.NoError(t, gdb.Create(&shop).Error)
	web := meshdb.Service{ProjectID: shop.ID, Name: "web", Slug: "web"}
	require.NoError(t, gdb.Create(&web).Error)

	source := "crew:" + crew.String()
	for _, p := range people {
		require.NoError(t, gdb.Create(&meshdb.ResourcePermission{OrganizationID: orgID, UserID: p.ID, ResourceType: meshdb.ResourceService,
			ResourceID: web.ID, Action: meshdb.ActionView, Source: source}).Error)
	}
	ravi := people[0]

	// Every check reads it.
	require.NoError(t, svcs.Permissions.CheckAccess(ctx, orgID, ravi.ID, web.ID, meshdb.ResourceService, meshdb.ActionView, &shop.ID))
	visible, _, err := svcs.Permissions.VisibleProjectIDs(ctx, orgID, ravi.ID)
	require.NoError(t, err)
	require.True(t, visible[shop.ID], "a derived grant on a service shows its project")

	// A direct grant of the same action sits beside it, and revoking the
	// direct one leaves the derived one.
	require.NoError(t, svcs.Permissions.Grant(ctx, orgID, ravi.ID, web.ID, meshdb.ResourceService, meshdb.ActionView))
	var n int64
	gdb.Model(&meshdb.ResourcePermission{}).Where("user_id = ?", ravi.ID).Count(&n)
	require.EqualValues(t, 2, n)
	require.NoError(t, svcs.Permissions.Revoke(ctx, orgID, ravi.ID, web.ID, meshdb.ResourceService, meshdb.ActionView))
	require.NoError(t, svcs.Permissions.CheckAccess(ctx, orgID, ravi.ID, web.ID, meshdb.ResourceService, meshdb.ActionView, &shop.ID),
		"revoking the direct grant left the derived one")

	// Lists say where it comes from.
	mine, err := svcs.Permissions.ListForUser(ctx, orgID, ravi.ID)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.NotNil(t, mine[0].Via)
	require.Equal(t, service.GrantVia{Kind: "crew", ID: crew.String(), Label: "crew", Name: "Platform", Link: "/crews/" + crew.String()}, *mine[0].Via)
	onWeb, err := svcs.Permissions.ListForResource(ctx, orgID, meshdb.ResourceService, web.ID)
	require.NoError(t, err)
	require.Len(t, onWeb, 2)
	require.Equal(t, "Platform", onWeb[0].Via.Name)

	// The Access page shows one line for the source, not one per person.
	rules, err := svcs.MeshAccess.Rules(ctx, orgID)
	require.NoError(t, err)
	var lines []service.AccessRule
	for _, r := range rules {
		if r.From.Kind == "crew" {
			lines = append(lines, r)
		}
	}
	require.Len(t, lines, 1)
	require.Equal(t, "Platform", lines[0].From.Name)
	require.Equal(t, 2, lines[0].From.People)
	require.Equal(t, []string{"view"}, lines[0].Actions)

	// A person leaving takes their derived grants, and tells the edition.
	require.NoError(t, svcs.Orgs.RemoveMember(ctx, orgID, people[1].ID))
	require.Equal(t, 1, forgotMembers)
	gdb.Model(&meshdb.ResourcePermission{}).Where("user_id = ?", people[1].ID).Count(&n)
	require.Zero(t, n)

	// The resource going takes every grant on it, and tells the edition.
	require.NoError(t, svcs.Workloads.Delete(ctx, web.ID))
	require.GreaterOrEqual(t, forgotResources, 1)
	gdb.Model(&meshdb.ResourcePermission{}).Where("resource_id = ?", web.ID).Count(&n)
	require.Zero(t, n)
}
