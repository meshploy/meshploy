package service_test

import (
	"context"
	"errors"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The proxy reloads its routes when the routes count moves, so every way a
// route changes has to move it, and a change that rolls back must not.
// Nothing else does.
func TestEveryRouteChangeMovesTheProxysCount(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, _, _, _, _ := newChain(t)
	count := func() int64 {
		t.Helper()
		v, err := meshdb.EdgeVersionOf(gdb, meshdb.EdgeRoutes)
		require.NoError(t, err)
		return v
	}
	moves := func(what string, change func()) {
		t.Helper()
		before := count()
		change()
		if count() == before {
			t.Errorf("%s did not move the routes count", what)
		}
	}

	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	domain := meshdb.Domain{OrganizationID: project.OrganizationID, BaseDomain: "acme.dev", InternalSubdomain: "internal",
		PreviewSubdomain: "preview", Verified: true, IsPrimary: true, VerifyToken: "t"}
	require.NoError(t, gdb.Create(&domain).Error)

	var route *meshdb.Route
	moves("creating a route", func() {
		var err error
		route, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: project.OrganizationID, ProjectID: prod,
			DomainID: &domain.ID, Zone: meshdb.RouteZonePublic, Subdomain: "app", Paused: true})
		require.NoError(t, err)
	})
	moves("publishing it", func() {
		require.NoError(t, gdb.Model(route).Update("published", true).Error)
	})
	moves("deleting it", func() {
		require.NoError(t, svcs.Routes.Delete(ctx, route.ID))
	})

	before := count()
	_ = gdb.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Model(&meshdb.Route{}).Where("project_id = ?", prod).Update("published", false).Error)
		return errors.New("roll back")
	})
	require.NoError(t, gdb.Model(&project).Update("name", "renamed").Error)
	if count() != before {
		t.Errorf("a rolled-back route change or a project rename moved the routes count")
	}
}
