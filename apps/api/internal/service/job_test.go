package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupJobTest registers a user, retrieves the auto-created org, and creates a project.
// Returns the services aggregate, the gorm.DB, org ID, and project ID.
func setupJobTest(t *testing.T) (*service.Services, *gorm.DB, string, string) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	user, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "jobuser",
		Email:    "jobuser@example.com",
		Password: "pass",
	})
	require.NoError(t, err)

	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)

	proj, err := svcs.Projects.Create(ctx, org.ID, "test-project", "test-project")
	require.NoError(t, err)

	return svcs, db, org.ID.String(), proj.ID.String()
}

func TestJobCreate(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID := setupJobTest(t)
	pid := parseUUID(t, projID)

	t.Run("one-shot job created idle", func(t *testing.T) {
		job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
			ProjectID: pid,
			Name:      "my-job",
			IsCron:    false,
			Image:     "alpine:latest",
			Command:   "echo hello",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, job.ID)
		assert.Equal(t, "my-job", job.Name)
		assert.False(t, job.IsCron)
		assert.Equal(t, meshdb.JobStatusIdle, job.Status)
		assert.NotEmpty(t, job.K8sName)
	})

	t.Run("cron job gets schedule and policy defaults", func(t *testing.T) {
		job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
			ProjectID: pid,
			Name:      "my-cron",
			IsCron:    true,
			Image:     "alpine:latest",
			Schedule:  "0 2 * * *",
		})
		require.NoError(t, err)
		assert.True(t, job.IsCron)
		assert.Equal(t, "0 2 * * *", job.Schedule)
		assert.Equal(t, meshdb.ConcurrencyAllow, job.ConcurrencyPolicy)
		assert.Equal(t, 5, job.HistoryLimit)
	})

	t.Run("resource defaults applied when omitted", func(t *testing.T) {
		job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
			ProjectID: pid,
			Name:      "defaults-job",
			Image:     "alpine:latest",
		})
		require.NoError(t, err)
		assert.Equal(t, "100m", job.CPURequest)
		assert.Equal(t, "500m", job.CPULimit)
		assert.Equal(t, "128Mi", job.MemoryRequest)
		assert.Equal(t, "512Mi", job.MemoryLimit)
	})
}

func TestJobTrigger(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID := setupJobTest(t)

	job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
		ProjectID: parseUUID(t, projID),
		Name:      "trigger-me",
		Image:     "alpine:latest",
		Command:   "echo hi",
	})
	require.NoError(t, err)

	t.Run("creates run in pending state without k8s", func(t *testing.T) {
		run, err := svcs.Jobs.Trigger(ctx, job.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, run.ID)
		assert.Equal(t, meshdb.JobStatusPending, run.Status)
		assert.NotEmpty(t, run.K8sJobName)
	})
}

func TestJobDelete(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID := setupJobTest(t)

	job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
		ProjectID: parseUUID(t, projID),
		Name:      "to-delete",
		Image:     "alpine:latest",
	})
	require.NoError(t, err)

	require.NoError(t, svcs.Jobs.Delete(ctx, job.ID))

	_, err = svcs.Jobs.Get(ctx, job.ID, parseUUID(t, projID))
	require.Error(t, err)
}

func TestJobList(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, projID := setupJobTest(t)
	pid := parseUUID(t, projID)

	for i := 0; i < 3; i++ {
		_, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
			ProjectID: pid,
			Name:      fmt.Sprintf("job-%d", i),
			Image:     "alpine:latest",
		})
		require.NoError(t, err)
	}

	jobs, err := svcs.Jobs.List(ctx, pid)
	require.NoError(t, err)
	assert.Len(t, jobs, 3)
}
