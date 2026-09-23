package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func domainOrg(t *testing.T, gdb *gorm.DB) uuid.UUID {
	t.Helper()
	org := &db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(org).Error)
	return org.ID
}

func TestCreateDomainRejectsWhatIsNotADomain(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	org := domainOrg(t, gdb)

	// A base domain has to be delegable and certifiable. A single label is
	// neither, and the rest are the ways people paste one in.
	for _, bad := range []string{"", "localhost", "https://example.com", "example.com:443", "*.example.com", "exa mple.com", "-bad.com"} {
		_, err := svc.Domains.Create(context.Background(), org, bad, "")
		assert.Error(t, err, "%q must be refused", bad)
	}
}

func TestCreateDomainNormalisesAndDefaults(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	org := domainOrg(t, gdb)

	d, err := svc.Domains.Create(context.Background(), org, "  Example.COM.  ", "")
	require.NoError(t, err)
	assert.Equal(t, "example.com", d.BaseDomain, "case and the trailing dot are not part of the name")
	assert.Equal(t, db.DNSModeDelegation, d.DNSMode)

	// Never primary on creation: promoting one moves the platform's own
	// hostnames, which is not a side effect of typing a name.
	assert.False(t, d.IsPrimary)
	assert.False(t, d.Verified)
	assert.NotEmpty(t, d.VerifyToken)
	assert.Nil(t, d.RetiringAt)
}

func TestCreateDomainRefusesADomainAlreadyHeld(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()

	_, err := svc.Domains.Create(ctx, domainOrg(t, gdb), "example.com", db.DNSModeOnDemand)
	require.NoError(t, err)

	// Another org, same name. base_domain is globally unique, which is what
	// stops one org claiming another's.
	_, err = svc.Domains.Create(ctx, domainOrg(t, gdb), "example.com", "")
	require.Error(t, err)
}

func TestCreateDomainRejectsAnUnknownDNSMode(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	_, err := svc.Domains.Create(context.Background(), domainOrg(t, gdb), "example.com", "wildcard")
	require.Error(t, err)
}

// Two base domains on one gateway can be arranged differently. That is the
// whole reason the mode moved off the server and onto the row.
func TestDNSModeIsPerDomain(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	a, err := svc.Domains.Create(ctx, org, "delegated.test", db.DNSModeDelegation)
	require.NoError(t, err)
	b, err := svc.Domains.Create(ctx, org, "ondemand.test", db.DNSModeOnDemand)
	require.NoError(t, err)

	b, err = svc.Domains.SetDNSMode(ctx, b.ID, db.DNSModeDelegation)
	require.NoError(t, err)
	assert.Equal(t, db.DNSModeDelegation, b.DNSMode)

	again, err := svc.Domains.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, db.DNSModeDelegation, again.DNSMode, "one domain's mode is not the other's")

	_, err = svc.Domains.SetDNSMode(ctx, a.ID, "cloudflare")
	require.Error(t, err)
}

func TestOnlyOnePrimaryDomainPerOrg(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "first.test", db.DNSModeDelegation))
	d, err := svc.Domains.Create(ctx, org, "second.test", "")
	require.NoError(t, err)

	// The index is the guarantee, not the service: two people promoting at once
	// still meet here.
	err = gdb.Model(&db.Domain{}).Where("id = ?", d.ID).Update("is_primary", true).Error
	require.Error(t, err, "a second primary must be refused by the database")

	// Another org is unaffected - the index is scoped to the organization.
	other := domainOrg(t, gdb)
	require.NoError(t, svc.Domains.CreateSeeded(ctx, other, "elsewhere.test", db.DNSModeDelegation))
}

func TestSeededDomainIsPrimaryAndCarriesTheInstallMode(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "seeded.test", db.DNSModeOnDemand))
	// Seeding twice is what startup does on every boot.
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "other.test", db.DNSModeDelegation))

	list, err := svc.Domains.List(ctx, org)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "seeded.test", list[0].BaseDomain)
	assert.True(t, list[0].IsPrimary)
	assert.True(t, list[0].Verified, "we control CoreDNS for the seeded domain, so ownership is implicit")
	assert.Equal(t, db.DNSModeOnDemand, list[0].DNSMode)
}

func TestListPutsThePrimaryFirst(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	_, err := svc.Domains.Create(ctx, org, "older.test", "")
	require.NoError(t, err)
	newer, err := svc.Domains.Create(ctx, org, "newer.test", "")
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", newer.ID).Update("is_primary", true).Error)

	list, err := svc.Domains.List(ctx, org)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// The newer row comes first because it is primary, which is the order the
	// picker wants. Age only breaks the tie.
	assert.Equal(t, "newer.test", list[0].BaseDomain)
	assert.Equal(t, "older.test", list[1].BaseDomain)
}

func TestDeleteDomainRefusesThePrimaryAndOneStillRouted(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "primary.test", db.DNSModeDelegation))
	primary, err := svc.Domains.List(ctx, org)
	require.NoError(t, err)

	// The platform is served on it and there is nothing to fall back to.
	require.Error(t, svc.Domains.Delete(ctx, primary[0].ID))

	second, err := svc.Domains.Create(ctx, org, "second.test", "")
	require.NoError(t, err)

	proj := &db.Project{OrganizationID: org, Name: "p", Slug: "p"}
	require.NoError(t, gdb.Create(proj).Error)
	route := &db.Route{
		OrganizationID: org, ProjectID: proj.ID, DomainID: &second.ID,
		Zone: db.RouteZonePublic, Subdomain: "app", Hostname: "app.second.test",
	}
	require.NoError(t, gdb.Create(route).Error)

	// Still answering for somebody. The FK would refuse anyway; the point is
	// that the caller is told how many and what to do.
	err = svc.Domains.Delete(ctx, second.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 route")

	require.NoError(t, gdb.Unscoped().Delete(route).Error)
	require.NoError(t, svc.Domains.Delete(ctx, second.ID))
}

// The on-demand TLS ask endpoint used to compare against the install's DOMAIN,
// so a second base domain got no certificate and no explanation.
func TestBaseDomainForMatchesAnyVerifiedDomain(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "first.test", db.DNSModeDelegation))
	second, err := svc.Domains.Create(ctx, org, "second.test", db.DNSModeOnDemand)
	require.NoError(t, err)

	got, err := svc.Domains.BaseDomainFor(ctx, "app.first.test")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "first.test", got.BaseDomain)

	// Unverified until the TXT record is found, so nothing may be issued for it.
	got, err = svc.Domains.BaseDomainFor(ctx, "app.second.test")
	require.NoError(t, err)
	assert.Nil(t, got)

	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", second.ID).Update("verified", true).Error)
	got, err = svc.Domains.BaseDomainFor(ctx, "APP.second.test.")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "second.test", got.BaseDomain)

	// The domain itself is not under itself, and a lookalike is not a match.
	for _, host := range []string{"first.test", "notfirst.test", "first.test.evil.com", ""} {
		got, err = svc.Domains.BaseDomainFor(ctx, host)
		require.NoError(t, err)
		assert.Nil(t, got, "%q must not match", host)
	}
}

// A domain delegated under another one must win over its parent, or every name
// inside it resolves to the wrong zone's settings.
func TestBaseDomainForPrefersTheLongestMatch(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.test", db.DNSModeDelegation))
	child, err := svc.Domains.Create(ctx, org, "apps.example.test", db.DNSModeOnDemand)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", child.ID).Update("verified", true).Error)

	got, err := svc.Domains.BaseDomainFor(ctx, "shop.apps.example.test")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "apps.example.test", got.BaseDomain)
}

// A base domain is org-wide, so what is on it spans projects. Listing per
// project would answer a question nobody asked.
func TestRoutesOnDomainSpansProjects(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "example.com", db.DNSModeDelegation))
	list, err := svc.Domains.List(ctx, org)
	require.NoError(t, err)
	base := list[0]

	mk := func(name string) *db.Project {
		p := &db.Project{OrganizationID: org, Name: name, Slug: name}
		require.NoError(t, gdb.Create(p).Error)
		return p
	}
	alpha, beta := mk("alpha"), mk("beta")

	route := func(p *db.Project, host string, domainID *uuid.UUID, published bool) {
		r := &db.Route{OrganizationID: org, ProjectID: p.ID, DomainID: domainID,
			Zone: db.RouteZonePublic, Subdomain: host, Hostname: host}
		require.NoError(t, gdb.Create(r).Error)
		if !published {
			// GORM leaves a false bool out of the insert when the column has a
			// default, so a paused route is written and then paused - the same
			// thing RouteService.Create does.
			require.NoError(t, gdb.Model(r).Update("published", false).Error)
		}
	}
	route(alpha, "shop.example.com", &base.ID, true)
	route(beta, "admin.example.com", &base.ID, false)
	// A custom domain: its own hostname, no base domain.
	route(beta, "store.customer.com", nil, true)

	rows, err := svc.Domains.RoutesOnDomain(ctx, org, base.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2, "both projects' routes, and not the custom domain")
	assert.Equal(t, "admin.example.com", rows[0].Hostname, "sorted by hostname")
	assert.Equal(t, "beta", rows[0].ProjectName, "the project is named, so the page can link to it")
	assert.False(t, rows[0].Published, "a paused route still holds the domain")
	assert.Equal(t, "shop.example.com", rows[1].Hostname)
	assert.Equal(t, "alpha", rows[1].ProjectName)

	custom, err := svc.Domains.CustomDomains(ctx, org)
	require.NoError(t, err)
	require.Len(t, custom, 1)
	assert.Equal(t, "store.customer.com", custom[0].Hostname)
	assert.Equal(t, "beta", custom[0].ProjectName)
}

// One org's routes are not another's, even though the query is org-wide.
func TestRoutesOnDomainIsScopedToTheOrganisation(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	mine, theirs := domainOrg(t, gdb), domainOrg(t, gdb)

	require.NoError(t, svc.Domains.CreateSeeded(ctx, mine, "mine.test", db.DNSModeDelegation))
	list, _ := svc.Domains.List(ctx, mine)
	base := list[0]

	p := &db.Project{OrganizationID: theirs, Name: "theirs", Slug: "theirs"}
	require.NoError(t, gdb.Create(p).Error)
	require.NoError(t, gdb.Create(&db.Route{OrganizationID: theirs, ProjectID: p.ID,
		DomainID: &base.ID, Zone: db.RouteZonePublic, Subdomain: "x", Hostname: "x.mine.test"}).Error)

	rows, err := svc.Domains.RoutesOnDomain(ctx, mine, base.ID)
	require.NoError(t, err)
	assert.Empty(t, rows)

	custom, err := svc.Domains.CustomDomains(ctx, mine)
	require.NoError(t, err)
	assert.Empty(t, custom)
}
