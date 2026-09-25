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

// A route in an environment level carries the level's name in its hostname,
// so app in staging is app-staging and only production has the real names.
func TestRoutesInALevelDeriveTheirHostnames(t *testing.T) {
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
	assert.Equal(t, "app-staging1.acme.dev", route.Hostname)
	assert.Equal(t, "app", route.Subdomain, "the subdomain stays the service's own")

	internal, err := svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: project.OrganizationID, ProjectID: s1,
		DomainID: &domain.ID, Zone: meshdb.RouteZoneInternal, Subdomain: "grafana", Paused: true})
	require.NoError(t, err)
	assert.Equal(t, "grafana-staging1.internal.acme.dev", internal.Hostname)

	_, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: project.OrganizationID, ProjectID: prod,
		DomainID: &domain.ID, Zone: meshdb.RouteZonePublic, Subdomain: "shop-staging1", Paused: true})
	assert.ErrorContains(t, err, "ends like the names the staging1 level derives",
		"production cannot take a name a level would derive")
}

// Copying a service into a level copies its routes under the level's names,
// paused, and its first successful deploy there publishes them.
func TestACopiedServiceBringsItsRoutesAndTheyGoLiveWhenItRuns(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	domain := meshdb.Domain{OrganizationID: project.OrganizationID, BaseDomain: "acme.dev", InternalSubdomain: "internal",
		PreviewSubdomain: "preview", Verified: true, IsPrimary: true, VerifyToken: "t"}
	require.NoError(t, gdb.Create(&domain).Error)

	// Production serves web at app.acme.dev.
	live := meshdb.Route{OrganizationID: project.OrganizationID, ProjectID: prod, DomainID: &domain.ID,
		Zone: meshdb.RouteZonePublic, Subdomain: "app", Hostname: "app.acme.dev"}
	require.NoError(t, gdb.Create(&live).Error)
	require.NoError(t, gdb.Create(&meshdb.RouteTarget{RouteID: live.ID, Path: "/", ServiceID: &web.ID, TargetIP: "100.64.0.1", TargetPort: 30002}).Error)

	_, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "web", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s2, s1, prod}})
	require.NoError(t, err)

	var copied meshdb.Route
	require.NoError(t, gdb.Preload("Targets").First(&copied, "project_id = ?", s2).Error)
	assert.Equal(t, "app-staging2.acme.dev", copied.Hostname)
	assert.False(t, copied.Published, "nothing runs there yet")
	assert.True(t, copied.AwaitingDeploy)
	var webS2 meshdb.Service
	require.NoError(t, gdb.First(&webS2, "project_id = ? AND name = ?", s2, "web").Error)
	require.Len(t, copied.Targets, 1)
	assert.Equal(t, webS2.ID, *copied.Targets[0].ServiceID, "it serves the level's own copy")
	assert.Zero(t, copied.Targets[0].TargetPort)

	// The board links each card to its level's address, the copy's not live yet.
	cardRoutes := func() map[uuid.UUID][]service.BoardRoute {
		board, err := svcs.Promotions.Board(ctx, prod)
		require.NoError(t, err)
		out := map[uuid.UUID][]service.BoardRoute{}
		for _, c := range board.Groups[0].Cells {
			out[c.LevelID] = c.Routes
		}
		return out
	}
	assert.Equal(t, []service.BoardRoute{{Hostname: "app.acme.dev", Live: true}}, cardRoutes()[prod])
	assert.Equal(t, []service.BoardRoute{{Hostname: "app-staging2.acme.dev", Live: false}}, cardRoutes()[s2])

	// The copy runs: a published port, and a gateway to route through.
	require.NoError(t, gdb.Model(&meshdb.ServicePort{}).Where("service_id = ?", webS2.ID).
		Updates(map[string]any{"node_port": 31555, "is_public": true}).Error)
	require.NoError(t, gdb.Create(&meshdb.Node{OrganizationID: project.OrganizationID, Name: "gw", TailscaleIP: "100.64.0.1",
		K3sRole: meshdb.K3sRoleServer, Status: meshdb.NodeOnline}).Error)
	svcs.Routes.PublishAwaitingRoutes(ctx, webS2.ID)

	require.NoError(t, gdb.Preload("Targets").First(&copied, "id = ?", copied.ID).Error)
	assert.True(t, copied.Published)
	assert.False(t, copied.AwaitingDeploy)
	assert.Equal(t, 31555, copied.Targets[0].TargetPort)
	assert.True(t, cardRoutes()[s2][0].Live, "once it runs, the card's link is live")

	// A route an operator paused is not waiting, and stays paused.
	require.NoError(t, gdb.Model(&copied).Update("published", false).Error)
	svcs.Routes.PublishAwaitingRoutes(ctx, webS2.ID)
	require.NoError(t, gdb.First(&copied, "id = ?", copied.ID).Error)
	assert.False(t, copied.Published)
}

// A service can start in a level. Promoting it creates it in production for
// the first time, with its routes under production's real names: app-staging1
// becomes app, paused until production's first deploy of it succeeds.
func TestAServiceBornInALevelGetsProductionNamesWhenPromoted(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, _, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	domain := meshdb.Domain{OrganizationID: project.OrganizationID, BaseDomain: "acme.dev", InternalSubdomain: "internal",
		PreviewSubdomain: "preview", Verified: true, IsPrimary: true, VerifyToken: "t"}
	require.NoError(t, gdb.Create(&domain).Error)

	shop := meshdb.Service{ProjectID: s1, Name: "shop", Slug: "shop", Type: meshdb.ServiceTypeApplication, EnvVars: "MODE=staging"}
	require.NoError(t, gdb.Create(&shop).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: shop.ID, Name: "http", Port: 3000, IsHTTP: true, IsPrimary: true}).Error)
	route, err := svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: project.OrganizationID, ProjectID: s1,
		DomainID: &domain.ID, Zone: meshdb.RouteZonePublic, Subdomain: "shop", Paused: true})
	require.NoError(t, err)
	require.Equal(t, "shop-staging1.acme.dev", route.Hostname)
	require.NoError(t, gdb.Create(&meshdb.RouteTarget{RouteID: route.ID, Path: "/", ServiceID: &shop.ID}).Error)

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "shop", ServiceIDs: []uuid.UUID{shop.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: shop.ID, Status: meshdb.DeploymentSuccess, Image: "registry/shop:v1", DeployedAt: &now}).Error)

	result, err := svcs.Promotions.Promote(ctx, s1, group.ID)
	require.NoError(t, err)
	require.Len(t, result.Promoted, 1)
	assert.True(t, result.Promoted[0].CreatedThere, "production had no shop until now")

	var up meshdb.Service
	require.NoError(t, gdb.First(&up, "project_id = ? AND name = ?", prod, "shop").Error)
	assert.Equal(t, meshdb.EncryptedString("MODE=staging"), up.EnvVars, "its own variables come as they are, staging's values included")

	var copied meshdb.Route
	require.NoError(t, gdb.Preload("Targets").First(&copied, "project_id = ?", prod).Error)
	assert.Equal(t, "shop.acme.dev", copied.Hostname, "production's copy takes the real name")
	assert.True(t, copied.AwaitingDeploy)
	require.Len(t, copied.Targets, 1)
	assert.Equal(t, up.ID, *copied.Targets[0].ServiceID)

	require.Eventually(t, func() bool {
		var d meshdb.Deployment
		return gdb.First(&d, "id = ?", result.Promoted[0].Deployment.ID).Error == nil && d.Status == meshdb.DeploymentSuccess
	}, 10*time.Second, 50*time.Millisecond)
	var staging meshdb.Route
	require.NoError(t, gdb.First(&staging, "id = ?", route.ID).Error)
	assert.Equal(t, "shop-staging1.acme.dev", staging.Hostname, "staging keeps its own name")
}
