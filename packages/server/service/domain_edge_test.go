package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The edge name is edge.<primary domain>, offered for a CNAME only once it
// resolves to the gateway, and no route may take it.
func TestTheGatewaysEdgeName(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{Domain: "acme.test", HostDir: t.TempDir()})

	// No domain row yet: the install's domain names it.
	restore := service.UseEdgeForTest(svc, "203.0.113.10", nil)
	defer restore()
	edge := svc.Domains.Edge(ctx)
	assert.Equal(t, "edge.acme.test", edge.Name)
	assert.Equal(t, "203.0.113.10", edge.PublicIP)
	assert.False(t, edge.Resolves, "a name that does not resolve is not offered")

	org := &meshdb.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org.ID, "acme.test", meshdb.DNSModeOnDemand))

	restore = service.UseEdgeForTest(svc, "203.0.113.10", map[string][]string{"edge.acme.test": {"198.51.100.7"}})
	assert.False(t, svc.Domains.Edge(ctx).Resolves, "a name resolving to another machine is not offered")

	restore = service.UseEdgeForTest(svc, "203.0.113.10", map[string][]string{"edge.acme.test": {"203.0.113.10"}})
	edge = svc.Domains.Edge(ctx)
	assert.True(t, edge.Resolves)
	assert.Equal(t, "edge.acme.test", edge.Name)

	project := &meshdb.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop"}
	require.NoError(t, gdb.Create(project).Error)
	var d meshdb.Domain
	require.NoError(t, gdb.Where("base_domain = ?", "acme.test").First(&d).Error)
	route := func(sub string) error {
		_, err := svc.Routes.Create(ctx, service.CreateRouteInput{
			OrgID: org.ID, ProjectID: project.ID, DomainID: &d.ID, Subdomain: sub, Zone: meshdb.RouteZonePublic,
			Targets: []service.TargetInput{{Path: "/", TargetIP: "100.64.0.2", TargetPort: 8080}},
		})
		return err
	}
	require.NoError(t, route("shop"))
	assert.Error(t, route("edge"), "a route took the edge name")
}
