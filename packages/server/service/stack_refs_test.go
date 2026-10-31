package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An app that reads another stack service's published variable has that
// service's group by the time it could start, without a second step. A reference
// to nothing is said out loud instead of starting as literal text, and the same
// name prefix alone attaches nothing.
func TestAStackServiceGetsWhatItReads(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	name := "reader" + uuid.NewString()[:6]
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: name, Email: name + "@example.com", Password: "a long enough password"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "shop", "shop-"+uuid.NewString()[:6])
	require.NoError(t, err)

	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "store", Spec: `
services:
  shop:
    image: shop:1
    expose: ["9000"]
    environment:
      DATABASE_URL: $${SHOP_DB_URL}
      MISSING: $${NOT_DEFINED_ANYWHERE}
      LITERAL: $$$${SHOP_DB_PASSWORD}
    depends_on:
      - shop-db
  plain:
    image: plain:1
    expose: ["8000"]
  shop-db:
    image: postgres:16
    x-meshploy:
      type: database
      database:
        engine: postgres
        version: "16"
        db_name: shop
        db_user: shop
`}, user.ID)
	require.NoError(t, err)

	attached := func(name string) []string {
		t.Helper()
		var svc meshdb.Service
		require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, name).First(&svc).Error)
		groups, err := svcs.VariableGroups.ListForService(ctx, svc.ID)
		require.NoError(t, err)
		var names []string
		for _, g := range groups {
			names = append(names, g.Name)
		}
		return names
	}
	assert.Equal(t, []string{"shop-db (service)"}, attached("shop"))
	assert.Empty(t, attached("plain"), "a service that reads nothing got a group")
	assert.Empty(t, attached("shop-db"))

	said := strings.Join(r.Warnings, "\n")
	assert.Contains(t, said, "NOT_DEFINED_ANYWHERE")
	assert.NotContains(t, said, "SHOP_DB_URL", "warned about what was attached")
	assert.NotContains(t, said, "SHOP_DB_PASSWORD", "warned about an escaped reference")
}
