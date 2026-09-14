package service_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
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
