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

// An existing gateway upgrading into the domains feature: its domain row was
// written before dns_mode existed, and cannot say which mode it was installed
// in. Guessing delegation is wrong for an on-demand gateway - the edge would be
// rendered for an NS delegation nobody made, and certificates would stop
// renewing. The mode comes from the install's DNS_MODE instead.
func TestUpgradingKeepsAnOnDemandGatewayOnDemand(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	org := &meshdb.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	require.NoError(t, gdb.Create(&meshdb.Domain{
		OrganizationID: org.ID, BaseDomain: "meshp.example.in", Verified: true,
		InternalSubdomain: "internal", PreviewSubdomain: "preview",
	}).Error)

	// The table as it was before this release: no dns_mode, no is_primary.
	require.NoError(t, gdb.Exec(`ALTER TABLE domains DROP COLUMN dns_mode`).Error)
	require.NoError(t, gdb.Exec(`DROP INDEX IF EXISTS idx_one_primary_domain_per_org`).Error)
	require.NoError(t, gdb.Exec(`ALTER TABLE domains DROP COLUMN is_primary`).Error)

	// The new release starts: migrations, then the API on an on-demand gateway.
	require.NoError(t, meshdb.Migrate(gdb))
	svc := service.New(gdb, &config.Config{Domain: "meshp.example.in", DNSMode: "ondemand"})

	list, err := svc.Domains.List(ctx, org.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, meshdb.DNSModeOnDemand, list[0].DNSMode, "an on-demand gateway must stay on-demand")
	assert.True(t, list[0].IsPrimary, "the seeded domain is the primary")
}

// A gateway that predates DNS_MODE altogether was installed with delegation.
func TestUpgradingAGatewayWithNoRecordedModeMeansDelegation(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	org := &meshdb.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	require.NoError(t, gdb.Create(&meshdb.Domain{OrganizationID: org.ID, BaseDomain: "old.example.com", Verified: true}).Error)
	require.NoError(t, gdb.Exec(`UPDATE domains SET dns_mode = ''`).Error)

	svc := service.New(gdb, &config.Config{Domain: "old.example.com"})
	list, err := svc.Domains.List(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, meshdb.DNSModeDelegation, list[0].DNSMode)
}

// DNS_MODE records what the installer was told. A mode switched in the console
// since must survive every restart.
func TestAStartDoesNotOverwriteAModeChosenInTheConsole(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	org := &meshdb.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	svc := service.New(gdb, &config.Config{Domain: "a.test", DNSMode: "ondemand"})
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org.ID, "a.test", meshdb.DNSModeOnDemand))
	list, _ := svc.Domains.List(ctx, org.ID)
	_, err := svc.Domains.SetDNSMode(ctx, list[0].ID, meshdb.DNSModeDelegation)
	require.NoError(t, err)

	svc = service.New(gdb, &config.Config{Domain: "a.test", DNSMode: "ondemand"}) // a restart
	list, _ = svc.Domains.List(ctx, org.ID)
	assert.Equal(t, meshdb.DNSModeDelegation, list[0].DNSMode)
}
