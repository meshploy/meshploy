package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A job that works on a database attaches that database's group rather than
// carrying its own copy of the password: the copy is what goes stale when the
// database is reset.
func TestJobReadsAnAttachedGroup(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "jobs", Slug: "jobs"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "jobs", "jobs")
	require.NoError(t, err)

	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "Primary DB", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)

	// The database's own group, generated when it was created.
	var group meshdb.VariableGroup
	require.NoError(t, gdb.Where("service_id = ?", database.ID).First(&group).Error)

	job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
		ProjectID: project.ID, Name: "migrate", Image: "migrate:latest",
		EnvVars: "DATABASE_URL=${PRIMARY_DB_URL}",
	})
	require.NoError(t, err)

	// Nothing attached yet: the job sees only its own variables.
	env, err := svcs.VariableGroups.CollectEnvVarsForJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Empty(t, env)

	require.NoError(t, svcs.VariableGroups.AttachJob(ctx, job.ID, group.ID))

	groups, err := svcs.VariableGroups.ListForJob(ctx, job.ID)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, group.ID, groups[0].ID)

	env, err = svcs.VariableGroups.CollectEnvVarsForJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Contains(t, env, "PRIMARY_DB_URL", "the group the job attached carries the connection it references")
	assert.Contains(t, env["PRIMARY_DB_URL"], "postgresql://app:pass@")

	// Attaching twice is the same as attaching once.
	require.NoError(t, svcs.VariableGroups.AttachJob(ctx, job.ID, group.ID))
	groups, err = svcs.VariableGroups.ListForJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Len(t, groups, 1)

	require.NoError(t, svcs.VariableGroups.DetachJob(ctx, job.ID, group.ID))
	groups, err = svcs.VariableGroups.ListForJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

// A service may not detach its own generated group; a job has no group of its
// own, so nothing is pinned to it and detaching always works.
func TestJobAttachmentsAreNotPinned(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "pin", Slug: "pin"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "pin", "pin")
	require.NoError(t, err)

	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "DB", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "pass",
	})
	require.NoError(t, err)
	var group meshdb.VariableGroup
	require.NoError(t, gdb.Where("service_id = ?", database.ID).First(&group).Error)

	require.Error(t, svcs.VariableGroups.Detach(ctx, database.ID, group.ID),
		"a database keeps its own group")

	job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{
		ProjectID: project.ID, Name: "task", Image: "alpine",
	})
	require.NoError(t, err)
	require.NoError(t, svcs.VariableGroups.AttachJob(ctx, job.ID, group.ID))
	require.NoError(t, svcs.VariableGroups.DetachJob(ctx, job.ID, group.ID))
}
