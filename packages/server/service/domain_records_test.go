package service_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/dnsprovider"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memDNS is a provider holding records in memory for one zone, example.com,
// that its token may write in. rejected makes it refuse the token.
type memDNS struct {
	mu       sync.Mutex
	records  []dnsprovider.Record
	rejected bool
}

func (m *memDNS) Zones(context.Context) ([]dnsprovider.Zone, error) {
	if m.rejected {
		return nil, dnsprovider.ErrRejected
	}
	return []dnsprovider.Zone{{ID: "z1", Name: "example.com"}}, nil
}
func (m *memDNS) Records(_ context.Context, _ dnsprovider.Zone, name string) ([]dnsprovider.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []dnsprovider.Record
	for _, r := range m.records {
		if r.Name == name {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memDNS) CreateRecord(_ context.Context, _ dnsprovider.Zone, r dnsprovider.Record) (dnsprovider.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID = uuid.NewString()
	m.records = append(m.records, r)
	return r, nil
}
func (m *memDNS) DeleteRecord(_ context.Context, _ dnsprovider.Zone, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.records[:0]
	for _, r := range m.records {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	m.records = kept
	return nil
}
func (m *memDNS) names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, r := range m.records {
		out = append(out, r.Name)
	}
	return out
}

func recordsServer(t *testing.T) (*service.Services, uuid.UUID, *db.DNSProvider, *memDNS) {
	t.Helper()
	svc, gdb, org, _ := edgeServer(t)
	mem := &memDNS{}
	service.UseDNSProvidersForTest(svc, func(dnsprovider.Kind, string) (dnsprovider.Provider, error) { return mem, nil }, "203.0.113.10")
	p := &db.DNSProvider{OrganizationID: org, Name: "Cloudflare", Kind: "cloudflare", Token: "tok", Zones: []string{"example.com"}}
	require.NoError(t, gdb.Create(p).Error)
	return svc, org, p, mem
}

func status(t *testing.T, err error) int {
	t.Helper()
	var se huma.StatusError
	require.ErrorAs(t, err, &se, "got %v", err)
	return se.GetStatus()
}

// Adding a domain through a provider writes its records, verifies it without
// a TXT record, and removing it takes away what was written and nothing else.
func TestADomainWhoseRecordsAProviderWrites(t *testing.T) {
	svc, org, p, mem := recordsServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "gw.example.net", db.DNSModeDelegation))
	mem.records = append(mem.records, dnsprovider.Record{ID: "op", Type: "TXT", Name: "acme.example.com", Content: "v=spf1 -all"})

	d, err := svc.Domains.CreateThroughProvider(ctx, org, "acme.example.com", p.ID)
	require.NoError(t, err)
	assert.True(t, d.Verified, "the provider's records are the proof of control")
	assert.Equal(t, db.DNSModeOnDemand, d.DNSMode)
	require.NotNil(t, d.DNSProviderID)
	assert.ElementsMatch(t, []string{"acme.example.com", "*.acme.example.com"}, mem.names(),
		"the wildcard only: the name already has a record of the operator's")

	_, err = svc.Domains.SetDNSMode(ctx, d.ID, db.DNSModeDelegation)
	assert.Equal(t, http.StatusUnprocessableEntity, status(t, err), "a provider's domain stays on-demand")
	err = svc.DNSProviders.Delete(ctx, org, p.ID)
	assert.ErrorIs(t, err, service.ErrDNSProviderInUse)

	_, err = svc.Domains.StartRetiring(ctx, d.ID)
	require.NoError(t, err)
	require.NoError(t, svc.Domains.Delete(ctx, d.ID))
	assert.Equal(t, []string{"acme.example.com"}, mem.names(), "the operator's record stays")
}

// A name outside the token's zones, or one the operator points elsewhere,
// is refused before anything is kept.
func TestAddingThroughAProviderIsRefused(t *testing.T) {
	svc, org, p, mem := recordsServer(t)
	ctx := context.Background()

	_, err := svc.Domains.CreateThroughProvider(ctx, org, "acme.example.org", p.ID)
	assert.Equal(t, http.StatusUnprocessableEntity, status(t, err))
	assert.Contains(t, err.Error(), "example.com", "the zones it may write in are named")

	mem.records = []dnsprovider.Record{{ID: "op", Type: "CNAME", Name: "*.taken.example.com", Content: "elsewhere.net"}}
	_, err = svc.Domains.CreateThroughProvider(ctx, org, "taken.example.com", p.ID)
	assert.Equal(t, http.StatusConflict, status(t, err))

	list, err := svc.Domains.List(ctx, org)
	require.NoError(t, err)
	assert.Empty(t, list, "a domain whose records failed is not added")

	_, err = svc.Domains.CreateThroughProvider(ctx, org, "x.example.com", uuid.New())
	assert.Equal(t, http.StatusUnprocessableEntity, status(t, err), "another organization's or a missing provider")
}

// Switching to manual removes Meshploy's records and keeps the domain served;
// a token the provider no longer accepts does not trap the domain.
func TestSwitchingADomainsRecordsToManual(t *testing.T) {
	svc, org, p, mem := recordsServer(t)
	ctx := context.Background()
	d, err := svc.Domains.CreateThroughProvider(ctx, org, "acme.example.com", p.ID)
	require.NoError(t, err)

	d, err = svc.Domains.SetDNSProvider(ctx, d.ID, nil)
	require.NoError(t, err)
	assert.Nil(t, d.DNSProviderID)
	assert.True(t, d.Verified)
	assert.Empty(t, mem.names())

	d, err = svc.Domains.SetDNSProvider(ctx, d.ID, &p.ID)
	require.NoError(t, err)
	assert.Len(t, mem.names(), 2)

	mem.rejected = true
	d, err = svc.Domains.SetDNSProvider(ctx, d.ID, nil)
	require.NoError(t, err, "a revoked token must not keep the domain on its provider")
	assert.Nil(t, d.DNSProviderID)
}
