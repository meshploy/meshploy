package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDatabase(t *testing.T) (*service.Services, *meshdb.Service) {
	t.Helper()
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "access", Slug: "access"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "access", "access")
	require.NoError(t, err)

	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "Primary DB", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)
	return svcs, database
}

// A database is in-cluster only until someone asks otherwise, and the console
// reads that from the config rather than inferring it from a port.
func TestDatabaseIsNotExposedByDefault(t *testing.T) {
	ctx := context.Background()
	svcs, database := newDatabase(t)

	dc, err := svcs.Workloads.GetDatabaseConfig(ctx, database.ID)
	require.NoError(t, err)
	assert.False(t, dc.MeshExposed)
	assert.Zero(t, dc.NodePort)
}

// Without a cluster the record still has to change: an operator toggling this
// on a dev API should not get an error, and the flag must survive.
func TestExposeDatabaseOverTheMesh(t *testing.T) {
	ctx := context.Background()
	svcs, database := newDatabase(t)

	on := true
	dc, err := svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{MeshExposed: &on})
	require.NoError(t, err)
	assert.True(t, dc.MeshExposed)

	off := false
	dc, err = svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{MeshExposed: &off})
	require.NoError(t, err)
	assert.False(t, dc.MeshExposed)
	assert.Zero(t, dc.NodePort, "withdrawing the port must clear the address the console shows")
}

// A port outside the range Kubernetes assigns from is refused here, with a
// message that says the range, rather than reaching the cluster and failing
// with an API error nobody can act on.
func TestDatabasePortMustBeInTheNodePortRange(t *testing.T) {
	ctx := context.Background()
	svcs, database := newDatabase(t)

	on, port := true, 5432
	_, err := svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{
		MeshExposed: &on, NodePort: &port,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "30000")

	port = 31000
	dc, err := svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{
		MeshExposed: &on, NodePort: &port,
	})
	require.NoError(t, err)
	assert.True(t, dc.MeshExposed)
}
