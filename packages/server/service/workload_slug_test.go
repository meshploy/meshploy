package service_test

import (
	"context"
	"strings"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The namespace is the project, so two services sharing a Kubernetes name
// resolve to one Deployment: deploying the same template twice had the second
// silently overwrite the first. The first keeps the plain name — a suffix on
// every workload would be noise for the single-instance case that is normal.
func TestWorkloadSlugSuffixesOnlyOnCollision(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	first, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{Name: "zot", Image: "zot:latest"})
	require.NoError(t, err)
	assert.Equal(t, "zot", first.Slug, "the first instance should be addressable as plain zot")

	second, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{Name: "zot", Image: "zot:latest"})
	require.NoError(t, err)
	assert.NotEqual(t, first.Slug, second.Slug, "two services must never share a Kubernetes name")
	assert.True(t, strings.HasPrefix(second.Slug, "zot-"), "the suffix should keep the name recognisable, got %q", second.Slug)
}

// A service created before the slug column exists has an empty slug and is
// running under its display name. It must keep that name — and it must still
// block a new service from claiming it, or the new one would adopt the running
// workload.
func TestLegacyServiceKeepsItsNameAndBlocksIt(t *testing.T) {
	ctx := context.Background()
	svcs, db, pid, _ := setupDeploymentTest(t)
	_ = svcs

	legacy, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{Name: "legacy-app", Image: "nginx:alpine"})
	require.NoError(t, err)
	// Simulate a pre-slug row.
	require.NoError(t, db.Model(&meshdb.Service{}).Where("id = ?", legacy.ID).Update("slug", "").Error)

	next, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{Name: "legacy-app", Image: "nginx:alpine"})
	require.NoError(t, err)
	assert.NotEqual(t, "legacy-app", next.Slug,
		"a pre-slug service still occupies its name in the cluster and must not be adopted")

	// And the legacy row's own Kubernetes name is unchanged.
	ns, k8sName, err := svcs.Workloads.GetK8sInfo(ctx, legacy.ID)
	require.NoError(t, err)
	assert.Equal(t, "legacy-app", k8sName, "an existing workload must never be renamed by this change")
	assert.NotEmpty(t, ns)
}

// Once the name is stored, renaming a service is a display change only. It used
// to move the Deployment, which meant delete-and-recreate and an orphan left
// behind whenever that failed partway.
func TestRenameDoesNotMoveTheClusterWorkload(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	svc, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{Name: "api", Image: "nginx:alpine"})
	require.NoError(t, err)
	require.Equal(t, "api", svc.Slug)

	newName := "gateway"
	updated, err := svcs.Workloads.Update(ctx, svc.ID, service.UpdateWorkloadInput{Name: &newName})
	require.NoError(t, err)
	assert.Equal(t, "gateway", updated.Name)
	assert.Equal(t, "api", updated.Slug, "the Kubernetes name is fixed at creation")

	_, k8sName, err := svcs.Workloads.GetK8sInfo(ctx, svc.ID)
	require.NoError(t, err)
	assert.Equal(t, "api", k8sName)
}
