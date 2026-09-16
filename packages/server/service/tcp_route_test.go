package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type tcpEnv struct {
	svcs    *service.Services
	gdb     *gorm.DB
	org     meshdb.Organization
	project *meshdb.Project
}

func newTCPEnv(t *testing.T) tcpEnv {
	t.Helper()
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "tcp", Slug: "tcp"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "tcp", "tcp")
	require.NoError(t, err)

	// A route resolves through the gateway node, so one has to be online.
	require.NoError(t, gdb.Create(&meshdb.Node{
		OrganizationID: org.ID, Name: "gateway", TailscaleIP: "100.64.0.1",
		K3sRole: meshdb.K3sRoleServer, Status: "online",
	}).Error)

	return tcpEnv{svcs: svcs, gdb: gdb, org: org, project: project}
}

func (e tcpEnv) database(t *testing.T, name string) *meshdb.Service {
	t.Helper()
	database, err := e.svcs.Workloads.Create(context.Background(), e.project.ID, service.CreateWorkloadInput{
		Name: name, Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)
	return database
}

func (e tcpEnv) create(port int, in service.CreateTCPRouteInput) (*meshdb.TCPRoute, error) {
	in.OrgID, in.ProjectID, in.GatewayPort = e.org.ID, e.project.ID, port
	return e.svcs.TCPRoutes.Create(context.Background(), in)
}

// A database that nothing has published has no NodePort to forward to, so the
// route would point at a port the cluster never opened. The message says which
// setting is missing rather than reporting an empty target.
func TestTCPRouteNeedsAPublishedDatabase(t *testing.T) {
	e := newTCPEnv(t)
	database := e.database(t, "Primary DB")

	_, err := e.create(5432, service.CreateTCPRouteInput{ServiceID: &database.ID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mesh access")
}

func TestTCPRouteToADatabase(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	database := e.database(t, "Primary DB")

	on := true
	_, err := e.svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{MeshExposed: &on})
	require.NoError(t, err)
	// Without a cluster nothing assigns a port, so stand in for what a provision
	// would have recorded.
	require.NoError(t, e.gdb.Model(&meshdb.DatabaseConfig{}).
		Where("service_id = ?", database.ID).Update("node_port", 31432).Error)

	route, err := e.create(5432, service.CreateTCPRouteInput{ServiceID: &database.ID})
	require.NoError(t, err)
	assert.Equal(t, "100.64.0.1", route.TargetIP, "a route forwards through the gateway's mesh address")
	assert.Equal(t, 31432, route.TargetPort)
	assert.Equal(t, meshdb.TCPRoutePending, route.Status, "until the gateway reports it is listening")
}

// The gateway's own ports would take the console, the mesh or the cluster off
// the air, and the failure would look like a broken machine rather than a route.
func TestTCPRouteRefusesTheGatewaysOwnPorts(t *testing.T) {
	e := newTCPEnv(t)
	database := e.database(t, "Primary DB")

	for _, port := range []int{22, 443, 4000, 6443, 8081} {
		_, err := e.create(port, service.CreateTCPRouteInput{ServiceID: &database.ID})
		require.Error(t, err, "port %d", port)
		assert.Contains(t, err.Error(), "gateway's own")
	}
}

func TestTCPRoutePortIsTakenOnlyOnce(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	database := e.database(t, "Primary DB")

	on := true
	_, err := e.svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{MeshExposed: &on})
	require.NoError(t, err)
	require.NoError(t, e.gdb.Model(&meshdb.DatabaseConfig{}).
		Where("service_id = ?", database.ID).Update("node_port", 31432).Error)

	_, err = e.create(5432, service.CreateTCPRouteInput{ServiceID: &database.ID})
	require.NoError(t, err)

	_, err = e.create(5432, service.CreateTCPRouteInput{ServiceID: &database.ID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already routed")
}

// "Only from my laptop" is the common case, so a bare address is accepted and
// stored as the range it means.
func TestTCPRouteAllowList(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	database := e.database(t, "Primary DB")

	on := true
	_, err := e.svcs.Workloads.UpdateDatabaseConfig(ctx, database.ID, service.UpdateDatabaseConfigInput{MeshExposed: &on})
	require.NoError(t, err)
	require.NoError(t, e.gdb.Model(&meshdb.DatabaseConfig{}).
		Where("service_id = ?", database.ID).Update("node_port", 31432).Error)

	route, err := e.create(5432, service.CreateTCPRouteInput{
		ServiceID: &database.ID, AllowedCIDRs: []string{"203.0.113.7", "10.0.0.0/8"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.7/32", "10.0.0.0/8"}, []string(route.AllowedCIDRs))

	_, err = e.create(5433, service.CreateTCPRouteInput{
		ServiceID: &database.ID, AllowedCIDRs: []string{"not-an-address"},
	})
	require.Error(t, err)
}

// An HTTP port has a domain route, which gives it a hostname and TLS. Routing
// it as raw TCP would quietly bypass both.
func TestTCPRouteRefusesAnHTTPPort(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)

	app, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "web", Image: "nginx",
		Ports: []service.PortInput{{Name: "http", Port: 80, IsHTTP: true, IsPrimary: true, IsPublic: true}},
	})
	require.NoError(t, err)

	_, err = e.create(8080, service.CreateTCPRouteInput{ServiceID: &app.ID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain route")
}
