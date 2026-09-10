package service

import (
	"strings"
	"testing"

	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func clusterNode(name string, ready, builder bool) *corev1.Node {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}}}
	if builder {
		n.Labels["meshploy.com/role"] = "builder"
	}
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}}
	return n
}

// With no database, buildNodeProblem cannot apply roles, so these cover the
// cluster side: what counts as a build node, and what a pinned node needs.
func TestBuildNodeProblem(t *testing.T) {
	cases := []struct {
		name   string
		nodes  []*corev1.Node
		pinned string
		want   string // substring; "" means no problem
	}{
		{"an online build node", []*corev1.Node{clusterNode("gw", true, true)}, "", ""},
		{"no node marked for builds", []*corev1.Node{clusterNode("gw", true, false)}, "", "Act as build node"},
		{"the only build node is offline", []*corev1.Node{clusterNode("gw", false, true)}, "", "No online node"},
		{"pinned to a node that exists", []*corev1.Node{clusterNode("gw", true, false)}, "gw", ""},
		{"pinned to a node that is gone", []*corev1.Node{clusterNode("gw", true, true)}, "old-worker", "not in the cluster"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			for _, n := range c.nodes {
				if _, err := client.CoreV1().Nodes().Create(t.Context(), n, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			s := &DeploymentService{k8s: client}
			got := s.buildNodeProblem(t.Context(), c.pinned)
			if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// A record with a build role reaches the cluster node it describes, matched by
// name or by mesh IP; a node already labelled is left alone.
func TestBuildRoleTargets(t *testing.T) {
	nodes := []appk8s.ClusterNode{
		{Name: "srv1854405", InternalIPs: []string{"100.64.0.1"}},
		{Name: "worker-a", InternalIPs: []string{"100.64.0.7"}},
		{Name: "worker-b", Labels: map[string]string{"meshploy.com/role": "builder"}},
	}
	records := []db.Node{
		{Name: "srv1854405", MeshRole: db.MeshRoleWorkloadBuilder},
		{Name: "renamed", TailscaleIP: "100.64.0.7", MeshRole: db.MeshRoleBuilder},
		{Name: "worker-b", MeshRole: db.MeshRoleWorkloadBuilder},
	}
	got := buildRoleTargets(nodes, records)
	want := map[string]db.MeshRole{"srv1854405": db.MeshRoleWorkloadBuilder, "worker-a": db.MeshRoleBuilder}
	if len(got) != len(want) || got["srv1854405"] != want["srv1854405"] || got["worker-a"] != want["worker-a"] {
		t.Errorf("got %v, want %v", got, want)
	}
}
