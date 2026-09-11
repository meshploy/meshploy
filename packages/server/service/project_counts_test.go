package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// The project page's tab counts come from one query per call. It used to count
// a table that no longer exists, fail, and have its error ignored, so every
// project showed zero of everything. Each kind is counted here.
func TestProjectCountsCoverEveryResourceKind(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "counts", Slug: "counts"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "counts", "counts")
	require.NoError(t, err)

	for _, name := range []string{"api", "web"} {
		_, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{Name: name, Image: "nginx:alpine"})
		require.NoError(t, err)
	}
	_, err = svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "primary-db", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
	})
	require.NoError(t, err)
	_, err = svcs.VariableGroups.Create(ctx, service.CreateGroupInput{ProjectID: project.ID, Name: "shared"})
	require.NoError(t, err)

	// Every group the Variables tab lists: the one made here and each
	// service's generated discovery group.
	var groups int64
	require.NoError(t, gdb.Model(&meshdb.VariableGroup{}).Where("project_id = ?", project.ID).Count(&groups).Error)
	require.GreaterOrEqual(t, groups, int64(1))

	got, err := svcs.Projects.GetWithCounts(ctx, project.ID)
	require.NoError(t, err)
	require.Equal(t, 2, got.ServicesCount)
	require.Equal(t, 1, got.DatabasesCount)
	require.Equal(t, int(groups), got.VariablesCount)

	list, err := svcs.Projects.ListWithCounts(ctx, org.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, got.ProjectCounts, list[0].ProjectCounts)
}

// A port's flags are stored as given. With `default:true` on the model an
// explicit false was stored as true, so a database's port became HTTP and
// public, and no port could be kept internal.
func TestServicePortsKeepTheirFlags(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "ports", Slug: "ports"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "ports", "ports")
	require.NoError(t, err)

	ports := func(serviceID any) []meshdb.ServicePort {
		t.Helper()
		var out []meshdb.ServicePort
		require.NoError(t, gdb.Where("service_id = ?", serviceID).Find(&out).Error)
		require.NotEmpty(t, out)
		return out
	}

	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "primary-db", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
	})
	require.NoError(t, err)
	for _, p := range ports(database.ID) {
		require.False(t, p.IsHTTP, "a database port is not HTTP")
		require.False(t, p.IsPublic, "a database port is internal")
	}

	internal, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "worker", Image: "nginx:alpine",
		Ports: []service.PortInput{{Name: "grpc", Port: 9000, IsPrimary: true}},
	})
	require.NoError(t, err)
	p := ports(internal.ID)[0]
	require.False(t, p.IsHTTP)
	require.False(t, p.IsPublic)

	public, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{Name: "site", Image: "nginx:alpine"})
	require.NoError(t, err)
	p = ports(public.ID)[0]
	require.True(t, p.IsHTTP, "the default port is still HTTP")
	require.True(t, p.IsPublic, "and still public")
}
