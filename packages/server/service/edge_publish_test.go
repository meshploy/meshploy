package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/config"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func edgeServer(t *testing.T) (*service.Services, *gorm.DB, uuid.UUID, string) {
	t.Helper()
	gdb := newTestDB(t)
	host := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(host, "inbox"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(host, "state"), 0o755))
	svc := service.New(gdb, &config.Config{
		PublicIP: "203.0.113.10", GatewayIP: "100.64.0.1", HostDir: host,
	})
	org := &db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	return svc, gdb, org.ID, host
}

func desired(t *testing.T, host string) *hostagent.EdgeSnapshot {
	t.Helper()
	snap, err := hostagent.ReadDesiredEdgeSnapshot(hostagent.Local(host))
	require.NoError(t, err)
	return snap
}

// The API can write the domain set and nothing else. Recording it is not the
// same event as the gateway serving it, which is what keeps a database change
// from silently reconfiguring the edge.
func TestPublishEdgeRecordsTheDomainSetInTheInbox(t *testing.T) {
	svc, gdb, org, host := edgeServer(t)
	ctx := context.Background()

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeOnDemand))
	second, err := svc.Domains.Create(ctx, org, "second.test", db.DNSModeDelegation)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", second.ID).Update("verified", true).Error)

	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))

	snap := desired(t, host)
	require.Len(t, snap.Domains, 2)
	assert.Equal(t, "example.com", snap.Domains[0].BaseDomain)
	assert.True(t, snap.Domains[0].Primary)
	assert.Equal(t, "ondemand", snap.Domains[0].DNSMode)
	assert.Equal(t, "second.test", snap.Domains[1].BaseDomain)
	assert.Equal(t, "delegation", snap.Domains[1].DNSMode, "each domain carries its own mode")
	assert.Equal(t, "203.0.113.10", snap.PublicIP)
	assert.Equal(t, "100.64.0.1", snap.MeshIP)
}

// An unverified domain has no DNS pointing here, so a site block for it would
// have Caddy trying for a certificate it cannot get, over and over.
func TestPublishEdgeLeavesOutUnverifiedDomains(t *testing.T) {
	svc, _, org, host := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeDelegation))
	_, err := svc.Domains.Create(ctx, org, "not-yet.test", db.DNSModeDelegation)
	require.NoError(t, err)

	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))
	snap := desired(t, host)
	require.Len(t, snap.Domains, 1)
	assert.Equal(t, "example.com", snap.Domains[0].BaseDomain)
}

// Retiring stops new routes attaching. It does not switch anything off, so the
// domain keeps being served until it is removed.
func TestPublishEdgeKeepsServingARetiringDomain(t *testing.T) {
	svc, gdb, org, host := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeDelegation))
	retiring, err := svc.Domains.Create(ctx, org, "going.test", db.DNSModeDelegation)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", retiring.ID).
		Updates(map[string]any{"verified": true, "retiring_at": "2026-09-23T00:00:00Z"}).Error)

	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))
	snap := desired(t, host)
	require.Len(t, snap.Domains, 2)
}

// Nothing is applied by writing the file. A request is what asks for it, and
// it is a separate, visible event.
func TestPublishEdgeQueuesARequestOnlyWhenTheAgentIsThere(t *testing.T) {
	svc, _, org, host := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeDelegation))

	// No agent reporting in a test environment: the set is still recorded, and
	// the domain change itself must not fail because a host service is down.
	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))
	assert.FileExists(t, filepath.Join(host, hostagent.EdgeDesiredFile))

	entries, err := os.ReadDir(filepath.Join(host, "inbox"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), hostagent.RequestDomainApply,
			"nothing should be queued while the agent is not reporting")
	}
}

// A developer running the API locally has no gateway and no host directory.
func TestPublishEdgeIsSilentWithoutAHostDirectory(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	org := &db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	require.NoError(t, svc.System.PublishEdge(context.Background(), org.ID, uuid.New()))
}

// The generator refuses a set it cannot serve, and so must the API: writing one
// would leave the agent with a file it can only reject.
func TestPublishEdgeRefusesASetThatCannotBeServed(t *testing.T) {
	svc, gdb, org, host := edgeServer(t)
	ctx := context.Background()
	// An organisation with no primary domain: nothing would serve the console
	// or the API, so there is no set to publish.
	_, err := svc.Domains.Create(ctx, org, "example.com", db.DNSModeDelegation)
	require.NoError(t, err)
	require.Error(t, svc.System.PublishEdge(ctx, org, uuid.New()))
	_, statErr := os.Stat(filepath.Join(host, hostagent.EdgeDesiredFile))
	assert.True(t, os.IsNotExist(statErr), "nothing unusable may reach the inbox")

	// Once one of them is verified and primary, the file that is written is
	// readable as a snapshot, not just as JSON.
	require.NoError(t, gdb.Model(&db.Domain{}).Where("organization_id = ?", org).
		Updates(map[string]any{"verified": true, "is_primary": true}).Error)
	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))
	raw, err := os.ReadFile(filepath.Join(host, hostagent.EdgeDesiredFile))
	require.NoError(t, err)
	var snap hostagent.EdgeSnapshot
	require.NoError(t, json.Unmarshal(raw, &snap))
	require.NoError(t, snap.Validate())
}

// Where an edition registers a project prefix, every project's console name is
// published, levels aside, and a project created or deleted publishes again.
// Nothing changes for one whose names are already installed.
func TestProjectConsoleNamesFollowTheProjects(t *testing.T) {
	service.UseProjectConsolePrefixForTest(t, "apps")
	svc, gdb, org, host := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeDelegation))
	shop, err := svc.Projects.Create(ctx, org, "Shop", "shop-"+uuid.NewString()[:6])
	require.NoError(t, err)
	tools, err := svc.Projects.Create(ctx, org, "Tools", "tools-"+uuid.NewString()[:6])
	require.NoError(t, err)
	require.NoError(t, gdb.Create(&db.Project{OrganizationID: org, Name: "Shop", Slug: shop.Slug + "-staging",
		ParentProjectID: &shop.ID, EnvLevel: 1}).Error)
	require.NoError(t, gdb.Create(&db.Project{OrganizationID: org, Name: "Mesh", Slug: "internal"}).Error)

	install := func() {
		t.Helper()
		snap := desired(t, host)
		require.NoError(t, hostagent.WriteEdgeSnapshot(host, *snap))
		require.NoError(t, os.Remove(filepath.Join(host, hostagent.EdgeDesiredFile)))
	}
	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))
	assert.ElementsMatch(t, []string{"apps." + shop.Slug, "apps." + tools.Slug}, desired(t, host).ProjectConsoleNames,
		"a level is reached through its project, and a slug that is the internal label is left out")
	install()

	require.NoError(t, svc.System.PublishProjectNames(ctx, org, uuid.New()))
	_, statErr := os.Stat(filepath.Join(host, hostagent.EdgeDesiredFile))
	assert.True(t, os.IsNotExist(statErr), "names already installed publish nothing")

	require.NoError(t, gdb.Delete(&db.Project{}, "id = ?", tools.ID).Error)
	require.NoError(t, svc.System.PublishProjectNames(ctx, org, uuid.New()))
	assert.Equal(t, []string{"apps." + shop.Slug}, desired(t, host).ProjectConsoleNames)
}

// A route cannot take a name of the shape a project's console is served at,
// whether or not that project exists yet.
func TestARouteCannotTakeAProjectConsoleName(t *testing.T) {
	service.UseProjectConsolePrefixForTest(t, "apps")
	svc, gdb, org, _ := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeDelegation))
	var domain db.Domain
	require.NoError(t, gdb.Where("organization_id = ?", org).First(&domain).Error)
	project, err := svc.Projects.Create(ctx, org, "Shop", "shop-"+uuid.NewString()[:6])
	require.NoError(t, err)

	_, err = svc.Routes.Create(ctx, service.CreateRouteInput{ProjectID: project.ID, DomainID: &domain.ID,
		Subdomain: "apps.later", Zone: db.RouteZonePublic})
	require.ErrorContains(t, err, "reserved")
}

// A domain whose records a provider writes carries the provider's kind and ID
// in the snapshot, and its token goes beside it for the host agent alone. The
// snapshot, which the console reads back from state/, never holds it.
func TestPublishEdgeHandsOverTheDNSProvidersToken(t *testing.T) {
	svc, gdb, org, host := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeOnDemand))
	second, err := svc.Domains.Create(ctx, org, "second.test", db.DNSModeDelegation)
	require.NoError(t, err)
	p := &db.DNSProvider{OrganizationID: org, Name: "Cloudflare", Kind: "cloudflare", Token: "cf-token-value"}
	require.NoError(t, gdb.Create(p).Error)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("organization_id = ?", org).Update("dns_provider_id", p.ID).Error)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", second.ID).Update("verified", true).Error)

	require.NoError(t, svc.System.PublishEdge(ctx, org, uuid.New()))

	snap := desired(t, host)
	require.Len(t, snap.Domains, 2)
	require.NotNil(t, snap.Domains[0].DNSProvider)
	assert.Equal(t, p.ID.String(), snap.Domains[0].DNSProvider.ID)
	assert.Equal(t, "cloudflare", snap.Domains[0].DNSProvider.Kind)
	assert.Nil(t, snap.Domains[1].DNSProvider, "a delegated domain answers its own challenges")

	raw, err := os.ReadFile(filepath.Join(host, hostagent.EdgeDesiredFile))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "cf-token-value")
	creds, err := hostagent.TakeDNSCredentials(host)
	require.NoError(t, err)
	assert.Equal(t, hostagent.DNSCredentials{p.ID.String(): "cf-token-value"}, creds)
}

// Through a cluster agent the edge is another machine: what it should serve
// goes to the host folder the agent carries, says the platform runs
// elsewhere, so the edge renders no Headscale of its own, carries the edge
// node's addresses rather than this API's, and asks for an apply there.
// Without an edge node, or before it has reported its public address, nothing
// is served, and the domain change itself is not what fails.
func TestAnAgentAPIPublishesItsEdgeThroughTheAgent(t *testing.T) {
	gdb := newTestDB(t)
	// Built as an ordinary API and switched after: an agent API registers a
	// namespace hook for the whole process, which later tests would inherit.
	cfg := &config.Config{PublicIP: "203.0.113.10", GatewayIP: "100.64.0.1"}
	svc := service.New(gdb, cfg)
	cfg.ClusterAgent = true
	host := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(host, "state"), 0o755))
	beat, _ := json.Marshal(hostagent.Agent{HeartbeatAt: time.Now().UTC()})
	require.NoError(t, os.WriteFile(filepath.Join(host, "state", hostagent.AgentFile), beat, 0o644))
	service.UseHostFolderForTest(svc, hostagent.Local(host))

	org := &db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org.ID, "example.com", db.DNSModeOnDemand))

	assert.ErrorIs(t, svc.System.PublishEdge(ctx, org.ID, uuid.New()), appk8s.ErrNoClusterAgent)
	edge := &db.Node{OrganizationID: org.ID, Name: "edge", TailscaleIP: "100.64.0.2", ClusterAgentCert: "cert", ClusterAgentPort: 7443}
	require.NoError(t, gdb.Create(edge).Error)
	assert.ErrorIs(t, svc.System.PublishEdge(ctx, org.ID, uuid.New()), service.ErrEdgeNotReported)
	require.NoError(t, gdb.Model(edge).Update("public_ip", "198.51.100.7").Error)

	require.NoError(t, svc.System.PublishPrimaryEdge(ctx))
	snap := desired(t, host)
	assert.True(t, snap.PlatformElsewhere)
	assert.Equal(t, "198.51.100.7", snap.PublicIP)
	assert.Equal(t, "100.64.0.2", snap.MeshIP)
	require.Len(t, snap.Domains, 1)
	pending, err := hostagent.PendingRequests(hostagent.Local(host))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, hostagent.RequestDomainApply, pending[0].Type)
}

// What the edge's install recorded reaches the node holding the agent: its
// public address, once, so a move is seen and the edge published again, and
// whether published ports are bound to the mesh.
func TestTheEdgesFactsReachItsNode(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	host := t.TempDir()
	service.UseAgentNodesForTest(svc, hostagent.Local(host))
	ctx := context.Background()
	org := &db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	edge := &db.Node{OrganizationID: org.ID, Name: "edge", TailscaleIP: "100.64.0.2", ClusterAgentCert: "cert", ClusterAgentPort: 7443}
	require.NoError(t, gdb.Create(edge).Error)

	moved, err := svc.Nodes.SyncEdgeFacts(ctx)
	require.NoError(t, err)
	assert.False(t, moved, "nothing reported yet")

	require.NoError(t, os.MkdirAll(filepath.Join(host, "state"), 0o755))
	facts, _ := json.Marshal(hostagent.Facts{PublicIP: "198.51.100.7", NodePortAddresses: "100.64.0.0/10"})
	require.NoError(t, os.WriteFile(filepath.Join(host, "state", hostagent.FactsFile), facts, 0o644))
	moved, err = svc.Nodes.SyncEdgeFacts(ctx)
	require.NoError(t, err)
	assert.True(t, moved)
	var got db.Node
	require.NoError(t, gdb.First(&got, "id = ?", edge.ID).Error)
	assert.Equal(t, "198.51.100.7", got.PublicIP)
	dc, _ := svc.Workloads.GetDatabaseConfig(ctx, uuid.New())
	assert.True(t, dc.NodePortMeshOnly)

	moved, err = svc.Nodes.SyncEdgeFacts(ctx)
	require.NoError(t, err)
	assert.False(t, moved, "the same address again is no move")
}
