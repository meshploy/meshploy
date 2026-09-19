package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (e tcpEnv) httpRoute(t *testing.T, hostname string, paused bool) *meshdb.Route {
	t.Helper()
	var gateway meshdb.Node
	require.NoError(t, e.gdb.First(&gateway, "name = ?", "gateway").Error)
	port := 8080
	route, err := e.svcs.Routes.Create(context.Background(), service.CreateRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID, Hostname: hostname, Paused: paused,
		Targets: []service.TargetInput{{Path: "/", NodeID: &gateway.ID, Port: port}},
	})
	require.NoError(t, err)
	return route
}

// A route is live by default, as creating one always was. Paused, it keeps its
// targets but no certificate is authorised for it, and publishing brings it
// back, recording who did.
func TestRoutePublishAndPause(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	me := uuid.New()

	live := e.httpRoute(t, "live.example.com", false)
	assert.True(t, live.Published)
	require.NoError(t, e.gdb.Model(live).Update("custom_domain_verified", true).Error)
	assert.True(t, e.svcs.Routes.HasRoute(ctx, "live.example.com"))
	assert.True(t, e.svcs.Routes.IsCustomDomainVerified(ctx, "live.example.com"))

	paused, err := e.svcs.Routes.SetPublished(ctx, live.ID, e.project.ID, false, me)
	require.NoError(t, err)
	assert.False(t, paused.Published)
	assert.Len(t, paused.Targets, 1, "pausing keeps the targets")
	require.NotNil(t, paused.PublishedChangedBy)
	assert.Equal(t, me, *paused.PublishedChangedBy)
	assert.False(t, e.svcs.Routes.HasRoute(ctx, "live.example.com"), "a paused route gets no certificate")
	assert.False(t, e.svcs.Routes.IsCustomDomainVerified(ctx, "live.example.com"))

	back, err := e.svcs.Routes.SetPublished(ctx, live.ID, e.project.ID, true, me)
	require.NoError(t, err)
	assert.True(t, back.Published)
	assert.True(t, e.svcs.Routes.HasRoute(ctx, "live.example.com"))

	// Created paused, it is stored paused: GORM would otherwise drop the false
	// and write the column default.
	prepared := e.httpRoute(t, "prepared.example.com", true)
	var stored meshdb.Route
	require.NoError(t, e.gdb.First(&stored, "id = ?", prepared.ID).Error)
	assert.False(t, stored.Published)
	assert.False(t, e.svcs.Routes.HasRoute(ctx, "prepared.example.com"))

	// Another project cannot publish it.
	other, err := e.svcs.Projects.Create(ctx, e.org.ID, "other", "other")
	require.NoError(t, err)
	_, err = e.svcs.Routes.SetPublished(ctx, prepared.ID, other.ID, true, me)
	assert.Error(t, err)
}

// Routes that existed before the column was added are live afterwards, so an
// upgrade takes nothing offline.
func TestExistingRoutesArePublishedAfterUpgrade(t *testing.T) {
	e := newTCPEnv(t)
	route := e.httpRoute(t, "old.example.com", false)

	require.NoError(t, e.gdb.Migrator().DropColumn(&meshdb.Route{}, "published"))
	require.NoError(t, meshdb.Migrate(e.gdb))

	var stored meshdb.Route
	require.NoError(t, e.gdb.First(&stored, "id = ?", route.ID).Error)
	assert.True(t, stored.Published)
}

// A paused TCP route keeps its port reserved and reports itself paused;
// publishing puts it back to pending until the gateway opens the listener.
func TestTCPRoutePublishAndPause(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	var gateway meshdb.Node
	require.NoError(t, e.gdb.First(&gateway, "name = ?", "gateway").Error)

	route, err := e.create(7000, service.CreateTCPRouteInput{NodeID: &gateway.ID, NodePort: 9000, Paused: true})
	require.NoError(t, err)
	assert.False(t, route.Published)
	assert.Equal(t, meshdb.TCPRoutePaused, route.Status)

	_, err = e.create(7000, service.CreateTCPRouteInput{NodeID: &gateway.ID, NodePort: 9001})
	assert.Error(t, err, "a paused route still holds its port")

	published, err := e.svcs.TCPRoutes.SetPublished(ctx, route.ID, e.project.ID, true, uuid.New())
	require.NoError(t, err)
	assert.True(t, published.Published)
	assert.Equal(t, meshdb.TCPRoutePending, published.Status)
}

// The HTTP twin of TestATargetFromAnotherOrgIsNotFound: a route's targets are
// resolved in the route's own organisation, whether the target is a service or
// another route to redirect to.
func TestAnHTTPTargetFromAnotherOrgIsNotFound(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)

	other := meshdb.Organization{Name: "other-http", Slug: "other-http"}
	require.NoError(t, e.gdb.Create(&other).Error)
	otherProject, err := e.svcs.Projects.Create(ctx, other.ID, "other-http", "other-http")
	require.NoError(t, err)
	otherSvc, err := e.svcs.Workloads.Create(ctx, otherProject.ID, service.CreateWorkloadInput{
		Name: "theirs", Type: meshdb.ServiceTypeApplication, Image: "nginx:1.27",
	})
	require.NoError(t, err)
	otherRoute, err := e.svcs.Routes.Create(ctx, service.CreateRouteInput{
		OrgID: other.ID, ProjectID: otherProject.ID, Hostname: "theirs.example.com",
	})
	require.NoError(t, err)

	mine := e.httpRoute(t, "mine.example.com", false)

	_, err = e.svcs.Routes.AddTarget(ctx, mine.ID, service.TargetInput{Path: "/x", ServiceID: &otherSvc.ID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")

	_, err = e.svcs.Routes.AddTarget(ctx, mine.ID, service.TargetInput{Path: "/y", RedirectRouteID: &otherRoute.ID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirect target route not found")
}
