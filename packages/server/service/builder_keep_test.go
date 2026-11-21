package service

import (
	"context"
	"testing"

	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"k8s.io/client-go/kubernetes/fake"
)

type keepRecorder struct {
	appk8s.Driver
	kept []string
}

func (k *keepRecorder) KeepBuilder(_ context.Context, node, _ string) (bool, error) {
	k.kept = append(k.kept, node)
	return true, nil
}

// On a new install the gateway is a build node by its record alone: nobody
// has opened the Nodes page or started a build to label it. The keeper labels
// it first, so the builder is kept there before the first build.
func TestKeepBuildersLabelsTheGatewayFirst(t *testing.T) {
	gdb := newExtTestDB(t)
	ctx := context.Background()
	org := db.Organization{Name: "Acme", Slug: "acme"}
	if err := gdb.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&db.Node{OrganizationID: org.ID, Name: "gw", MeshRole: db.MeshRoleWorkloadBuilder}).Error; err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(clusterNode("gw", true, false))
	rec := &keepRecorder{}
	s := &NodeService{db: gdb, k8s: client, cluster: rec}

	s.keepBuilders(ctx)

	nodes, err := appk8s.GetClusterNodes(ctx, client)
	if err != nil || len(nodes) != 1 || !appk8s.IsBuildNode(nodes[0].Labels) {
		t.Fatalf("the gateway was not labelled a build node: %+v %v", nodes, err)
	}
	if len(rec.kept) != 1 || rec.kept[0] != "gw" {
		t.Fatalf("builder kept on %v, want [gw]", rec.kept)
	}
}
