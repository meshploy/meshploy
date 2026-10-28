package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A domain reserved for one project takes routes from that project and its
// levels only, and no other project can hold a name under it, even as a
// whole hostname. Its project cannot be deleted from under it.
func TestADomainReservedForOneProject(t *testing.T) {
	svc, gdb, org, _ := edgeServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "pool.example.net", db.DNSModeOnDemand))
	acme, err := svc.Projects.Create(ctx, org, "Acme", "acme-"+uuid.NewString()[:6])
	require.NoError(t, err)
	other, err := svc.Projects.Create(ctx, org, "Other", "other-"+uuid.NewString()[:6])
	require.NoError(t, err)
	staging, err := svc.Projects.CreateLevel(ctx, acme.ID, "staging", acme.ID, false)
	require.NoError(t, err)

	d, err := svc.Domains.Create(ctx, org, "acme.example.com", db.DNSModeOnDemand)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", d.ID).Update("verified", true).Error)

	_, err = svc.Domains.ReserveForProject(ctx, d.ID, &staging.ID)
	assert.ErrorContains(t, err, "not one of its environment levels")
	_, err = svc.Domains.ReserveForProject(ctx, d.ID, &acme.ID)
	require.NoError(t, err)

	_, err = svc.Routes.Create(ctx, service.CreateRouteInput{OrgID: org, ProjectID: acme.ID, DomainID: &d.ID,
		Subdomain: "app", Zone: db.RouteZonePublic})
	require.NoError(t, err, "its own project")
	_, err = svc.Routes.Create(ctx, service.CreateRouteInput{OrgID: org, ProjectID: staging.ID, DomainID: &d.ID,
		Subdomain: "app", Zone: db.RouteZonePublic})
	require.NoError(t, err, "and its levels")
	_, err = svc.Routes.Create(ctx, service.CreateRouteInput{OrgID: org, ProjectID: other.ID, DomainID: &d.ID,
		Subdomain: "steal", Zone: db.RouteZonePublic})
	assert.ErrorContains(t, err, "reserved for another project")
	_, err = svc.Routes.Create(ctx, service.CreateRouteInput{OrgID: org, ProjectID: other.ID, Hostname: "squat.acme.example.com"})
	assert.ErrorContains(t, err, "reserved for another project", "a whole hostname under it is refused too")

	// The level goes first, as it must for any project; then the domain is
	// what holds it.
	require.NoError(t, svc.Promotions.DeleteProject(ctx, staging.ID))
	err = svc.Promotions.DeleteProject(ctx, acme.ID)
	assert.ErrorIs(t, err, service.ErrProjectHasDomain)
	assert.ErrorContains(t, err, "acme.example.com")

	_, err = svc.Domains.ReserveForProject(ctx, d.ID, nil)
	require.NoError(t, err)
	_, err = svc.Routes.Create(ctx, service.CreateRouteInput{OrgID: org, ProjectID: other.ID, DomainID: &d.ID,
		Subdomain: "shared", Zone: db.RouteZonePublic})
	require.NoError(t, err, "given back, every project may use it")
	_, err = svc.Domains.ReserveForProject(ctx, d.ID, &acme.ID)
	assert.ErrorContains(t, err, "route(s) of other projects", "reserving it would strand another project's route")
}
