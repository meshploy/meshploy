package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// Renaming a level renames its hostnames with it; the namespace stays.
func TestRenamingALevelRederivesItsHostnames(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, _, _ := newChain(t)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	domain := meshdb.Domain{OrganizationID: project.OrganizationID, BaseDomain: "acme.dev", InternalSubdomain: "internal",
		PreviewSubdomain: "preview", Verified: true, IsPrimary: true, VerifyToken: "t"}
	require.NoError(t, gdb.Create(&domain).Error)
	route, err := svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: project.OrganizationID, ProjectID: s1,
		DomainID: &domain.ID, Zone: meshdb.RouteZonePublic, Subdomain: "app", Paused: true})
	require.NoError(t, err)
	require.Equal(t, "app-staging1.acme.dev", route.Hostname)

	_, err = svcs.Projects.RenameLevel(ctx, prod, "live")
	assert.ErrorIs(t, err, service.ErrRenameProduction)
	_, err = svcs.Projects.RenameLevel(ctx, s1, "staging2")
	assert.ErrorIs(t, err, service.ErrLevelTaken)

	n, err := svcs.Projects.RenameLevel(ctx, s1, "qa")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	var got meshdb.Route
	require.NoError(t, gdb.First(&got, "id = ?", route.ID).Error)
	assert.Equal(t, "app-qa.acme.dev", got.Hostname)
	var level meshdb.Project
	require.NoError(t, gdb.First(&level, "id = ?", s1).Error)
	assert.Equal(t, "qa", level.EnvName)
	assert.Equal(t, "coreline-staging1", level.Slug, "the namespace cannot be renamed, and is not")

	// A production route already ending in -uat means no level may become uat.
	_, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: project.OrganizationID, ProjectID: prod,
		DomainID: &domain.ID, Zone: meshdb.RouteZonePublic, Subdomain: "shop-uat", Paused: true})
	require.NoError(t, err)
	_, err = svcs.Projects.RenameLevel(ctx, s1, "uat")
	assert.ErrorContains(t, err, "already ends in -uat")
}

// A copy into a lower level is a group of its own, which promotes like any
// other, and which a named group absorbs when the service joins it.
func TestACopyIsAGroupOfItsOwnUntilItJoinsOne(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)
	api := meshdb.Service{ProjectID: prod, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication}
	require.NoError(t, gdb.Create(&api).Error)

	_, err := svcs.Promotions.CopyToLevel(ctx, web.ID, prod)
	assert.ErrorIs(t, err, service.ErrNotBelow)

	single, err := svcs.Promotions.CopyToLevel(ctx, web.ID, s2)
	require.NoError(t, err)
	assert.True(t, single.Single)
	assert.Equal(t, "web", single.Name)
	assert.Equal(t, []string{s2.String(), prod.String()}, []string(single.Path), "from the level copied to, up to production")
	var copied meshdb.Service
	require.NoError(t, gdb.First(&copied, "project_id = ? AND name = ?", s2, "web").Error)

	named, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "backend", ServiceIDs: []uuid.UUID{api.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)
	require.NoError(t, svcs.Promotions.AddToGroup(ctx, prod, named.ID, []uuid.UUID{web.ID}))

	var gone int64
	require.NoError(t, gdb.Model(&meshdb.PromotionGroup{}).Where("id = ?", single.ID).Count(&gone).Error)
	assert.Zero(t, gone, "the single-service group was merged away")
	var inS1 meshdb.Service
	require.NoError(t, gdb.First(&inS1, "project_id = ? AND name = ?", s1, "web").Error, "web entered backend's entry level")

	// A service in a named group is not taken by another.
	other, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "other", ServiceIDs: []uuid.UUID{copied.ID}, Path: []uuid.UUID{s2, prod}})
	assert.Nil(t, other)
	assert.ErrorIs(t, err, service.ErrGroupMember)

	// Removing leaves the copies running; the last one out takes the group.
	deleted, err := svcs.Promotions.RemoveFromGroup(ctx, prod, named.ID, web.ID)
	require.NoError(t, err)
	assert.False(t, deleted)
	require.NoError(t, gdb.First(&inS1, "project_id = ? AND name = ?", s1, "web").Error, "copies stay where they are")
	deleted, err = svcs.Promotions.RemoveFromGroup(ctx, prod, named.ID, api.ID)
	require.NoError(t, err)
	assert.True(t, deleted)

	assert.Error(t, svcs.Promotions.RenameGroup(ctx, prod, uuid.New(), "x"), "an unknown group")
}

// Bringing a service down runs what production runs in a lower level.
func TestBringingDownRunsProductionsImageBelow(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, web, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())

	_, err := svcs.Promotions.BringDown(ctx, web.ID, s1)
	assert.ErrorIs(t, err, service.ErrNothingToPromote, "production has never deployed web")

	now := time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Image: "registry/web:v7", DeployedAt: &now}).Error)
	_, err = svcs.Promotions.BringDown(ctx, web.ID, prod)
	assert.ErrorIs(t, err, service.ErrNotBelow)

	dep, err := svcs.Promotions.BringDown(ctx, web.ID, s1)
	require.NoError(t, err)
	assert.Equal(t, "registry/web:v7", dep.Image)
	var target meshdb.Service
	require.NoError(t, gdb.First(&target, "id = ?", dep.ServiceID).Error)
	assert.Equal(t, s1, target.ProjectID, "staging1 got a copy to run it")
}

// Removing a level's copy deletes it and the level's routes to it, leaves
// production alone, and takes it out of the group it builds for there.
func TestRemovingACopyFromALevel(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, _, s2, web, _ := newChain(t)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	domain := meshdb.Domain{OrganizationID: project.OrganizationID, BaseDomain: "acme.dev", InternalSubdomain: "internal",
		PreviewSubdomain: "preview", Verified: true, IsPrimary: true, VerifyToken: "t"}
	require.NoError(t, gdb.Create(&domain).Error)
	live := meshdb.Route{OrganizationID: project.OrganizationID, ProjectID: prod, DomainID: &domain.ID,
		Zone: meshdb.RouteZonePublic, Subdomain: "app", Hostname: "app.acme.dev"}
	require.NoError(t, gdb.Create(&live).Error)
	require.NoError(t, gdb.Create(&meshdb.RouteTarget{RouteID: live.ID, Path: "/", ServiceID: &web.ID, TargetIP: "100.64.0.1", TargetPort: 30002}).Error)

	_, err := svcs.Promotions.RemoveFromLevel(ctx, web.ID)
	assert.ErrorIs(t, err, service.ErrRemoveProduction)

	group, err := svcs.Promotions.CopyToLevel(ctx, web.ID, s2)
	require.NoError(t, err)
	var copied meshdb.Service
	require.NoError(t, gdb.First(&copied, "project_id = ? AND name = ?", s2, "web").Error)

	out, err := svcs.Promotions.RemoveFromLevel(ctx, copied.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, out.RoutesRemoved, "app-staging2 went with it")
	assert.True(t, out.LeftGroup)
	assert.True(t, out.GroupDeleted, "a single-service group goes with its only copy")

	var n int64
	require.NoError(t, gdb.Model(&meshdb.Service{}).Where("id = ?", copied.ID).Count(&n).Error)
	assert.Zero(t, n)
	require.NoError(t, gdb.Model(&meshdb.PromotionGroup{}).Where("id = ?", group.ID).Count(&n).Error)
	assert.Zero(t, n)
	require.NoError(t, gdb.Model(&meshdb.Route{}).Where("id = ?", live.ID).Count(&n).Error)
	assert.Equal(t, int64(1), n, "production's route is untouched")
	require.NoError(t, gdb.Model(&meshdb.Service{}).Where("id = ?", web.ID).Count(&n).Error)
	assert.Equal(t, int64(1), n, "and so is production's web")
}
