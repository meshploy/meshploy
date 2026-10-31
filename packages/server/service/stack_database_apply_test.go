package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// A managed database records what it was provisioned with, as a deploy does, so
// applying the same file again finds nothing changed. A change to it is still
// reported.
func TestApplyingAnUnchangedDatabaseSaysNothing(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{})
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())
	name := "dbapply" + uuid.NewString()[:6]
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: name, Email: name + "@example.com", Password: "a long enough password"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "shop", "shop-"+uuid.NewString()[:6])
	require.NoError(t, err)

	spec := func(image string) string {
		return `
services:
  shop-db:
    image: ` + image + `
    x-meshploy:
      type: database
      database:
        engine: postgres
        version: "16"
        db_name: shop
        db_user: shop
`
	}
	apply := func(s string) *service.ApplyResult {
		t.Helper()
		r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "store", Spec: s}, user.ID)
		require.NoError(t, err)
		require.Empty(t, r.Errors)
		return r
	}

	apply(spec("postgres:16"))
	require.Eventually(t, func() bool {
		var svc meshdb.Service
		require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, "shop-db").First(&svc).Error)
		return svc.Status == meshdb.ServiceRunning
	}, 10*time.Second, 50*time.Millisecond)

	assert.Empty(t, apply(spec("postgres:16")).Warnings)
	assert.NotEmpty(t, apply(spec("postgres:17")).Warnings, "a real change is still reported")
}
