package service_test

import (
	"context"
	"testing"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupVolumeTest returns services, db, orgID, projectID, and an application service ID.
func setupVolumeTest(t *testing.T) (*service.Services, *gorm.DB, string, string, string) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	user, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "voluser",
		Email:    "voluser@example.com",
		Password: "pass",
	})
	require.NoError(t, err)

	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)

	proj, err := svcs.Projects.Create(ctx, org.ID, "vol-project", "vol-project")
	require.NoError(t, err)

	// Create a dummy application service so we can test attachment.
	svc, err := svcs.Workloads.Create(ctx, proj.ID, service.CreateWorkloadInput{
		Name:     "app-svc",
		Image:    "nginx:latest",
		Ports:    []service.PortInput{{Name: "http", Port: 80, IsHTTP: true, IsPrimary: true, IsPublic: true}},
		Replicas: 1,
	})
	require.NoError(t, err)

	return svcs, db, org.ID.String(), proj.ID.String(), svc.ID.String()
}

func TestVolumeCreate(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID, _ := setupVolumeTest(t)
	pid := parseUUID(t, projID)

	t.Run("creates volume row with pending status", func(t *testing.T) {
		vol, err := svcs.Volumes.Create(ctx, pid, "my-vol", 10)
		require.NoError(t, err)
		assert.NotEmpty(t, vol.ID)
		assert.Equal(t, "my-vol", vol.Name)
		assert.Equal(t, 10, vol.StorageGB)
		assert.NotEmpty(t, vol.Slug)
		// No k8s → stays pending.
		assert.Equal(t, meshdb.VolumePending, vol.Status)
	})

	t.Run("default storage applied when size zero", func(t *testing.T) {
		vol, err := svcs.Volumes.Create(ctx, pid, "default-vol", 0)
		require.NoError(t, err)
		assert.Equal(t, 5, vol.StorageGB)
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := svcs.Volumes.Create(ctx, pid, "", 5)
		require.Error(t, err)
	})
}

func TestVolumeAttachDetach(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID, svcID := setupVolumeTest(t)
	pid := parseUUID(t, projID)

	vol, err := svcs.Volumes.Create(ctx, pid, "attach-vol", 5)
	require.NoError(t, err)

	t.Run("attach creates mount row", func(t *testing.T) {
		mount, err := svcs.Volumes.Attach(ctx, vol.ID, parseUUID(t, svcID), "/data")
		require.NoError(t, err)
		assert.NotEmpty(t, mount.ID)
		assert.Equal(t, "/data", mount.MountPath)
	})

	t.Run("second attach to same volume is rejected", func(t *testing.T) {
		_, err := svcs.Volumes.Attach(ctx, vol.ID, parseUUID(t, svcID), "/data2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already attached")
	})

	t.Run("detach removes mount row", func(t *testing.T) {
		// Get the mount ID.
		mounts, err := svcs.Volumes.ListMounts(ctx, vol.ID)
		require.NoError(t, err)
		require.Len(t, mounts, 1)

		require.NoError(t, svcs.Volumes.Detach(ctx, mounts[0].ID))

		mounts, err = svcs.Volumes.ListMounts(ctx, vol.ID)
		require.NoError(t, err)
		assert.Empty(t, mounts)
	})
}

func TestVolumeDelete(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID, svcID := setupVolumeTest(t)
	pid := parseUUID(t, projID)

	t.Run("delete attached volume is blocked", func(t *testing.T) {
		vol, err := svcs.Volumes.Create(ctx, pid, "attached-vol", 5)
		require.NoError(t, err)

		_, err = svcs.Volumes.Attach(ctx, vol.ID, parseUUID(t, svcID), "/data")
		require.NoError(t, err)

		err = svcs.Volumes.Delete(ctx, vol.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "still attached")
	})

	t.Run("delete unattached volume succeeds", func(t *testing.T) {
		vol, err := svcs.Volumes.Create(ctx, pid, "free-vol", 5)
		require.NoError(t, err)

		require.NoError(t, svcs.Volumes.Delete(ctx, vol.ID))

		_, err = svcs.Volumes.Get(ctx, vol.ID)
		require.Error(t, err)
	})
}

func TestVolumeAttachValidation(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID, svcID := setupVolumeTest(t)
	pid := parseUUID(t, projID)

	vol, err := svcs.Volumes.Create(ctx, pid, "validate-vol", 5)
	require.NoError(t, err)

	t.Run("empty mount path rejected", func(t *testing.T) {
		_, err := svcs.Volumes.Attach(ctx, vol.ID, parseUUID(t, svcID), "")
		require.Error(t, err)
	})

	t.Run("relative mount path rejected", func(t *testing.T) {
		_, err := svcs.Volumes.Attach(ctx, vol.ID, parseUUID(t, svcID), "data/relative")
		require.Error(t, err)
	})
}
