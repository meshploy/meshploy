package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/config"
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
	snap, err := hostagent.ReadDesiredEdgeSnapshot(host)
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
