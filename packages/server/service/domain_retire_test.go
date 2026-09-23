package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retireEnv is an org with a primary base domain and a second one, both
// verified, and a service-less route on the second.
type retireEnv struct {
	tcpEnv
	primary, second *meshdb.Domain
	gateway         meshdb.Node
}

func newRetireEnv(t *testing.T) retireEnv {
	t.Helper()
	e := retireEnv{tcpEnv: newTCPEnv(t)}
	ctx := context.Background()
	require.NoError(t, e.svcs.Domains.CreateSeeded(ctx, e.org.ID, "primary.test", meshdb.DNSModeDelegation))
	list, err := e.svcs.Domains.List(ctx, e.org.ID)
	require.NoError(t, err)
	e.primary = &list[0]
	e.second, err = e.svcs.Domains.Create(ctx, e.org.ID, "second.test", meshdb.DNSModeOnDemand)
	require.NoError(t, err)
	require.NoError(t, e.gdb.Model(e.second).Update("verified", true).Error)
	e.second.Verified = true
	require.NoError(t, e.gdb.First(&e.gateway, "name = ?", "gateway").Error)
	return e
}

func (e retireEnv) routeOn(t *testing.T, d *meshdb.Domain, zone meshdb.RouteZone, sub string) *meshdb.Route {
	t.Helper()
	r, err := e.svcs.Routes.Create(context.Background(), service.CreateRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID, DomainID: &d.ID, Zone: zone, Subdomain: sub,
		Targets: []service.TargetInput{{Path: "/", NodeID: &e.gateway.ID, Port: 8080}},
	})
	require.NoError(t, err)
	return r
}

// Retiring is a write because it has to be: once the domain takes no new
// routes, the list of what still holds it can only shrink.
func TestARetiringDomainTakesNoNewRoutes(t *testing.T) {
	ctx := context.Background()
	e := newRetireEnv(t)
	e.routeOn(t, e.second, meshdb.RouteZonePublic, "shop")

	d, err := e.svcs.Domains.StartRetiring(ctx, e.second.ID)
	require.NoError(t, err)
	require.NotNil(t, d.RetiringAt)

	_, err = e.svcs.Routes.Create(ctx, service.CreateRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID, DomainID: &e.second.ID, Zone: meshdb.RouteZonePublic, Subdomain: "blog",
		Targets: []service.TargetInput{{Path: "/", NodeID: &e.gateway.ID, Port: 8080}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "being retired")

	// Nothing stops serving: the route already there still answers.
	assert.True(t, e.svcs.Routes.HasRoute(ctx, "shop.second.test"))

	// And the decision is undone by stopping it.
	d, err = e.svcs.Domains.StopRetiring(ctx, e.second.ID)
	require.NoError(t, err)
	assert.Nil(t, d.RetiringAt)
	e.routeOn(t, e.second, meshdb.RouteZonePublic, "blog")
}

func TestThePrimaryAndAnUnverifiedDomainCannotBeRetired(t *testing.T) {
	ctx := context.Background()
	e := newRetireEnv(t)
	_, err := e.svcs.Domains.StartRetiring(ctx, e.primary.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary")

	typo, err := e.svcs.Domains.Create(ctx, e.org.ID, "typo.test", "")
	require.NoError(t, err)
	_, err = e.svcs.Domains.StartRetiring(ctx, typo.ID)
	require.Error(t, err, "nothing was ever served on it - it is removed, not retired")

	// Which is exactly why an unverified one can be removed straight away.
	require.NoError(t, e.svcs.Domains.Delete(ctx, typo.ID))
}

// A domain that has served routes is removed by seeing it through, not by a
// click: retire it, clear it, then remove it.
func TestRemovingAServingDomainNeedsItRetiredAndClear(t *testing.T) {
	ctx := context.Background()
	e := newRetireEnv(t)
	route := e.routeOn(t, e.second, meshdb.RouteZonePublic, "shop")

	err := e.svcs.Domains.Delete(ctx, e.second.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start retiring")

	_, err = e.svcs.Domains.StartRetiring(ctx, e.second.ID)
	require.NoError(t, err)
	err = e.svcs.Domains.Delete(ctx, e.second.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 route")

	_, _, err = e.svcs.Routes.MoveRoute(ctx, service.MoveRouteInput{
		RouteID: route.ID, ProjectID: e.project.ID, DomainID: e.primary.ID,
	})
	require.NoError(t, err)
	require.NoError(t, e.svcs.Domains.Delete(ctx, e.second.ID))
}

func TestMovingARouteKeepsItsSubdomainZoneAndTargets(t *testing.T) {
	ctx := context.Background()
	e := newRetireEnv(t)
	pub := e.routeOn(t, e.second, meshdb.RouteZonePublic, "shop")
	internal := e.routeOn(t, e.second, meshdb.RouteZoneInternal, "grafana")

	moved, redirect, err := e.svcs.Routes.MoveRoute(ctx, service.MoveRouteInput{
		RouteID: pub.ID, ProjectID: e.project.ID, DomainID: e.primary.ID,
	})
	require.NoError(t, err)
	assert.Nil(t, redirect)
	assert.Equal(t, "shop.primary.test", moved.Hostname)
	assert.Equal(t, e.primary.ID, *moved.DomainID)
	assert.True(t, e.svcs.Routes.HasRoute(ctx, "shop.primary.test"))
	assert.False(t, e.svcs.Routes.HasRoute(ctx, "shop.second.test"), "without a redirect the old name is released")

	var targets int64
	require.NoError(t, e.gdb.Model(&meshdb.RouteTarget{}).Where("route_id = ?", pub.ID).Count(&targets).Error)
	assert.EqualValues(t, 1, targets, "the targets move with it")

	moved, _, err = e.svcs.Routes.MoveRoute(ctx, service.MoveRouteInput{
		RouteID: internal.ID, ProjectID: e.project.ID, DomainID: e.primary.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "grafana.internal.primary.test", moved.Hostname, "the zone decides the shape of the name")
}

// Links already out there keep working. The redirect is itself a route on the
// old domain, and holds it until somebody decides the grace period is over.
func TestMovingARouteCanLeaveARedirectBehind(t *testing.T) {
	ctx := context.Background()
	e := newRetireEnv(t)
	route := e.routeOn(t, e.second, meshdb.RouteZonePublic, "shop")
	_, err := e.svcs.Domains.StartRetiring(ctx, e.second.ID)
	require.NoError(t, err)

	moved, redirect, err := e.svcs.Routes.MoveRoute(ctx, service.MoveRouteInput{
		RouteID: route.ID, ProjectID: e.project.ID, DomainID: e.primary.ID, KeepRedirect: true,
	})
	require.NoError(t, err, "a retiring domain may still gain the redirect that winds it down")
	require.NotNil(t, redirect)
	assert.Equal(t, "shop.second.test", redirect.Hostname)
	require.Len(t, redirect.Targets, 1)
	require.NotNil(t, redirect.Targets[0].RedirectRouteID)
	assert.Equal(t, moved.ID, *redirect.Targets[0].RedirectRouteID)
	assert.Equal(t, 301, redirect.Targets[0].RedirectCode)

	rows, err := e.svcs.Domains.RoutesOnDomain(ctx, e.org.ID, e.second.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the redirect still holds the domain")
	assert.Equal(t, "shop.primary.test", rows[0].RedirectsTo, "and the checklist can say what it is")
}

func TestMoveRefusesWhatCannotMove(t *testing.T) {
	ctx := context.Background()
	e := newRetireEnv(t)
	pub := e.routeOn(t, e.second, meshdb.RouteZonePublic, "shop")
	internal := e.routeOn(t, e.second, meshdb.RouteZoneInternal, "grafana")
	custom := e.httpRoute(t, "store.customer.example", false)

	move := func(r *meshdb.Route, to *meshdb.Domain, keep bool) error {
		_, _, err := e.svcs.Routes.MoveRoute(ctx, service.MoveRouteInput{
			RouteID: r.ID, ProjectID: e.project.ID, DomainID: to.ID, KeepRedirect: keep,
		})
		return err
	}

	assert.ErrorContains(t, move(pub, e.second, false), "already on that domain")
	assert.ErrorContains(t, move(custom, e.primary, false), "custom hostname")
	assert.ErrorContains(t, move(internal, e.primary, true), "only a public route")

	// Onto a retiring domain: it would only have to move again.
	_, err := e.svcs.Domains.StartRetiring(ctx, e.second.ID)
	require.NoError(t, err)
	other := e.routeOn(t, e.primary, meshdb.RouteZonePublic, "blog")
	assert.ErrorContains(t, move(other, e.second, false), "being retired itself")

	// And onto a name already taken, named rather than a constraint violation.
	e.routeOn(t, e.primary, meshdb.RouteZonePublic, "shop")
	assert.ErrorContains(t, move(pub, e.primary, false), "shop.primary.test is already routed")
}
