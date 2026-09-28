package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A service made on its own keeps its last three images for rollback, not
// every image it ever builds.
func TestAServiceMadeOnItsOwnKeepsItsLastThreeImages(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, GitRepo: "acme/api"})
	require.NoError(t, err)
	bc, err := e.svcs.Workloads.GetBuildConfig(ctx, svc.ID)
	require.NoError(t, err)
	assert.True(t, bc.RollbackEnabled)
	assert.Equal(t, 3, bc.ImageRetention)

	// One given a build later, from its settings, keeps them the same way.
	img, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "web", Type: meshdb.ServiceTypeApplication, Image: "web:1"})
	require.NoError(t, err)
	repo := "acme/web"
	bc, err = e.svcs.Workloads.UpsertBuildConfig(ctx, img.ID, service.UpdateBuildConfigInput{GitRepo: &repo})
	require.NoError(t, err)
	assert.True(t, bc.RollbackEnabled)
	assert.Equal(t, 3, bc.ImageRetention)

	_, err = e.svcs.Workloads.UpsertBuildConfig(ctx, img.ID, service.UpdateBuildConfigInput{ImageRetention: ptr(0)})
	assert.Error(t, err, "keeping no image would leave nothing running to roll back from")
}

// A stack's services keep images as the stack says, off until it is turned
// on; a service that says otherwise in the file keeps its own setting.
func TestAStacksServicesKeepImagesAsTheStackSays(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	spec := `services:
  api:
    build: .
    x-meshploy:
      source: {git: acme/api}
  worker:
    build: .
    x-meshploy:
      source: {git: acme/worker}
      rollback: {enabled: true, retention: 7}
`
	r, err := e.svcs.Stacks.ApplyManifest(ctx, e.project.ID, service.ManifestInput{Name: "shop", Spec: spec}, e.org.ID)
	require.NoError(t, err)
	require.Empty(t, r.Errors)
	bcOf := func(name string) *meshdb.BuildConfig {
		var svc meshdb.Service
		require.NoError(t, e.gdb.First(&svc, "stack_id = ? AND name = ?", r.Stack.ID, name).Error)
		bc, err := e.svcs.Workloads.GetBuildConfig(ctx, svc.ID)
		require.NoError(t, err)
		return bc
	}
	assert.False(t, bcOf("api").RollbackEnabled, "a stack keeps every image until it says otherwise")
	assert.Equal(t, 7, bcOf("worker").ImageRetention)

	on, keep := true, 4
	_, err = e.svcs.Stacks.Update(ctx, r.Stack.ID, service.UpdateStackInput{RollbackEnabled: &on, ImageRetention: &keep})
	require.NoError(t, err)
	assert.True(t, bcOf("api").RollbackEnabled, "at once, not at the next apply")
	assert.Equal(t, 4, bcOf("api").ImageRetention)
	assert.Equal(t, 7, bcOf("worker").ImageRetention, "its own setting in the file wins")

	// And an apply keeps it so.
	_, err = e.svcs.Stacks.ApplyManifest(ctx, e.project.ID, service.ManifestInput{Name: "shop", Spec: spec}, e.org.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, bcOf("api").ImageRetention)
	assert.Equal(t, 7, bcOf("worker").ImageRetention)
}

// fakeRegistry answers for images like the built-in registry: a manifest
// asked for in the formats a build pushes, and deleted by digest.
type fakeRegistry struct {
	mu      sync.Mutex
	present map[string]bool // "name:tag"
	deleted []string
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	name, ref, _ := strings.Cut(path, "/manifests/")
	switch r.Method {
	case http.MethodHead:
		// A Railpack image is an OCI index: asked for as Docker's alone, the
		// registry has no such manifest.
		if !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") || !f.present[name+":"+ref] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:"+name+"-"+ref)
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		tag := strings.TrimPrefix(ref, "sha256:"+name+"-")
		delete(f.present, name+":"+tag)
		f.deleted = append(f.deleted, name+":"+tag)
		w.WriteHeader(http.StatusAccepted)
	}
}

func deployed(t *testing.T, gdb *gorm.DB, serviceID string, image string, at time.Time) meshdb.Deployment {
	t.Helper()
	d := meshdb.Deployment{ServiceID: parseUUID(t, serviceID), Status: meshdb.DeploymentSuccess, Image: image}
	d.CreatedAt = at
	require.NoError(t, gdb.Create(&d).Error)
	return d
}

// Keeping its last images removes the older ones from the registry, in
// whatever format they were pushed, and marks them removed; an image a recent
// deployment runs again, or another level runs, stays.
func TestKeepingTheLastImagesRemovesTheRest(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	reg := &fakeRegistry{present: map[string]bool{}}
	srv := httptest.NewServer(reg)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	require.NoError(t, e.gdb.Create(&meshdb.RegistryIntegration{OrganizationID: e.org.ID, Name: "built-in",
		Provider: meshdb.RegistryBuiltin, Endpoint: host}).Error)

	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, GitRepo: "acme/api"})
	require.NoError(t, err)
	other, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api-prod", Type: meshdb.ServiceTypeApplication, Image: "x"})
	require.NoError(t, err)

	base := time.Now().Add(-time.Hour)
	var deps []meshdb.Deployment
	for i, tag := range []string{"1", "2", "3", "4", "5", "6"} {
		reg.present["tcp/api:"+tag] = true
		deps = append(deps, deployed(t, e.gdb, svc.ID.String(), host+"/tcp/api:"+tag, base.Add(time.Duration(i)*time.Minute)))
	}
	// Rolled back to 2 most recently: it runs, so it is kept.
	deployed(t, e.gdb, svc.ID.String(), host+"/tcp/api:2", base.Add(10*time.Minute))
	// Another level was promoted to 1.
	deployed(t, e.gdb, other.ID.String(), host+"/tcp/api:1", base)

	service.PruneImagesForTest(e.svcs, ctx, svc.ID)
	assert.ElementsMatch(t, []string{"tcp/api:3", "tcp/api:4"}, reg.deleted,
		"kept: 2 (running again), 6 and 5 (the last three images), and 1 (another level runs it)")

	var removed []string
	e.gdb.Model(&meshdb.Deployment{}).Where("service_id = ? AND image_removed_at IS NOT NULL", svc.ID).Pluck("image", &removed)
	assert.ElementsMatch(t, []string{host + "/tcp/api:3", host + "/tcp/api:4"}, removed)

	bc, err := e.svcs.Workloads.GetBuildConfig(ctx, svc.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, bc.ImagesKept, "1, 2, 5 and 6 can still be rolled back to")
	_ = deps
}

// Once images have been removed, the registry's garbage collection is asked
// of the host agent, at most weekly and never while a build may be pushing.
func TestTheRegistryIsCollectedOnceImagesWereRemoved(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(hostagent.InboxDir(dir), 0o755))
	inbox := func() []string {
		entries, _ := os.ReadDir(hostagent.InboxDir(dir))
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		return names
	}
	now := time.Now()

	require.NoError(t, service.RequestRegistryGCForTest(e.svcs, ctx, dir, now))
	assert.Empty(t, inbox(), "nothing removed, nothing to collect")

	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, GitRepo: "acme/api"})
	require.NoError(t, err)
	gone := deployed(t, e.gdb, svc.ID.String(), "r/api:1", now.Add(-time.Hour))
	require.NoError(t, e.gdb.Model(&gone).Update("image_removed_at", now.Add(-time.Minute)).Error)

	building := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentBuilding}
	require.NoError(t, e.gdb.Create(&building).Error)
	require.NoError(t, service.RequestRegistryGCForTest(e.svcs, ctx, dir, now))
	assert.Empty(t, inbox(), "a build may be pushing")

	require.NoError(t, e.gdb.Model(&building).Update("status", meshdb.DeploymentFailed).Error)
	require.NoError(t, service.RequestRegistryGCForTest(e.svcs, ctx, dir, now))
	require.Len(t, inbox(), 1)
	assert.True(t, strings.HasPrefix(inbox()[0], hostagent.RequestRegistryGC+"-"))
	require.NoError(t, service.RequestRegistryGCForTest(e.svcs, ctx, dir, now))
	assert.Len(t, inbox(), 1, "asked once until the agent takes it")

	// Collected a day ago: not due again for a week.
	require.NoError(t, os.Remove(filepath.Join(hostagent.InboxDir(dir), inbox()[0])))
	require.NoError(t, os.MkdirAll(hostagent.MigrateDir(dir), 0o755))
	done, _ := json.Marshal(hostagent.RegistryGC{FinishedAt: now.Add(-24 * time.Hour)})
	require.NoError(t, os.WriteFile(filepath.Join(hostagent.MigrateDir(dir), hostagent.RegistryGCFile), done, 0o644))
	require.NoError(t, service.RequestRegistryGCForTest(e.svcs, ctx, dir, now))
	assert.Empty(t, inbox())
}

// A level's services made before keeping their last images was the default
// are listed, and moved to it in one step; a stack's are left to its setting.
func TestServicesKeepingEveryImageMoveToTheLastThree(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, GitRepo: "acme/api"})
	require.NoError(t, err)
	require.NoError(t, e.gdb.Model(&meshdb.BuildConfig{}).Where("service_id = ?", svc.ID).
		Update("rollback_enabled", false).Error)

	keeping, err := e.svcs.Workloads.KeepingEveryImage(ctx, e.project.ID)
	require.NoError(t, err)
	require.Len(t, keeping, 1)
	assert.Equal(t, "api", keeping[0].Name)

	_, err = e.svcs.Workloads.KeepLastImages(ctx, e.project.ID)
	require.NoError(t, err)
	keeping, err = e.svcs.Workloads.KeepingEveryImage(ctx, e.project.ID)
	require.NoError(t, err)
	assert.Empty(t, keeping)
}

func ptr[T any](v T) *T { return &v }
