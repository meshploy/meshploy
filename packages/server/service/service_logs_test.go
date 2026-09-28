package service_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Stopping a service deletes its pod and the pod's logs; the Logs tab then
// shows what it wrote last, and says when it stopped. A pod waiting to
// restart after a crash shows the run that exited, which is what explains it.
func TestAStoppedServiceStillShowsItsLastLogs(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, Image: "app:1"})
	require.NoError(t, err)
	var got meshdb.Service
	require.NoError(t, e.gdb.First(&got, "id = ?", svc.ID).Error)
	slug := got.Slug
	if slug == "" {
		slug = "api"
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: slug + "-abc", Namespace: e.project.Slug,
			Labels: map[string]string{"app": slug, "managed-by": "meshploy"}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", RestartCount: 300,
			State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 3, Reason: "Error"}},
		}}},
	}
	client := fake.NewSimpleClientset(pod)
	service.UseK8sForTest(e.svcs, client)

	stream := func() string {
		var buf bytes.Buffer
		require.NoError(t, e.svcs.Deployments.StreamRuntimeLogs(ctx, svc.ID, service.LogOptions{}, &buf, func() {}))
		return buf.String()
	}
	assert.Contains(t, stream(), "exited with code 3; these are the lines of the run that exited")

	e.svcs.Deployments.KeepLastLogs(ctx, svc.ID)
	require.NoError(t, client.CoreV1().Pods(e.project.Slug).Delete(ctx, pod.Name, metav1.DeleteOptions{}))

	out := stream()
	assert.Contains(t, out, "No pod is running. These are the last lines it wrote before it was stopped")
	assert.Contains(t, out, "fake logs", "the fake cluster's log body, as it was kept")

	text, err := e.svcs.Deployments.FetchRuntimeLogs(ctx, svc.ID, service.LogOptions{})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(text, "No pod is running."), "the download says so too")

	// Keeping again with no pod keeps what was kept, not the note about it.
	e.svcs.Deployments.KeepLastLogs(ctx, svc.ID)
	require.NoError(t, e.gdb.First(&got, "id = ?", svc.ID).Error)
	assert.NotContains(t, got.LastLogs, "No pod is running")
	assert.WithinDuration(t, time.Now(), *got.LastLogsAt, time.Minute)
}
