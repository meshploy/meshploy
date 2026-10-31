package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A hostname one label under a domain the organisation has verified needs no
// proof: it is a route on that domain, held to that domain's rules. Anything
// else is still a custom hostname that has to be proved.
func TestAHostnameOnYourOwnDomainNeedsNoProof(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	org := meshdb.Organization{Name: "o", Slug: "o-" + uuid.NewString()[:8]}
	other := meshdb.Organization{Name: "x", Slug: "x-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	require.NoError(t, gdb.Create(&other).Error)
	proj := meshdb.Project{OrganizationID: org.ID, Name: "p", Slug: "p-" + uuid.NewString()[:6]}
	require.NoError(t, gdb.Create(&proj).Error)
	base := "b" + uuid.NewString()[:8] + ".example.com"
	foreign := "f" + uuid.NewString()[:8] + ".example.com"
	unproved := "u" + uuid.NewString()[:8] + ".example.com"
	domain := meshdb.Domain{OrganizationID: org.ID, BaseDomain: base, Verified: true, InternalSubdomain: "internal", PreviewSubdomain: "preview"}
	require.NoError(t, gdb.Create(&domain).Error)
	require.NoError(t, gdb.Create(&meshdb.Domain{OrganizationID: other.ID, BaseDomain: foreign, Verified: true}).Error)
	require.NoError(t, gdb.Create(&meshdb.Domain{OrganizationID: org.ID, BaseDomain: unproved, Verified: false}).Error)

	create := func(host string) (*meshdb.Route, error) {
		return svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: org.ID, ProjectID: proj.ID, Hostname: host, Zone: meshdb.RouteZonePublic})
	}

	route, err := create("Shop." + base + ".")
	require.NoError(t, err)
	require.NotNil(t, route.DomainID, "asked for proof of a domain already proved")
	assert.Equal(t, domain.ID, *route.DomainID)
	assert.Equal(t, "shop", route.Subdomain)
	assert.Equal(t, "shop."+base, route.Hostname)
	assert.Empty(t, route.CustomDomainVerifyToken)

	_, err = create("console." + base)
	require.Error(t, err, "a platform name taken through a whole hostname")
	assert.Contains(t, err.Error(), "reserved")

	for name, host := range map[string]string{
		"a deeper name":           "a.b." + base,
		"another org's domain":    "app." + foreign,
		"a domain not yet proved": "shop." + unproved,
		"somewhere else":          "shop.customer.example",
	} {
		r, err := create(host)
		require.NoError(t, err, name)
		assert.Nil(t, r.DomainID, name)
		assert.Len(t, r.CustomDomainVerifyToken, 32, name)
	}
}
