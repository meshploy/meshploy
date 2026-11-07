package service_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nothing can be pinned to a mesh-only node: it is not in the cluster. The
// refusal is a 400 naming the node; a cluster node still takes a pin.
func TestNothingIsPinnedToAMeshOnlyNode(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	org := meshdb.Organization{Name: "pins", Slug: "pins"}
	require.NoError(t, gdb.Create(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "pins", "pins")
	require.NoError(t, err)

	mesh := meshdb.Node{OrganizationID: org.ID, Name: "edge-box", TailscaleIP: "100.64.0.50", K3sRole: meshdb.K3sRoleAgent, MeshRole: meshdb.MeshRoleMesh}
	worker := meshdb.Node{OrganizationID: org.ID, Name: "worker", TailscaleIP: "100.64.0.51", K3sRole: meshdb.K3sRoleAgent, MeshRole: meshdb.MeshRoleWorkload}
	require.NoError(t, gdb.Create(&mesh).Error)
	require.NoError(t, gdb.Create(&worker).Error)

	refused := func(what string, err error) {
		t.Helper()
		var se huma.StatusError
		if assert.True(t, errors.As(err, &se), "%s: %v", what, err) {
			assert.Equal(t, http.StatusBadRequest, se.GetStatus(), what)
			assert.Contains(t, se.Error(), "edge-box", what)
		}
	}

	_, err = svcs.Workloads.Create(ctx, proj.ID, service.CreateWorkloadInput{Name: "api", Image: "nginx:alpine", NodeID: &mesh.ID})
	refused("service", err)
	_, err = svcs.Volumes.Create(ctx, proj.ID, "data", 5, &mesh.ID)
	refused("volume", err)
	_, err = svcs.Jobs.Create(ctx, service.CreateJobInput{ProjectID: proj.ID, Name: "nightly", Image: "alpine", NodeID: &mesh.ID})
	refused("job", err)

	svc, err := svcs.Workloads.Create(ctx, proj.ID, service.CreateWorkloadInput{Name: "web", Image: "nginx:alpine", NodeID: &worker.ID})
	require.NoError(t, err, "a cluster node still takes a pin")
	_, err = svcs.Workloads.Update(ctx, svc.ID, service.UpdateWorkloadInput{UpdateNode: true, NodeID: &mesh.ID})
	refused("moving a service", err)
}

// Nothing is pinned to, built on, or routed to another organization's node:
// one installation can hold several organizations' machines in one cluster,
// and a project reaches only its own organization's. An unknown node is
// refused the same way.
func TestNothingIsPlacedOnAnotherOrganizationsNode(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	home := meshdb.Organization{Name: "home", Slug: "home"}
	away := meshdb.Organization{Name: "away", Slug: "away"}
	require.NoError(t, gdb.Create(&home).Error)
	require.NoError(t, gdb.Create(&away).Error)
	proj, err := svcs.Projects.Create(ctx, home.ID, "shop", "shop")
	require.NoError(t, err)

	mine := meshdb.Node{OrganizationID: home.ID, Name: "mine", TailscaleIP: "100.64.0.60", K3sRole: meshdb.K3sRoleAgent, MeshRole: meshdb.MeshRoleWorkloadBuilder}
	theirs := meshdb.Node{OrganizationID: away.ID, Name: "theirs", TailscaleIP: "100.64.0.61", K3sRole: meshdb.K3sRoleAgent, MeshRole: meshdb.MeshRoleWorkloadBuilder}
	require.NoError(t, gdb.Create(&mine).Error)
	require.NoError(t, gdb.Create(&theirs).Error)
	unknown := uuid.New()

	refused := func(what string, err error) {
		t.Helper()
		var se huma.StatusError
		if assert.True(t, errors.As(err, &se), "%s: %v", what, err) {
			assert.Equal(t, http.StatusBadRequest, se.GetStatus(), what)
			assert.Contains(t, se.Error(), "no such node in this organization", what)
		}
	}

	for _, id := range []uuid.UUID{theirs.ID, unknown} {
		id := id
		_, err = svcs.Workloads.Create(ctx, proj.ID, service.CreateWorkloadInput{Name: "api", Image: "nginx:alpine", NodeID: &id})
		refused("service", err)
		_, err = svcs.Volumes.Create(ctx, proj.ID, "data", 5, &id)
		refused("volume", err)
		_, err = svcs.Jobs.Create(ctx, service.CreateJobInput{ProjectID: proj.ID, Name: "nightly", Image: "alpine", NodeID: &id})
		refused("job", err)
		_, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: home.ID, ProjectID: proj.ID, Hostname: "nas.home.example.com",
			Targets: []service.TargetInput{{Path: "/", NodeID: &id, Port: 8080}}})
		refused("route target", err)
	}

	// Moving what is already placed is refused the same way.
	svc, err := svcs.Workloads.Create(ctx, proj.ID, service.CreateWorkloadInput{Name: "web", Image: "nginx:alpine", NodeID: &mine.ID})
	require.NoError(t, err, "its own organization's node takes a pin")
	_, err = svcs.Workloads.Update(ctx, svc.ID, service.UpdateWorkloadInput{UpdateNode: true, NodeID: &theirs.ID})
	refused("moving a service", err)
	vol, err := svcs.Volumes.Create(ctx, proj.ID, "cache", 5, &mine.ID)
	require.NoError(t, err)
	_, err = svcs.Volumes.SetNode(ctx, vol.ID, &theirs.ID)
	refused("moving a volume", err)
	job, err := svcs.Jobs.Create(ctx, service.CreateJobInput{ProjectID: proj.ID, Name: "hourly", Image: "alpine", NodeID: &mine.ID})
	require.NoError(t, err)
	_, err = svcs.Jobs.Update(ctx, job.ID, service.UpdateJobInput{NodeID: &theirs.ID})
	refused("moving a job", err)
	_, err = svcs.Routes.Create(ctx, service.CreateRouteInput{OrgID: home.ID, ProjectID: proj.ID, Hostname: "own.home.example.com",
		Targets: []service.TargetInput{{Path: "/", NodeID: &mine.ID, Port: 8080}}})
	require.NoError(t, err, "a route reaches its own organization's node")

	// Builds: the pin is a cluster node name.
	other := "theirs"
	_, err = svcs.Workloads.UpsertBuildConfig(ctx, svc.ID, service.UpdateBuildConfigInput{BuilderNode: &other})
	refused("pinning builds", err)
	own, none := "mine", ""
	_, err = svcs.Workloads.UpsertBuildConfig(ctx, svc.ID, service.UpdateBuildConfigInput{BuilderNode: &own})
	require.NoError(t, err, "builds pin to its own organization's node")
	_, err = svcs.Workloads.UpsertBuildConfig(ctx, svc.ID, service.UpdateBuildConfigInput{BuilderNode: &none})
	require.NoError(t, err, "and unpin")

	var kept meshdb.Service
	require.NoError(t, gdb.First(&kept, "id = ?", svc.ID).Error)
	assert.Equal(t, &mine.ID, kept.NodeID, "the refused move left the pin")
}
