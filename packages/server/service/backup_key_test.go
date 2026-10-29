package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// One bucket can hold many databases' backups. A restore or a download
// through one database's config reaches its own objects only, never a
// neighbour's under the same bucket.
func TestABackupConfigReachesOnlyItsOwnBackups(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "keys", Slug: "keys"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "keys", "keys")
	require.NoError(t, err)
	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "shop", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)
	storage := meshdb.StorageIntegration{
		OrganizationID: org.ID, Name: "r2", Provider: "s3", Bucket: "backups", Region: "auto",
		Endpoint: "https://example.invalid", AccessKeyID: "k", SecretAccessKey: "s",
	}
	require.NoError(t, gdb.Create(&storage).Error)
	cfg, err := svcs.Backups.Create(ctx, org.ID, database.ID, service.CreateBackupInput{
		StorageIntegrationID: storage.ID, Schedule: "0 3 * * *", RetentionDays: 7, PathPrefix: "apps/acme",
	})
	require.NoError(t, err)

	var dc meshdb.DatabaseConfig
	require.NoError(t, gdb.First(&dc, "service_id = ?", database.ID).Error)
	own := "apps/acme/" + dc.Slug + "/2026-10-30.sql.gz"

	for _, key := range []string{
		"apps/other/" + dc.Slug + "/2026-10-30.sql.gz",
		"apps/acme/" + dc.Slug + "/../../other/x.sql.gz",
		"apps/acme/" + dc.Slug + "x/2026-10-30.sql.gz",
	} {
		require.True(t, errors.Is(svcs.Backups.Restore(ctx, cfg.ID, database.ID, key), service.ErrBackupKey), "restore %s", key)
		_, err := svcs.Backups.DownloadURL(ctx, cfg.ID, database.ID, key)
		require.True(t, errors.Is(err, service.ErrBackupKey), "download %s", key)
	}

	link, err := svcs.Backups.DownloadURL(ctx, cfg.ID, database.ID, own)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(link, "https://example.invalid/backups/"+own), link)
	require.Contains(t, link, "X-Amz-Signature")
}
