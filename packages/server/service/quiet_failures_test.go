package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Before a database goes, the console can say who reads it: production's web,
// and staging's web, which uses production's because staging has none. Once
// staging has its own, staging's web is no longer one of them.
func TestADatabasesDependentsAreEveryLevelThatReadsIt(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, web, pg := newChain(t)
	published := meshdb.VariableGroup{ProjectID: prod, ServiceID: &pg.ID, Name: "pg", SystemManaged: true}
	require.NoError(t, gdb.Create(&published).Error)
	require.NoError(t, gdb.Create(&meshdb.ServiceVariableGroup{ServiceID: web.ID, GroupID: published.ID}).Error)
	webS1 := meshdb.Service{ProjectID: s1, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication, LineageID: &web.ID}
	require.NoError(t, gdb.Create(&webS1).Error)
	require.NoError(t, gdb.Create(&meshdb.ServiceVariableGroup{ServiceID: webS1.ID, GroupID: published.ID}).Error)
	job := meshdb.Job{ProjectID: prod, Name: "migrate", Image: "migrate"}
	require.NoError(t, gdb.Create(&job).Error)
	require.NoError(t, gdb.Create(&meshdb.JobVariableGroup{JobID: job.ID, GroupID: published.ID}).Error)

	deps, err := svcs.VariableGroups.Dependents(ctx, pg.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []service.Dependent{
		{Kind: "service", Name: "web", Level: "production"},
		{Kind: "service", Name: "web", Level: "staging1"},
		{Kind: "job", Name: "migrate", Level: "production"},
	}, deps)

	// staging1 gets its own pg, running and published.
	pgS1 := meshdb.Service{ProjectID: s1, Name: "pg", Slug: "pg", Type: meshdb.ServiceTypeDatabase, LineageID: &pg.ID}
	require.NoError(t, gdb.Create(&pgS1).Error)
	require.NoError(t, gdb.Create(&meshdb.VariableGroup{ProjectID: s1, ServiceID: &pgS1.ID, Name: "pg-s1", SystemManaged: true}).Error)
	deps, err = svcs.VariableGroups.Dependents(ctx, pg.ID)
	require.NoError(t, err)
	assert.NotContains(t, deps, service.Dependent{Kind: "service", Name: "web", Level: "staging1"})

	deps, err = svcs.VariableGroups.Dependents(ctx, web.ID)
	require.NoError(t, err)
	assert.Empty(t, deps, "web publishes nothing anyone reads")
}

// Deleting a project takes what it runs out of the cluster, not only its rows.
func TestDeletingAProjectClearsItsNamespace(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, orgID, "Shop", "shop")
	require.NoError(t, err)
	require.NoError(t, gdb.Create(&meshdb.Service{ProjectID: project.ID, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication}).Error)
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: project.Slug}})
	service.UseWorkloadK8sForTest(svcs, client)

	level, err := svcs.Projects.CreateLevel(ctx, project.ID, "staging", project.ID, false)
	require.NoError(t, err)
	assert.ErrorIs(t, svcs.Promotions.DeleteProject(ctx, project.ID), service.ErrProjectHasLevels)
	_, err = client.CoreV1().Namespaces().Get(ctx, project.Slug, metav1.GetOptions{})
	require.NoError(t, err, "refused before anything was touched")

	require.NoError(t, svcs.Promotions.DeleteProject(ctx, level.ID))
	require.NoError(t, svcs.Promotions.DeleteProject(ctx, project.ID))
	_, err = client.CoreV1().Namespaces().Get(ctx, project.Slug, metav1.GetOptions{})
	assert.Error(t, err, "the namespace is gone")
	var count int64
	gdb.Model(&meshdb.Service{}).Where("project_id = ?", project.ID).Count(&count)
	assert.Zero(t, count)
}

// A job whose variables cannot be read does not run without them: its run
// fails, and says why.
func TestAJobThatCannotReadItsVariablesDoesNotRun(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, _, _ := newChain(t)
	service.UseJobK8sForTest(svcs, fake.NewSimpleClientset())
	shared := meshdb.VariableGroup{ProjectID: s1, Name: "flags"}
	require.NoError(t, gdb.Create(&shared).Error)
	job := meshdb.Job{ProjectID: prod, Name: "report", Image: "busybox", K8sName: "report-1"}
	require.NoError(t, gdb.Create(&job).Error)
	require.NoError(t, gdb.Create(&meshdb.JobVariableGroup{JobID: job.ID, GroupID: shared.ID}).Error)

	run, err := svcs.Jobs.Trigger(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, meshdb.JobStatusFailed, run.Status)
	assert.Contains(t, run.Log, "the flags variable group belongs to staging1")
}
