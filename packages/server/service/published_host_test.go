package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A service publishes the address it really runs at. A database called
// docai_db runs as its generated name (docai-db-<suffix>), and a host built
// from the display name would point at nothing, which is how an app lost its
// database. The startup refresh corrects a group an earlier version wrote.
func TestAPublishedHostIsTheWorkloadsRealName(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, orgID, "DocAI", "docai")
	require.NoError(t, err)

	dbSvc, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "docai_db", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)
	var dc meshdb.DatabaseConfig
	require.NoError(t, gdb.First(&dc, "service_id = ?", dbSvc.ID).Error)
	require.NotEqual(t, "docai-db", dc.Slug, "the database runs under a generated name")

	host := func() string {
		var item meshdb.VariableGroupItem
		require.NoError(t, gdb.Where("key = ? AND group_id IN (?)", "DOCAI_DB_HOST",
			gdb.Model(&meshdb.VariableGroup{}).Select("id").Where("service_id = ?", dbSvc.ID)).First(&item).Error)
		return string(item.Value)
	}
	assert.Equal(t, dc.Slug+".docai.svc.cluster.local", host())

	// A group written wrong by an earlier version is put right at startup.
	require.NoError(t, gdb.Model(&meshdb.VariableGroupItem{}).Where("key = ?", "DOCAI_DB_HOST").
		Update("value", meshdb.EncryptedString("docai-db.docai.svc.cluster.local")).Error)
	require.NoError(t, svcs.VariableGroups.RefreshPublished(ctx))
	assert.Equal(t, dc.Slug+".docai.svc.cluster.local", host())
}
