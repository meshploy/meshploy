package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The proxy serves a.my-app.acme.dev from a route for *.my-app.acme.dev, so the
// certificate check must agree, or a wildcard route is routed and never gets a
// certificate. One label up only, as the proxy matches, and only while the
// wildcard route is published.
func TestAWildcardRouteCoversTheNamesUnderIt(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	org := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, org, "p", "p")
	require.NoError(t, err)
	wild := meshdb.Route{OrganizationID: org, ProjectID: project.ID, Zone: meshdb.RouteZonePublic,
		Subdomain: "*.my-app", Hostname: "*.my-app.acme.dev"}
	require.NoError(t, gdb.Create(&wild).Error)

	assert.True(t, svcs.Routes.HasRoute(ctx, "tenant1.my-app.acme.dev"))
	assert.True(t, svcs.Routes.HasRoute(ctx, "*.my-app.acme.dev"))
	assert.False(t, svcs.Routes.HasRoute(ctx, "a.b.my-app.acme.dev"), "one label up only, as the proxy matches")
	assert.False(t, svcs.Routes.HasRoute(ctx, "my-app.acme.dev"), "the wildcard does not cover its own parent")
	assert.False(t, svcs.Routes.HasRoute(ctx, "other.acme.dev"))

	require.NoError(t, gdb.Model(&wild).Update("published", false).Error)
	assert.False(t, svcs.Routes.HasRoute(ctx, "tenant1.my-app.acme.dev"), "a paused wildcard certifies nothing")
}
