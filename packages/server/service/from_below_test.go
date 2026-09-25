package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// A level never uses variables from a level below it. A service that started
// in staging and reads staging's own database, or a group of staging's, is
// not promoted to production, which is told what it needs first; and a
// deploy that would read them is refused.
func TestNothingIsBorrowedFromBelow(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, _, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())

	// staging1 has a database production does not, and a shop using it.
	pg := meshdb.Service{ProjectID: s1, Name: "orders-db", Slug: "orders-db", Type: meshdb.ServiceTypeDatabase}
	require.NoError(t, gdb.Create(&pg).Error)
	published := meshdb.VariableGroup{ProjectID: s1, ServiceID: &pg.ID, Name: "orders-db", SystemManaged: true}
	require.NoError(t, gdb.Create(&published).Error)
	shop := meshdb.Service{ProjectID: s1, Name: "shop", Slug: "shop", Type: meshdb.ServiceTypeApplication, EnvVars: "MODE=staging\nDEBUG=1"}
	require.NoError(t, gdb.Create(&shop).Error)
	require.NoError(t, gdb.Create(&meshdb.ServiceVariableGroup{ServiceID: shop.ID, GroupID: published.ID}).Error)
	now := time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: shop.ID, Status: meshdb.DeploymentSuccess, Image: "registry/shop:v1", DeployedAt: &now}).Error)

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "shop", ServiceIDs: []uuid.UUID{shop.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)

	notes, err := svcs.Promotions.Preflight(ctx, s1, group.ID)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.True(t, notes[0].NewThere)
	assert.Equal(t, 2, notes[0].OwnVariables, "its own variables are copied as they are, so the dialog says how many")
	assert.Contains(t, notes[0].Blocked, "give production its own orders-db first")

	_, err = svcs.Promotions.Promote(ctx, s1, group.ID)
	assert.ErrorIs(t, err, service.ErrNothingToPromote)
	var none meshdb.Service
	assert.Error(t, gdb.First(&none, "project_id = ? AND name = ?", prod, "shop").Error, "nothing half-made in production")

	// Production gets its own orders-db, running and published: shop moves.
	prodPG := meshdb.Service{ProjectID: prod, Name: "orders-db", Slug: "orders-db", Type: meshdb.ServiceTypeDatabase, LineageID: &pg.ID}
	require.NoError(t, gdb.Create(&prodPG).Error)
	require.NoError(t, gdb.Create(&meshdb.VariableGroup{ProjectID: prod, ServiceID: &prodPG.ID, Name: "orders-db-prod", SystemManaged: true}).Error)
	result, err := svcs.Promotions.Promote(ctx, s1, group.ID)
	require.NoError(t, err)
	require.Len(t, result.Promoted, 1)

	// A shared group of staging's attached in production is refused at deploy.
	shared := meshdb.VariableGroup{ProjectID: s1, Name: "flags"}
	require.NoError(t, gdb.Create(&shared).Error)
	var up meshdb.Service
	require.NoError(t, gdb.First(&up, "project_id = ? AND name = ?", prod, "shop").Error)
	require.NoError(t, gdb.Create(&meshdb.ServiceVariableGroup{ServiceID: up.ID, GroupID: shared.ID}).Error)
	_, err = svcs.Deployments.DeployImage(ctx, up.ID, "registry/shop:v1", "again", service.Provenance{})
	assert.ErrorIs(t, err, service.ErrBorrowFromBelow)
	assert.ErrorContains(t, err, "the flags variable group belongs to staging1")
}

// Leaving a group, one service or the whole group, hands the copies above the
// entry back their own builds: nothing promotes to them any more.
func TestLeavingAGroupGivesBackAutoDeploy(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, web, _ := newChain(t)
	first := func(dest any, conds ...any) error { return gdb.First(dest, conds...).Error }

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "web", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s1, prod}, Single: true})
	require.NoError(t, err)
	var bc meshdb.BuildConfig
	require.NoError(t, first(&bc, "service_id = ?", web.ID))
	require.False(t, bc.AutoDeploy, "production receives promotions while grouped")

	deleted, err := svcs.Promotions.RemoveFromGroup(ctx, prod, group.ID, web.ID)
	require.NoError(t, err)
	assert.True(t, deleted)
	require.NoError(t, first(&bc, "service_id = ?", web.ID))
	assert.True(t, bc.AutoDeploy, "ungrouped, production builds on push again, as staging did")

	group, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "again", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)
	require.NoError(t, svcs.Promotions.DeleteGroup(ctx, prod, group.ID))
	require.NoError(t, first(&bc, "service_id = ?", web.ID))
	assert.True(t, bc.AutoDeploy, "and the same when the whole group goes")
}
