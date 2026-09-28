package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A build with no registry linked pushes to the organization's own - its
// built-in one - as a stack's build: service does, which nobody is asked to
// choose a registry for. One linked by hand still wins, and with several and
// no built-in, the build says to choose.
func TestABuildWithNoRegistryUsesTheOrganizations(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, Image: "app:1"})
	require.NoError(t, err)
	bc := &meshdb.BuildConfig{ServiceID: svc.ID}

	_, err = service.ResolveRegistryForTest(e.svcs, ctx, bc)
	assert.ErrorContains(t, err, "no container registry configured", "none at all")

	other := meshdb.RegistryIntegration{OrganizationID: e.org.ID, Name: "ghcr", Provider: "ghcr", Endpoint: "ghcr.io"}
	require.NoError(t, e.gdb.Create(&other).Error)
	host, err := service.ResolveRegistryForTest(e.svcs, ctx, bc)
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io", host, "the only one")

	builtin := meshdb.RegistryIntegration{OrganizationID: e.org.ID, Name: "built-in", Provider: meshdb.RegistryBuiltin, Endpoint: "100.64.0.1:5000"}
	require.NoError(t, e.gdb.Create(&builtin).Error)
	host, err = service.ResolveRegistryForTest(e.svcs, ctx, bc)
	require.NoError(t, err)
	assert.Equal(t, "100.64.0.1:5000", host, "the built-in one, among several")

	bc.RegistryIntegrationID = &other.ID
	host, err = service.ResolveRegistryForTest(e.svcs, ctx, bc)
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io", host, "one linked by hand")
}

// A service still running what it ran before says its latest deployment
// failed, until one succeeds.
func TestAFailedDeploymentIsRememberedUntilOneSucceeds(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, service.CreateWorkloadInput{
		Name: "api", Type: meshdb.ServiceTypeApplication, Image: "app:1"})
	require.NoError(t, err)
	dep := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentBuilding}
	require.NoError(t, e.gdb.Create(&dep).Error)

	service.FailDeploymentForTest(e.svcs, dep.ID, "build failed")
	var got meshdb.Service
	require.NoError(t, e.gdb.First(&got, "id = ?", svc.ID).Error)
	assert.True(t, got.LatestDeployFailed)
	assert.Equal(t, meshdb.ServiceFailed, got.Status, "never deployed: nothing runs, so it failed")

	// One that has reached the cluster keeps running what it ran: a failed
	// build never touches the cluster, and "stopped" read as if it were down.
	// Started from an image without a deploy - as a migration starts one -
	// counts: its spec was applied.
	require.NoError(t, e.gdb.Model(&got).Updates(map[string]any{"deployed_spec_hash": "abc", "status": meshdb.ServiceDeploying}).Error)
	again := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentBuilding}
	require.NoError(t, e.gdb.Create(&again).Error)
	service.FailDeploymentForTest(e.svcs, again.ID, "build failed")
	require.NoError(t, e.gdb.First(&got, "id = ?", svc.ID).Error)
	assert.Equal(t, meshdb.ServiceRunning, got.Status)
	assert.True(t, got.LatestDeployFailed)

	next := meshdb.Deployment{ServiceID: svc.ID, Status: meshdb.DeploymentDeploying}
	require.NoError(t, e.gdb.Create(&next).Error)
	service.SucceedDeploymentForTest(e.svcs, ctx, next.ID, svc.ID, "app:2")
	require.NoError(t, e.gdb.First(&got, "id = ?", svc.ID).Error)
	assert.False(t, got.LatestDeployFailed)
}
