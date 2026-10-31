package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var errHeld = errors.New("held")

type heldProjects struct {
	sync.Mutex
	held map[uuid.UUID]bool
}

func (h *heldProjects) CheckProjectRun(_ context.Context, projectID uuid.UUID) error {
	h.Lock()
	defer h.Unlock()
	if h.held[projectID] {
		return errHeld
	}
	return nil
}

func (h *heldProjects) set(id uuid.UUID, on bool) {
	h.Lock()
	defer h.Unlock()
	h.held[id] = on
}

// A hold on a project reaches its levels and every way of running something,
// and a schedule, which fires in the cluster, is paused there until it ends.
func TestAHeldProjectRunsNothing(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{})
	client := fake.NewSimpleClientset()
	service.UseK8sForTest(svcs, client)
	service.UseJobsK8sForTest(svcs, client)
	hold := &heldProjects{held: map[uuid.UUID]bool{}}
	service.RegisterProjectHold(hold)

	org := meshdb.Organization{Name: "h", Slug: "h-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	proj := meshdb.Project{OrganizationID: org.ID, Name: "p", Slug: "p-" + uuid.NewString()[:6]}
	require.NoError(t, gdb.Create(&proj).Error)
	level := meshdb.Project{OrganizationID: org.ID, Name: "s", Slug: proj.Slug + "-staging", ParentProjectID: &proj.ID}
	require.NoError(t, gdb.Create(&level).Error)
	inLevel := meshdb.Service{ProjectID: level.ID, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication, Image: "registry/web:1", Status: meshdb.ServiceStopped}
	require.NoError(t, gdb.Create(&inLevel).Error)
	done := meshdb.Deployment{ServiceID: inLevel.ID, Status: meshdb.DeploymentSuccess, Image: inLevel.Image}
	require.NoError(t, gdb.Create(&done).Error)
	job := meshdb.Job{ProjectID: proj.ID, Name: "nightly", K8sName: "nightly", Image: "registry/job:1", IsCron: true, Schedule: "0 3 * * *"}
	require.NoError(t, gdb.Create(&job).Error)

	hold.set(proj.ID, true)
	_, err := svcs.Workloads.Start(ctx, inLevel.ID)
	require.ErrorIs(t, err, errHeld, "start")
	_, err = svcs.Deployments.Trigger(ctx, service.TriggerInput{ServiceID: inLevel.ID})
	require.ErrorIs(t, err, errHeld, "deploy")
	_, err = svcs.Deployments.Redeploy(ctx, inLevel.ID)
	require.ErrorIs(t, err, errHeld, "redeploy")
	_, err = svcs.Deployments.Rollback(ctx, done.ID)
	require.ErrorIs(t, err, errHeld, "roll back")
	_, err = svcs.Deployments.DeployImage(ctx, inLevel.ID, "registry/web:2", "promoted", service.Provenance{})
	require.ErrorIs(t, err, errHeld, "deploy an image")
	_, err = svcs.Jobs.Trigger(ctx, job.ID)
	require.ErrorIs(t, err, errHeld, "run a job")

	require.NoError(t, svcs.Jobs.ReapplySchedules(ctx, proj.ID))
	cron, err := client.BatchV1().CronJobs(proj.Slug).Get(ctx, "nightly", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, cron.Spec.Suspend)
	require.True(t, *cron.Spec.Suspend, "a held project's schedule fires")

	hold.set(proj.ID, false)
	require.NoError(t, svcs.Jobs.ReapplySchedules(ctx, proj.ID))
	cron, err = client.BatchV1().CronJobs(proj.Slug).Get(ctx, "nightly", metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, *cron.Spec.Suspend, "a schedule stays paused after the hold")
	_, err = svcs.Workloads.Start(ctx, inLevel.ID)
	require.NoError(t, err, "start after the hold")
}
