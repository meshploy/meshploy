package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A backup schedule or SMTP setting saved as off the first time is stored off.
// The columns used to carry a gorm default of true, which GORM writes in place
// of Go's false when it creates the row.
func TestFirstSaveKeepsFalse(t *testing.T) {
	ctx := context.Background()
	svcs, db, orgID, projID, _ := setupVolumeTest(t)
	oid := parseUUID(t, orgID)

	sto := meshdb.StorageIntegration{
		OrganizationID: oid, Name: "s3", Provider: meshdb.StorageProvider("s3"),
		Bucket: "backups", AccessKeyID: "key", SecretAccessKey: "secret",
	}
	require.NoError(t, db.Create(&sto).Error)

	t.Run("system backup enabled", func(t *testing.T) {
		_, err := svcs.Backups.UpsertSystem(ctx, oid, service.UpsertSystemBackupInput{
			StorageIntegrationID: sto.ID, Schedule: "0 2 * * *",
		})
		require.NoError(t, err)
		var row meshdb.SystemBackupConfig
		require.NoError(t, db.Where("organization_id = ?", oid).First(&row).Error)
		assert.False(t, row.Enabled)
	})

	t.Run("volume backup enabled", func(t *testing.T) {
		vol, err := svcs.Volumes.Create(ctx, parseUUID(t, projID), "data", 5, nil)
		require.NoError(t, err)
		_, err = svcs.Volumes.UpsertBackupConfig(ctx, vol.ID, service.VolumeBackupConfigInput{
			StorageIntegrationID: sto.ID, Schedule: "0 2 * * *", RetentionDays: 7,
		})
		require.NoError(t, err)
		var row meshdb.VolumeBackupConfig
		require.NoError(t, db.Where("volume_id = ?", vol.ID).First(&row).Error)
		assert.False(t, row.Enabled)
	})

	t.Run("email use_tls", func(t *testing.T) {
		_, err := svcs.EmailConfig.Save(ctx, oid, service.SaveEmailConfigInput{
			Host: "smtp.example.com", Port: 587, Username: "ops", Password: "pw",
			FromAddress: "ops@example.com",
		})
		require.NoError(t, err)
		var row meshdb.OrgEmailConfig
		require.NoError(t, db.Where("organization_id = ?", oid).First(&row).Error)
		assert.False(t, row.UseTLS)
	})
}
