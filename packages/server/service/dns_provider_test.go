package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/dnsprovider"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDNS is a provider whose token "good" may write in example.com and only
// read example.org; any other token is rejected.
type stubDNS struct{ token string }

func (s stubDNS) Zones(context.Context) ([]dnsprovider.Zone, error) {
	if s.token != "good" {
		return nil, dnsprovider.ErrRejected
	}
	return []dnsprovider.Zone{{ID: "z1", Name: "example.com"}, {ID: "z2", Name: "example.org"}}, nil
}
func (stubDNS) Records(context.Context, dnsprovider.Zone, string) ([]dnsprovider.Record, error) {
	return nil, nil
}
func (stubDNS) CreateRecord(_ context.Context, z dnsprovider.Zone, r dnsprovider.Record) (dnsprovider.Record, error) {
	if z.Name != "example.com" {
		return r, dnsprovider.ErrRejected
	}
	r.ID = "r1"
	return r, nil
}
func (stubDNS) DeleteRecord(context.Context, dnsprovider.Zone, string) error { return nil }

// A token is stored only once it passes its check, encrypted, with the zones
// it may write in; a provider a domain uses cannot be removed.
func TestConnectingADNSProvider(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	org := db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	svc := service.NewDNSProvidersForTest(gdb, func(_ dnsprovider.Kind, token string) (dnsprovider.Provider, error) {
		return stubDNS{token: token}, nil
	})

	_, err := svc.Create(ctx, org.ID, service.CreateDNSProviderInput{Name: "cf", Kind: dnsprovider.Cloudflare, Token: "bad"})
	assert.True(t, errors.Is(err, service.ErrDNSProviderCheck), "got %v", err)
	list, err := svc.List(ctx, org.ID)
	require.NoError(t, err)
	assert.Empty(t, list, "a token that fails is not kept")

	got, err := svc.Create(ctx, org.ID, service.CreateDNSProviderInput{Name: " cf ", Kind: dnsprovider.Cloudflare, Token: " good "})
	require.NoError(t, err)
	assert.Equal(t, "cf", got.Provider.Name)
	assert.Equal(t, []string{"example.com"}, got.Provider.Zones)
	assert.Len(t, got.Zones, 2, "the check reports every zone it saw")
	var stored db.DNSProvider
	require.NoError(t, gdb.First(&stored, "id = ?", got.Provider.ID).Error)
	assert.Equal(t, "good", string(stored.Token), "trimmed, and readable again through the key")
	var raw string
	require.NoError(t, gdb.Raw("SELECT token FROM dns_providers WHERE id = ?", got.Provider.ID).Scan(&raw).Error)
	assert.NotContains(t, raw, "good", "never stored as plain text")

	again, err := svc.Check(ctx, org.ID, got.Provider.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com"}, again.Provider.Zones)

	domain := db.Domain{OrganizationID: org.ID, BaseDomain: "example-" + uuid.NewString()[:8] + ".com", VerifyToken: "x", DNSMode: db.DNSModeOnDemand, DNSProviderID: &got.Provider.ID}
	require.NoError(t, gdb.Create(&domain).Error)
	err = svc.Delete(ctx, org.ID, got.Provider.ID)
	assert.ErrorIs(t, err, service.ErrDNSProviderInUse)
	assert.Contains(t, err.Error(), domain.BaseDomain)

	require.NoError(t, gdb.Model(&domain).Update("dns_provider_id", nil).Error)
	require.NoError(t, svc.Delete(ctx, org.ID, got.Provider.ID))
	other := uuid.New()
	assert.Error(t, svc.Delete(ctx, other, got.Provider.ID), "another organisation's provider is not found")
}
