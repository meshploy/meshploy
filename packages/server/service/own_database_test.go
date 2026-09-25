package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// A level that uses a database from above can be given its own: the same
// engine, version and names, with its own password and volume. Its services
// then use it, since a service uses the nearest copy.
func TestALevelCanHaveItsOwnDatabase(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, web, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())

	// Production's pg, published, and used by web.
	pg, err := svcs.Workloads.Create(ctx, prod, service.CreateWorkloadInput{Name: "orders", Type: meshdb.ServiceTypeDatabase,
		Engine: meshdb.DatabasePostgres, Version: "16", DBName: "orders", DBUser: "orders"})
	require.NoError(t, err)
	var prodDC meshdb.DatabaseConfig
	require.NoError(t, gdb.First(&prodDC, "service_id = ?", pg.ID).Error)
	var pgGroup meshdb.VariableGroup
	require.NoError(t, gdb.First(&pgGroup, "service_id = ?", pg.ID).Error)
	require.NoError(t, svcs.VariableGroups.Attach(ctx, web.ID, pgGroup.ID))

	t.Run("a clone needs a backup, and nothing is created without one", func(t *testing.T) {
		_, err := svcs.Promotions.OwnDatabase(ctx, s1, pg.ID, true)
		assert.ErrorIs(t, err, service.ErrNoBackup)
		var n int64
		require.NoError(t, gdb.Model(&meshdb.Service{}).Where("project_id = ? AND type = ?", s1, meshdb.ServiceTypeDatabase).Count(&n).Error)
		assert.Zero(t, n)
	})

	own, err := svcs.Promotions.OwnDatabase(ctx, s1, pg.ID, false)
	require.NoError(t, err)
	assert.Equal(t, s1, own.ProjectID)
	require.NotNil(t, own.LineageID)
	assert.Equal(t, pg.ID, *own.LineageID, "a copy of the same database, across levels")

	var ownDC meshdb.DatabaseConfig
	require.NoError(t, gdb.First(&ownDC, "service_id = ?", own.ID).Error)
	assert.Equal(t, prodDC.Engine, ownDC.Engine)
	assert.Equal(t, prodDC.Version, ownDC.Version)
	assert.Equal(t, "orders", ownDC.DBName)
	assert.NotEqual(t, prodDC.DBPassword, ownDC.DBPassword, "its own credentials, nothing shared")
	assert.NotEqual(t, prodDC.Slug, ownDC.Slug)

	_, err = svcs.Promotions.OwnDatabase(ctx, s1, pg.ID, false)
	assert.ErrorIs(t, err, service.ErrHasOwnDatabase)

	// web in staging1 now uses staging1's orders, not production's.
	var webS1 meshdb.Service
	webS1 = web
	webS1.ID, webS1.ProjectID, webS1.LineageID = [16]byte{}, s1, &web.ID
	require.NoError(t, gdb.Create(&webS1).Error)
	require.NoError(t, svcs.VariableGroups.Attach(ctx, webS1.ID, pgGroup.ID))
	env, err := svcs.VariableGroups.CollectEnvVars(ctx, webS1.ID)
	require.NoError(t, err)
	assert.Contains(t, env["ORDERS_HOST"], ".coreline-staging1.", "the level's own database, by its namespace")

	board, err := svcs.Promotions.Board(ctx, prod)
	require.NoError(t, err)
	var levels []string
	for _, c := range board.Ungrouped {
		if c.ServiceName == "orders" {
			levels = append(levels, c.LevelID.String())
		}
	}
	assert.Len(t, levels, 2, "the board shows orders in production and in staging1")
}
