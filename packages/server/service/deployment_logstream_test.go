package service_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupDeploymentTest is setupStackTest plus the *gorm.DB, which these tests
// need in order to drive a deployment's stored log directly -- there is no
// service-level API for "append a line as the rollout would".
func setupDeploymentTest(t *testing.T) (*service.Services, *gorm.DB, uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	user, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "deployuser",
		Email:    "deployuser@example.com",
		Password: "pass",
	})
	require.NoError(t, err)

	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)

	proj, err := svcs.Projects.Create(ctx, org.ID, "deploy-project", "deploy-project")
	require.NoError(t, err)

	return svcs, db, proj.ID, user.ID.String()
}

// syncBuf is an io.Writer the test can read while the stream is still writing.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// An image-based deploy has no build job, so there is no pod to follow. The
// stream used to sit on waitForBuildPod for 60s printing "Waiting for build pod
// (job: )…" while the rollout wrote its real output to the stored log, which
// only appeared once the page was reloaded and the finished deployment took the
// replay path. Every stack service deploys this way.
func TestStreamBuildLogsTailsStoredLogWhenThereIsNoBuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	svcs, db, pid, userID := setupDeploymentTest(t)

	svc, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{
		Name: "log-stream-app", Image: "nginx:alpine",
	})
	require.NoError(t, err)

	dep := meshdb.Deployment{
		ServiceID: svc.ID,
		Status:    meshdb.DeploymentDeploying,
		Image:     "nginx:alpine",
		Log:       "Direct image deploy triggered by user " + userID + "\n",
	}
	require.NoError(t, db.Create(&dep).Error)

	out := &syncBuf{}
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_ = svcs.Deployments.StreamBuildLogs(ctx, dep.ID, out, func() {})
	}()

	// The first poll must emit what is already stored, without waiting for a
	// build pod that will never exist.
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Direct image deploy triggered by user")
	}, 10*time.Second, 100*time.Millisecond, "stored log was not streamed; got: %s", out.String())

	assert.NotContains(t, out.String(), "Waiting for build pod",
		"an image deploy has no build job to wait for")

	// A line appended mid-rollout must arrive without a reload.
	require.NoError(t, db.Model(&meshdb.Deployment{}).Where("id = ?", dep.ID).
		Update("log", dep.Log+"Rolling out to cluster…\n").Error)
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Rolling out to cluster")
	}, 10*time.Second, 100*time.Millisecond, "a line appended while streaming never arrived")

	// Finishing the deployment closes the stream.
	require.NoError(t, db.Model(&meshdb.Deployment{}).Where("id = ?", dep.ID).
		Updates(map[string]any{
			"status": meshdb.DeploymentSuccess,
			"log":    dep.Log + "Rolling out to cluster…\nAll replicas healthy.\n",
		}).Error)

	select {
	case <-streamDone:
	case <-time.After(15 * time.Second):
		t.Fatal("stream did not close when the deployment finished")
	}

	final := out.String()
	assert.Contains(t, final, "All replicas healthy.")
	assert.Contains(t, final, "event: done")
	// Each line exactly once — a poll that re-sent from the start would repeat.
	assert.Equal(t, 1, strings.Count(final, "Rolling out to cluster…"))
}

// A half-written line must not be rendered and then completed on the next tick:
// in a terminal that reads as corrupted output.
func TestStreamBuildLogsHoldsBackPartialLines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	svcs, db, pid, _ := setupDeploymentTest(t)

	svc, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{
		Name: "partial-line-app", Image: "nginx:alpine",
	})
	require.NoError(t, err)

	dep := meshdb.Deployment{
		ServiceID: svc.ID,
		Status:    meshdb.DeploymentDeploying,
		Image:     "nginx:alpine",
		Log:       "first line\nsecond line without a newline yet",
	}
	require.NoError(t, db.Create(&dep).Error)

	out := &syncBuf{}
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_ = svcs.Deployments.StreamBuildLogs(ctx, dep.ID, out, func() {})
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "first line")
	}, 10*time.Second, 100*time.Millisecond)
	assert.NotContains(t, out.String(), "second line without a newline yet",
		"an incomplete line must be held back until it is terminated")

	require.NoError(t, db.Model(&meshdb.Deployment{}).Where("id = ?", dep.ID).
		Updates(map[string]any{
			"status": meshdb.DeploymentSuccess,
			"log":    "first line\nsecond line without a newline yet, now complete\n",
		}).Error)

	select {
	case <-streamDone:
	case <-time.After(15 * time.Second):
		t.Fatal("stream did not close")
	}
	assert.Contains(t, out.String(), "now complete")
	assert.Equal(t, 1, strings.Count(out.String(), "first line"))
}
