package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func pod(name, app string, restarts int32, last *corev1.ContainerStateTerminated, waiting *corev1.ContainerStateWaiting, memLimit string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": app, "managed-by": "meshploy"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: app}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name: app, RestartCount: restarts, State: corev1.ContainerState{Waiting: waiting},
		}}},
	}
	if last != nil {
		p.Status.ContainerStatuses[0].LastTerminationState.Terminated = last
	}
	if memLimit != "" {
		p.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memLimit)}
	}
	return p
}

// A service that keeps dying says why: an out-of-memory kill with the limit
// it hit, a process that keeps exiting with its code, an image that cannot be
// pulled. One that restarted days ago and is fine now says nothing. The
// overview names each, first.
func TestServicesSayWhyTheyKeepDying(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, orgID, "DocAI", "docai")
	require.NoError(t, err)
	mk := func(name string) meshdb.Service {
		s := meshdb.Service{ProjectID: project.ID, Name: name, Slug: name, Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceRunning}
		require.NoError(t, gdb.Create(&s).Error)
		return s
	}
	backend, worker, web, old := mk("backend"), mk("worker"), mk("web"), mk("old")

	recent := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	long := metav1.NewTime(time.Now().Add(-72 * time.Hour))
	pods := []*corev1.Pod{
		pod("backend-1", "backend", 6, &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: recent}, nil, "512Mi"),
		pod("worker-1", "worker", 9, &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1, FinishedAt: recent},
			&corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}, ""),
		pod("web-1", "web", 0, nil, &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "manifest unknown"}, ""),
		pod("old-1", "old", 2, &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1, FinishedAt: long}, nil, ""),
	}
	var objs []runtime.Object
	for _, p := range pods {
		p.Namespace = project.Slug
		objs = append(objs, p)
	}
	client := fake.NewSimpleClientset(objs...)
	service.UseWorkloadsK8sForTroubleTest(svcs, client)

	troubles, err := svcs.Workloads.Troubles(ctx, project.ID)
	require.NoError(t, err)
	require.NotNil(t, troubles[backend.ID])
	assert.Equal(t, service.TroubleOutOfMemory, troubles[backend.ID].Kind)
	assert.Equal(t, int32(6), troubles[backend.ID].Restarts)
	assert.Equal(t, "512Mi", troubles[backend.ID].MemoryLimit)
	require.NotNil(t, troubles[worker.ID])
	assert.Equal(t, service.TroubleCrashing, troubles[worker.ID].Kind)
	assert.Equal(t, int32(1), troubles[worker.ID].ExitCode)
	require.NotNil(t, troubles[web.ID])
	assert.Equal(t, service.TroubleImagePull, troubles[web.ID].Kind)
	assert.Equal(t, "manifest unknown", troubles[web.ID].Message)
	assert.Nil(t, troubles[old.ID], "restarts from days ago, and fine now")

	overview, err := svcs.Overview.Get(ctx, orgID, []uuid.UUID{project.ID}, true)
	require.NoError(t, err)
	titles := map[string]string{}
	for _, a := range overview.Attention {
		if a.Kind == "service_trouble" {
			titles[a.Title] = a.Detail
		}
	}
	assert.Equal(t, "6 restarts; memory limit 512Mi; in DocAI", titles["backend keeps running out of memory"])
	assert.Contains(t, titles, "worker keeps stopping")
	assert.Contains(t, titles, "web cannot pull its image")
	assert.NotContains(t, titles, "old keeps stopping")
}
