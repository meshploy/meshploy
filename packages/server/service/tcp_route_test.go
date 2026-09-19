package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
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

// The console checks what it can see before suggesting a hostname, but two
// people creating the same one at once still meet at the unique index. The
// message has to name the hostname rather than the constraint.
func TestDuplicateHostnameIsReportedAsAConflict(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)

	domain := meshdb.Domain{
		OrganizationID: e.org.ID, BaseDomain: "example.com", Verified: true,
		InternalSubdomain: "internal", PreviewSubdomain: "preview",
	}
	require.NoError(t, e.gdb.Create(&domain).Error)

	in := service.CreateRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID,
		DomainID: &domain.ID, Zone: meshdb.RouteZonePublic, Subdomain: "shop",
	}
	_, err := e.svcs.Routes.Create(ctx, in)
	require.NoError(t, err)

	_, err = e.svcs.Routes.Create(ctx, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shop.example.com")
	assert.Contains(t, err.Error(), "already routed")
}

// ── Address targets and zones ────────────────────────────────────────────────

// The proxy runs with the gateway's own network namespace, so it can reach a
// container bound to loopback - something no service or node target can name.
func TestTCPRouteToAnAddress(t *testing.T) {
	e := newTCPEnv(t)

	route, err := e.create(8080, service.CreateTCPRouteInput{TargetIP: "127.0.0.1", TargetPort: 3580})
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", route.TargetIP, "an address is taken as written")
	assert.Equal(t, 3580, route.TargetPort)
	assert.Equal(t, meshdb.TCPZonePublic, route.Zone, "a route with no zone is public, as every route was")
}

func TestTCPRouteRefusesAnAddressThatIsNotOne(t *testing.T) {
	e := newTCPEnv(t)

	_, err := e.create(8080, service.CreateTCPRouteInput{TargetIP: "localhost", TargetPort: 3580})
	require.Error(t, err, "a hostname resolves somewhere else at some other time; an address does not")
	_, err = e.create(8081, service.CreateTCPRouteInput{TargetIP: "127.0.0.1", TargetPort: 0})
	require.Error(t, err)
}

// Off the public interface the listening port may be the target's: the
// addresses differ, so the numbers need not.
func TestAMeshRouteTakesTheTargetsPort(t *testing.T) {
	e := newTCPEnv(t)

	route, err := e.svcs.TCPRoutes.Create(context.Background(), service.CreateTCPRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID,
		Zone:     meshdb.TCPZoneMesh,
		TargetIP: "127.0.0.1", TargetPort: 3580,
	})
	require.NoError(t, err)
	assert.Equal(t, 3580, route.GatewayPort, "the mesh listener defaults to the target's port")
	assert.Equal(t, meshdb.TCPZoneMesh, route.Zone)
}

// 0.0.0.0:5432 and 100.64.0.1:5432 collide, and a route that fails to bind
// months later because of a zone is worse than a refused form.
func TestPortsStayUniqueAcrossZones(t *testing.T) {
	e := newTCPEnv(t)

	_, err := e.create(5555, service.CreateTCPRouteInput{TargetIP: "127.0.0.1", TargetPort: 3580})
	require.NoError(t, err)

	_, err = e.svcs.TCPRoutes.Create(context.Background(), service.CreateTCPRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID, GatewayPort: 5555,
		Zone:     meshdb.TCPZoneLocal,
		TargetIP: "127.0.0.1", TargetPort: 3581,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already routed")
}

func TestAnUnknownZoneIsRefused(t *testing.T) {
	e := newTCPEnv(t)

	_, err := e.create(5556, service.CreateTCPRouteInput{
		Zone: "internal", TargetIP: "127.0.0.1", TargetPort: 3580,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "public, mesh or local")
}

// Authorising a route says nothing about what it may point at. Before this,
// resolving a target looked the service or node up by id alone, so anyone who
// could edit one route could name any id in the database - another org's
// service included - and have the gateway forward to it.
func TestATargetFromAnotherOrgIsNotFound(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)

	// A second org with its own published database and its own node.
	other := meshdb.Organization{Name: "other", Slug: "other"}
	require.NoError(t, e.gdb.Create(&other).Error)
	otherProject, err := e.svcs.Projects.Create(ctx, other.ID, "other", "other")
	require.NoError(t, err)
	otherDB, err := e.svcs.Workloads.Create(ctx, otherProject.ID, service.CreateWorkloadInput{
		Name: "Their DB", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)
	on := true
	_, err = e.svcs.Workloads.UpdateDatabaseConfig(ctx, otherDB.ID, service.UpdateDatabaseConfigInput{MeshExposed: &on})
	require.NoError(t, err)
	require.NoError(t, e.gdb.Model(&meshdb.DatabaseConfig{}).
		Where("service_id = ?", otherDB.ID).Update("node_port", 31999).Error)

	otherNode := meshdb.Node{OrganizationID: other.ID, Name: "theirs", TailscaleIP: "100.64.9.9", Status: "online"}
	require.NoError(t, e.gdb.Create(&otherNode).Error)

	_, err = e.create(15432, service.CreateTCPRouteInput{ServiceID: &otherDB.ID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")

	_, err = e.create(15433, service.CreateTCPRouteInput{NodeID: &otherNode.ID, NodePort: 8080})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node not found")
}

// Discovery reads the routes an org already has, to mark an endpoint as
// decided. The query runs against the real schema here, which is the only place
// a wrong column name shows up.
func TestDiscoveryReadsExistingRoutesWithoutAHostAgent(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)

	var gateway meshdb.Node
	require.NoError(t, e.gdb.First(&gateway, "name = ?", "gateway").Error)
	_, err := e.svcs.Routes.Create(ctx, service.CreateRouteInput{
		OrgID: e.org.ID, ProjectID: e.project.ID, Hostname: "outside.example.com",
		Targets: []service.TargetInput{{Path: "/", TargetIP: "127.0.0.1", TargetPort: 3001}},
	})
	require.NoError(t, err)
	_, err = e.create(15999, service.CreateTCPRouteInput{TargetIP: "127.0.0.1", TargetPort: 9000})
	require.NoError(t, err)

	got, err := e.svcs.System.GetDiscovery(ctx, e.org.ID)
	require.NoError(t, err)
	// No host directory is configured in tests, so nothing reports and the
	// gateway is named as silent rather than left out.
	assert.Empty(t, got.Nodes)
	require.Len(t, got.Silent, 1)
	assert.Equal(t, "gateway", got.Silent[0].Name)
}

// Ignoring an endpoint is how the list shrinks: sshd and node_exporter are
// correct as they are, and saying so is what makes a new endpoint visible among
// the ones already decided about.
func TestIgnoringAnEndpoint(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	var gateway meshdb.Node
	require.NoError(t, e.gdb.First(&gateway, "name = ?", "gateway").Error)
	me := uuid.New()

	row, err := e.svcs.System.IgnoreEndpoint(ctx, e.org.ID, gateway.ID, me, "0.0.0.0", 22, "sshd, on purpose")
	require.NoError(t, err)
	assert.Equal(t, "sshd, on purpose", row.Note)

	// Twice is the same decision with a new reason, not a second row.
	again, err := e.svcs.System.IgnoreEndpoint(ctx, e.org.ID, gateway.ID, me, "0.0.0.0", 22, "still on purpose")
	require.NoError(t, err)
	assert.Equal(t, row.ID, again.ID)
	assert.Equal(t, "still on purpose", again.Note)

	var count int64
	require.NoError(t, e.gdb.Model(&meshdb.IgnoredEndpoint{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	require.NoError(t, e.svcs.System.UnignoreEndpoint(ctx, e.org.ID, row.ID))
	require.NoError(t, e.gdb.Model(&meshdb.IgnoredEndpoint{}).Count(&count).Error)
	assert.EqualValues(t, 0, count)

	// Gone already is a 404, not a silent success.
	require.Error(t, e.svcs.System.UnignoreEndpoint(ctx, e.org.ID, row.ID))
}

// A node from another organisation is not one you may decide about.
func TestIgnoringAnEndpointOnAnotherOrgsNode(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)

	other := meshdb.Organization{Name: "other-ignore", Slug: "other-ignore"}
	require.NoError(t, e.gdb.Create(&other).Error)
	theirs := meshdb.Node{OrganizationID: other.ID, Name: "theirs", TailscaleIP: "100.64.7.7"}
	require.NoError(t, e.gdb.Create(&theirs).Error)

	_, err := e.svcs.System.IgnoreEndpoint(ctx, e.org.ID, theirs.ID, uuid.New(), "0.0.0.0", 22, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node not found")
}

// An address and a port is what an endpoint is; anything else is a mistake
// worth naming rather than a row nothing will ever match.
func TestIgnoringNeedsAnAddressAndAPort(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	var gateway meshdb.Node
	require.NoError(t, e.gdb.First(&gateway, "name = ?", "gateway").Error)

	_, err := e.svcs.System.IgnoreEndpoint(ctx, e.org.ID, gateway.ID, uuid.New(), "", 22, "")
	require.Error(t, err)
	_, err = e.svcs.System.IgnoreEndpoint(ctx, e.org.ID, gateway.ID, uuid.New(), "0.0.0.0", 0, "")
	require.Error(t, err)
}
