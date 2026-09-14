package k8s

import (
	"testing"

	"github.com/meshploy/packages/db"
	"k8s.io/client-go/kubernetes/fake"
)

// A mesh-only node is not in the cluster, so applying its role touches nothing
// and does not fail on the missing node object.
func TestSetNodeMeshRoleIgnoresMeshOnlyNodes(t *testing.T) {
	if err := SetNodeMeshRole(t.Context(), fake.NewSimpleClientset(), "edge-box", db.MeshRoleMesh); err != nil {
		t.Fatalf("mesh role: %v", err)
	}
	if err := SetNodeMeshRole(t.Context(), fake.NewSimpleClientset(), "missing", db.MeshRoleWorkload); err == nil {
		t.Fatal("a cluster role on a missing node should still fail")
	}
}
