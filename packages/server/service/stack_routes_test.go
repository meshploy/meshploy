package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stack file's x-meshploy.route is made by the apply, kept by every apply
// after it, and removed once the file stops naming it. Routes the file did not
// declare are left alone.
func TestStackDeclaredRoutes(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "router", Email: "router@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "site-routes", "site-routes")
	require.NoError(t, err)
	domain := meshdb.Domain{OrganizationID: org.ID, BaseDomain: "acme.dev", Verified: true, IsPrimary: true,
		InternalSubdomain: "internal", PreviewSubdomain: "preview"}
	require.NoError(t, gdb.Create(&domain).Error)

	spec := func(route string) string {
		return `
services:
  web:
    image: nginx:1.27
    expose: ["8080"]
` + route + `
  admin:
    image: nginx:1.27
    expose: ["9000"]
`
	}
	apply := func(spec string, opts ...service.ApplyOptions) *service.ApplyResult {
		t.Helper()
		r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "site", Spec: spec}, user.ID, opts...)
		require.NoError(t, err)
		return r
	}
	routes := func() []meshdb.Route {
		t.Helper()
		var rs []meshdb.Route
		require.NoError(t, gdb.Preload("Targets").Where("project_id = ?", proj.ID).Order("hostname").Find(&rs).Error)
		return rs
	}
	serviceID := func(name string) uuid.UUID {
		t.Helper()
		var svc meshdb.Service
		require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, name).First(&svc).Error)
		return svc.ID
	}

	// Declared: made at once, waiting for the service's first deploy.
	r := apply(spec("    x-meshploy:\n      route:\n        subdomain: shop"))
	require.Empty(t, r.Errors)
	assert.Equal(t, []string{"shop.acme.dev"}, r.Routes)
	rs := routes()
	require.Len(t, rs, 1)
	assert.True(t, rs[0].StackDeclared)
	assert.NotNil(t, rs[0].StackID)
	assert.False(t, rs[0].Published, "nothing to serve before the service runs")
	assert.True(t, rs[0].AwaitingDeploy, "its first deploy publishes it")
	require.Len(t, rs[0].Targets, 1)
	assert.Equal(t, serviceID("web"), *rs[0].Targets[0].ServiceID)
	first := rs[0].ID

	// Applied again: the same route, nothing made.
	r = apply(spec("    x-meshploy:\n      route:\n        subdomain: shop"))
	require.Empty(t, r.Errors)
	assert.Empty(t, r.Routes)
	assert.Empty(t, r.RoutesDeleted)
	rs = routes()
	require.Len(t, rs, 1)
	assert.Equal(t, first, rs[0].ID)

	// A route made by hand, for a service the file declares none for, is
	// never the apply's to remove.
	_, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: org.ID, ProjectID: proj.ID, DomainID: &domain.ID,
		Zone: meshdb.RouteZonePublic, Subdomain: "admin",
		Targets: []service.TargetInput{{ServiceID: ptrUUID(serviceID("admin")), Port: 9000, AllowUnresolved: true}}, Paused: true})
	require.NoError(t, err)

	// Renamed: the new one made, the old one removed, the hand-made one kept.
	r = apply(spec("    x-meshploy:\n      route:\n        - subdomain: app\n        - subdomain: app2\n          zone: internal"))
	require.Empty(t, r.Errors)
	assert.ElementsMatch(t, []string{"app.acme.dev", "app2.internal.acme.dev"}, r.Routes)
	assert.Equal(t, []string{"shop.acme.dev"}, r.RoutesDeleted)
	var hosts []string
	for _, rt := range routes() {
		hosts = append(hosts, rt.Hostname)
	}
	assert.ElementsMatch(t, []string{"admin.acme.dev", "app.acme.dev", "app2.internal.acme.dev"}, hosts)

	// No longer declared: removed.
	r = apply(spec(""))
	require.Empty(t, r.Errors)
	assert.ElementsMatch(t, []string{"app.acme.dev", "app2.internal.acme.dev"}, r.RoutesDeleted)
	require.Len(t, routes(), 1)

	// Declaring the hand-made route's hostname on the service it serves takes
	// it over rather than making a second.
	r = apply(spec("") + "    x-meshploy:\n      route:\n        subdomain: admin\n")
	require.Empty(t, r.Errors)
	assert.Equal(t, []string{"admin.acme.dev"}, r.Routes)
	rs = routes()
	require.Len(t, rs, 1)
	assert.True(t, rs[0].StackDeclared)
	assert.False(t, rs[0].Published, "it stays paused as its owner left it")

	// Given to another service in the file, the same route follows it.
	r = apply(spec("    x-meshploy:\n      route:\n        subdomain: admin"))
	require.Empty(t, r.Errors)
	assert.Empty(t, r.Routes)
	rs = routes()
	require.Len(t, rs, 1)
	require.Len(t, rs[0].Targets, 1)
	assert.Equal(t, serviceID("web"), *rs[0].Targets[0].ServiceID)

	// A hostname held by a route that is not the stack's, for a service that
	// is not the one declaring it, is a conflict for that route alone.
	other, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "other", Spec: `
services:
  blog:
    image: nginx:1.27
    expose: ["80"]
`}, user.ID)
	require.NoError(t, err)
	require.Empty(t, other.Errors)
	_, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: org.ID, ProjectID: proj.ID, DomainID: &domain.ID,
		Zone: meshdb.RouteZonePublic, Subdomain: "blog",
		Targets: []service.TargetInput{{ServiceID: ptrUUID(serviceID("blog")), Port: 80, AllowUnresolved: true}}, Paused: true})
	require.NoError(t, err)
	r = apply(spec("    x-meshploy:\n      route:\n        - subdomain: admin\n        - subdomain: blog"))
	require.Len(t, r.Errors, 1)
	assert.Contains(t, r.Errors[0], "web: route blog.acme.dev")
}

// Destroying a stack with its routes and applying it again brings its declared
// routes back: what a template's stack lost when its route was made once, by
// the deploy, and never again.
func TestStackDeclaredRoutesComeBackAfterDestroy(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "destroyer", Email: "destroyer@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "umami-apps", "umami-apps")
	require.NoError(t, err)
	require.NoError(t, gdb.Create(&meshdb.Domain{OrganizationID: org.ID, BaseDomain: "acme.dev", Verified: true, IsPrimary: true,
		InternalSubdomain: "internal", PreviewSubdomain: "preview"}).Error)

	spec := `
services:
  umami:
    image: ghcr.io/umami-software/umami:postgresql-v2.19.0
    expose: ["3000"]
    x-meshploy:
      route:
        subdomain: umami
        published: false
`
	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "umami", Spec: spec}, user.ID)
	require.NoError(t, err)
	require.Empty(t, r.Errors)
	var route meshdb.Route
	require.NoError(t, gdb.Where("hostname = ?", "umami.acme.dev").First(&route).Error)
	assert.False(t, route.Published)
	assert.False(t, route.AwaitingDeploy, "published: false is a pause its deploy keeps")

	_, err = svcs.Stacks.Destroy(ctx, r.Stack.ID, service.DestroyOptions{DeleteRoutes: true})
	require.NoError(t, err)
	var n int64
	gdb.Model(&meshdb.Route{}).Where("project_id = ?", proj.ID).Count(&n)
	require.Zero(t, n)

	r, err = svcs.Stacks.Apply(ctx, r.Stack.ID, user.ID, nil)
	require.NoError(t, err)
	require.Empty(t, r.Errors)
	assert.Equal(t, []string{"umami.acme.dev"}, r.Routes)
}

// A route with no domain to go on says so, and the rest of the apply goes on.
func TestStackDeclaredRouteWithoutDomain(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "nodomain", Email: "nodomain@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "bare-routes", "bare-routes")
	require.NoError(t, err)

	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "bare", Spec: `
services:
  web:
    image: nginx:1.27
    expose: ["8080"]
    x-meshploy:
      route:
        subdomain: shop
`}, user.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"web"}, r.Created)
	require.Len(t, r.Errors, 1)
	assert.True(t, strings.Contains(r.Errors[0], "no verified domain"), r.Errors[0])
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
