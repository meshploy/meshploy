package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A level's map comes in one read: its services and the rest, and who reads
// whose published variables, which the map draws as an edge and a failure
// travels along.
func TestAProjectMapIsOneReadWithWhoReadsWhom(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	api, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{Name: "api", Type: meshdb.ServiceTypeApplication, Image: "api:1"})
	require.NoError(t, err)
	pg := e.database(t, "orders")

	var published meshdb.VariableGroup
	require.NoError(t, e.gdb.First(&published, "service_id = ?", pg.ID).Error)
	require.NoError(t, e.svcs.VariableGroups.Attach(ctx, api.ID, published.ID))

	m, err := e.svcs.ProjectMaps.Get(ctx, e.project.ID)
	require.NoError(t, err)
	assert.Len(t, m.Services, 2)
	assert.Equal(t, []string{pg.ID.String()}, m.Reads[api.ID.String()])
	assert.Empty(t, m.Reads[pg.ID.String()], "its own group is not a read of another's")
	assert.NotNil(t, m.Troubles)
	assert.NotNil(t, m.Hints)
}
