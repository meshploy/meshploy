package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A restart ends what followed each deployment in flight. Picked up again,
// one that cannot be followed ends saying why, instead of staying "pending"
// or "deploying" for good.
func TestAnInterruptedDeploymentEndsSayingWhy(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{})
	org := meshdb.Organization{Name: "r", Slug: "r-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	proj := meshdb.Project{OrganizationID: org.ID, Name: "p", Slug: "p-" + uuid.NewString()[:6]}
	require.NoError(t, gdb.Create(&proj).Error)
	svc := meshdb.Service{ProjectID: proj.ID, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication}
	require.NoError(t, gdb.Create(&svc).Error)

	// The cluster runs an older image of it than the deployment was rolling out.
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: proj.Slug},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web", Image: "registry/web:old"}},
		}}},
	})
	service.UseK8sForTest(svcs, client)

	queued := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentPending, BuildJobName: "build-web-gone", Image: "registry/web:new", Log: "Build triggered\n"}
	rolling := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentDeploying, Image: "registry/web:new2", Log: "Deploying\n"}
	nothing := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentPending, Image: "registry/web:new3"}
	for _, d := range []*meshdb.Deployment{&queued, &rolling, &nothing} {
		require.NoError(t, gdb.Create(d).Error)
	}

	svcs.Deployments.ResumeInFlight(ctx)

	for d, want := range map[uuid.UUID]string{
		queued.ID:  "its build job is gone",
		rolling.ID: "it had not reached the cluster yet",
		nothing.ID: "nothing in the cluster to follow",
	} {
		var got meshdb.Deployment
		require.Eventually(t, func() bool {
			require.NoError(t, gdb.First(&got, "id = ?", d).Error)
			return got.Status == meshdb.DeploymentFailed
		}, 5*time.Second, 50*time.Millisecond)
		if !strings.Contains(got.Log, "The API restarted") || !strings.Contains(got.Log, want) {
			t.Errorf("log does not say why:\n%s", got.Log)
		}
	}
}
