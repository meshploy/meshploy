package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type primaryEnv struct {
	svc      *service.Services
	gdb      *gorm.DB
	org      uuid.UUID
	host     string
	old, new *meshdb.Domain
}

// An install on old.test with a second verified base domain, new.test.
func newPrimaryEnv(t *testing.T) primaryEnv {
	t.Helper()
	ctx := context.Background()
	gdb := newTestDB(t)
	host := t.TempDir()
	svc := service.New(gdb, &config.Config{
		Domain: "old.test", PublicIP: "203.0.113.10", GatewayIP: "100.64.0.1", HostDir: host,
	})
	org := &meshdb.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org.ID, "old.test", meshdb.DNSModeDelegation))
	list, err := svc.Domains.List(ctx, org.ID)
	require.NoError(t, err)
	next, err := svc.Domains.Create(ctx, org.ID, "new.test", meshdb.DNSModeDelegation)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(next).Update("verified", true).Error)
	next.Verified = true
	return primaryEnv{svc: svc, gdb: gdb, org: org.ID, host: host, old: &list[0], new: next}
}

// Moving the primary moves a pointer, not the platform: the old primary keeps
// serving its console and headscale names, because somebody is using that
// console and workers joined through that headscale.
func TestMakingADomainPrimaryKeepsTheOldOneServing(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)

	d, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)
	assert.True(t, d.IsPrimary)

	old, err := e.svc.Domains.Get(ctx, e.old.ID)
	require.NoError(t, err)
	assert.False(t, old.IsPrimary)
	assert.True(t, old.FormerPrimary)

	require.NoError(t, e.svc.System.PublishEdge(ctx, e.org, uuid.New()))
	snap, err := hostagent.ReadDesiredEdgeSnapshot(e.host)
	require.NoError(t, err)
	assert.Equal(t, "new.test", snap.Primary().BaseDomain)
	for _, dom := range snap.Domains {
		if dom.BaseDomain == "old.test" {
			assert.True(t, dom.Platform(), "the old primary's platform names must keep serving")
		}
	}
	assert.Equal(t, "mesh.old.test", snap.MeshDomain, "node names must not move with the primary")

	// And back again: a domain that becomes primary once more is not also a
	// former primary.
	_, err = e.svc.Domains.SetPrimary(ctx, e.old.ID)
	require.NoError(t, err)
	old, _ = e.svc.Domains.Get(ctx, e.old.ID)
	assert.True(t, old.IsPrimary)
	assert.False(t, old.FormerPrimary)
}

func TestOnlyAServingDomainCanBecomePrimary(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)

	unverified, err := e.svc.Domains.Create(ctx, e.org, "pending.test", "")
	require.NoError(t, err)
	_, err = e.svc.Domains.SetPrimary(ctx, unverified.ID)
	assert.ErrorContains(t, err, "verify this domain first")

	_, err = e.svc.Domains.StartRetiring(ctx, e.new.ID)
	require.NoError(t, err)
	_, err = e.svc.Domains.SetPrimary(ctx, e.new.ID)
	assert.ErrorContains(t, err, "being retired")
}

// New machines join through the primary's headscale name, which is recorded,
// and a former primary cannot go while machines still join through it.
func TestAFormerPrimaryStaysWhileNodesReachTheMeshThroughIt(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)

	// Joined before the switch - and one before the URL was recorded at all,
	// which is taken to be the install domain.
	early, err := e.svc.Nodes.Register(ctx, e.org, "early", "100.64.0.2")
	require.NoError(t, err)
	assert.Equal(t, "https://headscale.old.test", early.ControlURL)
	legacy, err := e.svc.Nodes.Register(ctx, e.org, "legacy", "100.64.0.3")
	require.NoError(t, err)
	require.NoError(t, e.gdb.Model(legacy).Update("control_url", "").Error)
	_, err = e.svc.Nodes.Register(ctx, e.org, "gateway", "100.64.0.1", meshdb.K3sRoleServer)
	require.NoError(t, err)

	_, err = e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)

	late, err := e.svc.Nodes.Register(ctx, e.org, "late", "100.64.0.4")
	require.NoError(t, err)
	assert.Equal(t, "https://headscale.new.test", late.ControlURL, "a node joining now uses the primary")

	old, _ := e.svc.Domains.Get(ctx, e.old.ID)
	on, err := e.svc.Domains.NodesOnDomain(ctx, e.org, old)
	require.NoError(t, err)
	names := []string{}
	for _, n := range on {
		names = append(names, n.Name)
	}
	assert.ElementsMatch(t, []string{"early", "legacy"}, names, "the gateway uses loopback, and late joined elsewhere")

	_, err = e.svc.Domains.StartRetiring(ctx, e.old.ID)
	require.NoError(t, err)
	err = e.svc.Domains.Delete(ctx, e.old.ID)
	assert.ErrorContains(t, err, "2 node(s) still reach the mesh through headscale.old.test")

	for _, n := range on {
		moved, err := e.svc.Domains.MarkNodeMoved(ctx, e.org, n.ID)
		require.NoError(t, err)
		assert.Equal(t, "https://headscale.new.test", moved.ControlURL)
	}
	require.NoError(t, e.svc.Domains.Delete(ctx, e.old.ID))
}

// A domain that was never primary never had anything join through it.
func TestAnOrdinaryDomainHasNoNodesOnIt(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	_, err := e.svc.Nodes.Register(ctx, e.org, "worker", "100.64.0.2")
	require.NoError(t, err)
	on, err := e.svc.Domains.NodesOnDomain(ctx, e.org, e.new)
	require.NoError(t, err)
	assert.Empty(t, on)
}

// What a notification links to, what an unauthenticated install script names,
// and where a provider is sent: the primary's platform addresses, which move
// with it. FRONTEND_URL and API_BASE_URL are written once at install.
func TestPlatformAddressesFollowThePrimary(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	assert.Equal(t, "https://console.old.test", e.svc.Domains.GatewayPlatformURL(ctx, "console"))
	assert.Equal(t, "https://api.old.test", e.svc.Domains.PlatformURL(ctx, e.org, "api"))

	_, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://console.new.test", e.svc.Domains.GatewayPlatformURL(ctx, "console"))
	assert.Equal(t, "https://api.new.test", e.svc.Domains.PlatformURL(ctx, e.org, "api"))

	assert.True(t, e.svc.Domains.ServesPlatform(ctx, "old.test"), "a former primary still serves its names")
	assert.True(t, e.svc.Domains.ServesPlatform(ctx, "new.test"))
	assert.False(t, e.svc.Domains.ServesPlatform(ctx, "evil.example"))
}
