package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A mesh-only node is not in the cluster, so applying its role touches nothing
// and does not fail on the missing node object.
func TestSetNodeMeshRoleIgnoresMeshOnlyNodes(t *testing.T) {
	if err := SetNodeMeshRole(t.Context(), fake.NewSimpleClientset(), "edge-box", NodePlacement{}); err != nil {
		t.Fatalf("mesh role: %v", err)
	}
	if err := SetNodeMeshRole(t.Context(), fake.NewSimpleClientset(), "missing", NodePlacement{InCluster: true}); err == nil {
		t.Fatal("a cluster role on a missing node should still fail")
	}
}

// Each placement reconciles the label and the taint, from any starting point,
// and leaves taints that are not Meshploy's alone.
func TestSetNodeMeshRoleReconcilesLabelAndTaint(t *testing.T) {
	other := corev1.Taint{Key: "example.com/gpu", Value: "yes", Effect: corev1.TaintEffectNoSchedule}
	ours := corev1.Taint{Key: meshRoleLabelKey, Value: meshRoleBuilderValue, Effect: corev1.TaintEffectNoSchedule}
	starts := map[string]*corev1.Node{
		"bare":        {ObjectMeta: metav1.ObjectMeta{Name: "n"}},
		"builds only": {ObjectMeta: metav1.ObjectMeta{Name: "n", Labels: map[string]string{meshRoleLabelKey: meshRoleBuilderValue}}, Spec: corev1.NodeSpec{Taints: []corev1.Taint{ours, other}}},
		"labelled":    {ObjectMeta: metav1.ObjectMeta{Name: "n", Labels: map[string]string{meshRoleLabelKey: meshRoleBuilderValue, "keep": "me"}}},
		"other taint": {ObjectMeta: metav1.ObjectMeta{Name: "n"}, Spec: corev1.NodeSpec{Taints: []corev1.Taint{other}}},
	}
	cases := []struct {
		name               string
		p                  NodePlacement
		wantLabel, wantOur bool
	}{
		{"builds and workloads", NodePlacement{InCluster: true, Builds: true}, true, false},
		{"workloads only", NodePlacement{InCluster: true}, false, false},
		{"builds only", NodePlacement{InCluster: true, Builds: true, BuildsOnly: true}, true, true},
		{"builds only, Builds unset", NodePlacement{InCluster: true, BuildsOnly: true}, true, true},
	}
	for startName, start := range starts {
		for _, c := range cases {
			t.Run(startName+" -> "+c.name, func(t *testing.T) {
				client := fake.NewSimpleClientset(start.DeepCopy())
				if err := SetNodeMeshRole(t.Context(), client, "n", c.p); err != nil {
					t.Fatal(err)
				}
				n, _ := client.CoreV1().Nodes().Get(t.Context(), "n", metav1.GetOptions{})
				if got := n.Labels[meshRoleLabelKey] == meshRoleBuilderValue; got != c.wantLabel {
					t.Errorf("builder label = %v, want %v", got, c.wantLabel)
				}
				var gotOur, gotOther int
				for _, tt := range n.Spec.Taints {
					switch tt.Key {
					case meshRoleLabelKey:
						gotOur++
					case other.Key:
						gotOther++
					}
				}
				if (gotOur == 1) != c.wantOur || gotOur > 1 {
					t.Errorf("Meshploy taints = %d, want %v", gotOur, c.wantOur)
				}
				hadOther := 0
				for _, tt := range start.Spec.Taints {
					if tt.Key == other.Key {
						hadOther = 1
					}
				}
				if gotOther != hadOther {
					t.Errorf("other taints = %d, want %d", gotOther, hadOther)
				}
				if start.Labels["keep"] != "" && n.Labels["keep"] != "me" {
					t.Error("an unrelated label was dropped")
				}
			})
		}
	}
}
