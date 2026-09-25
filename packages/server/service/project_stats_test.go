package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The overview's cards break each count down: services by status, routes by
// kind, jobs by how they run.
func TestAProjectsStatsBreakItsCountsDown(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, _, _, web, _ := newChain(t)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	require.NoError(t, gdb.Model(&web).Update("status", meshdb.ServiceRunning).Error)
	require.NoError(t, gdb.Create(&meshdb.Service{ProjectID: prod, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceFailed}).Error)
	require.NoError(t, gdb.Create(&meshdb.Route{OrganizationID: project.OrganizationID, ProjectID: prod, Zone: meshdb.RouteZonePublic, Hostname: "a.acme.dev", Published: true}).Error)
	paused := meshdb.Route{OrganizationID: project.OrganizationID, ProjectID: prod, Zone: meshdb.RouteZonePublic, Hostname: "b.acme.dev", Published: true}
	require.NoError(t, gdb.Create(&paused).Error)
	require.NoError(t, gdb.Model(&paused).Update("published", false).Error)
	require.NoError(t, gdb.Create(&meshdb.Route{OrganizationID: project.OrganizationID, ProjectID: prod, Zone: meshdb.RouteZoneInternal, Hostname: "c.internal.acme.dev", Published: true}).Error)
	require.NoError(t, gdb.Create(&meshdb.TCPRoute{OrganizationID: project.OrganizationID, ProjectID: prod, GatewayPort: 15432}).Error)
	require.NoError(t, gdb.Create(&meshdb.Job{ProjectID: prod, Name: "nightly", Image: "busybox", Schedule: "0 2 * * *", Status: meshdb.JobStatusFailed}).Error)

	got, err := svcs.Projects.GetWithCounts(ctx, prod)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"running": 1, "failed": 1}, got.Stats["services"])
	assert.Equal(t, map[string]int{"https": 1, "paused": 1, "internal": 1, "tcp": 1}, got.Stats["routes"])
	assert.Equal(t, map[string]int{"scheduled": 1, "failed": 1}, got.Stats["jobs"])
	assert.Equal(t, got.RoutesCount, 4)
}
