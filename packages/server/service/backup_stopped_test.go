package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// A stopped database has nothing to dump, and stopping one is something an
// operator chose to do. Asked for by hand, the answer comes back at once
// instead of a run that quietly does nothing.
func TestBackingUpAStoppedDatabaseIsRefusedRatherThanFailed(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "backups", Slug: "backups"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "backups", "backups")
	require.NoError(t, err)
	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "dummypsql", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)

	storage := meshdb.StorageIntegration{
		OrganizationID: org.ID, Name: "cf-r1", Provider: "s3", Bucket: "b",
		Endpoint: "https://example.invalid", AccessKeyID: "k", SecretAccessKey: "s",
	}
	require.NoError(t, gdb.Create(&storage).Error)

	cfg, err := svcs.Backups.Create(ctx, org.ID, database.ID, service.CreateBackupInput{
		StorageIntegrationID: storage.ID, Schedule: "0 2 * * *", RetentionDays: 4, PathPrefix: "dummydb",
	})
	require.NoError(t, err)

	// A database is created stopped, which is the state this is about.
	require.Equal(t, meshdb.ServiceStopped, database.Status)

	_, err = svcs.Backups.Trigger(ctx, cfg.ID, database.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stopped")

	// And nothing was recorded as having failed.
	var after meshdb.BackupConfig
	require.NoError(t, gdb.First(&after, "id = ?", cfg.ID).Error)
	if after.LastBackupStatus != nil {
		require.NotEqual(t, meshdb.BackupFailed, *after.LastBackupStatus)
	}
}
