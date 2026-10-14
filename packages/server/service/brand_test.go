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

// Every organisation shows Meshploy's brand until an edition gives it one.
// Signed out, the brand is the one of the organisation whose domain the host
// is under, a port and all; any other host shows Meshploy's.
func TestBrandIsMeshployUntilAnEditionGivesOne(t *testing.T) {
	gdb := newTestDB(t)
	svc := service.New(gdb, &config.Config{})
	ctx := context.Background()
	org := domainOrg(t, gdb)
	require.NoError(t, svc.Domains.CreateSeeded(ctx, org, "agency.test", db.DNSModeDelegation))

	assert.Equal(t, db.DefaultBrand, svc.Brands.ForOrg(ctx, org))
	assert.Equal(t, db.DefaultBrand, svc.Brands.ForHost(ctx, "console.agency.test"))

	db.RegisterBrandSource(func(_ context.Context, _ *gorm.DB, orgID uuid.UUID) (db.Brand, bool) {
		if orgID != org {
			return db.Brand{}, false
		}
		return db.Brand{Name: "Acme Cloud", Accent: "#ff6600", PoweredBy: true}, true
	})
	t.Cleanup(func() { db.RegisterBrandSource(nil) })

	got := svc.Brands.ForOrg(ctx, org)
	assert.Equal(t, "Acme Cloud", got.Name)
	assert.True(t, got.Custom)
	assert.Equal(t, got, svc.Brands.ForHost(ctx, "console.agency.test:443"))

	assert.Equal(t, db.DefaultBrand, svc.Brands.ForOrg(ctx, uuid.New()), "an organisation the source does not know")
	for _, host := range []string{"203.0.113.7", "localhost:5173", "console.other.test", ""} {
		assert.Equal(t, db.DefaultBrand, svc.Brands.ForHost(ctx, host), "%q", host)
	}
}
