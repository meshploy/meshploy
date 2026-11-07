package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A database pinned to a node skips the scheduler, so its claim must name the
// node or node-local storage never makes its volume.
func TestDatabaseClaimFollowsThePinnedNode(t *testing.T) {
	params := func(node string) DatabaseWorkloadParams {
		return DatabaseWorkloadParams{Name: "db", Namespace: "shop", Image: "postgres:17", Port: 5432, StorageGB: 1, DataPath: "/data", NodeName: node}
	}
	claim := func(t *testing.T, client *fake.Clientset) *corev1.PersistentVolumeClaim {
		t.Helper()
		c, err := client.CoreV1().PersistentVolumeClaims("shop").Get(t.Context(), "db-data", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("pinned: the new claim names the node", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := ApplyDatabaseDeployment(t.Context(), client, params("w1")); err != nil {
			t.Fatal(err)
		}
		if got := claim(t, client).Annotations[SelectedNodeAnnotation]; got != "w1" {
			t.Fatalf("selected node = %q, want w1", got)
		}
	})
	t.Run("unpinned: the claim leaves the node to the scheduler", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := ApplyDatabaseDeployment(t.Context(), client, params("")); err != nil {
			t.Fatal(err)
		}
		if _, ok := claim(t, client).Annotations[SelectedNodeAnnotation]; ok {
			t.Fatal("an unpinned database's claim names a node")
		}
	})
	t.Run("pinned later, still unbound: the claim is given the node", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		ApplyDatabaseDeployment(t.Context(), client, params(""))
		if err := ApplyDatabaseDeployment(t.Context(), client, params("w2")); err != nil {
			t.Fatal(err)
		}
		if got := claim(t, client).Annotations[SelectedNodeAnnotation]; got != "w2" {
			t.Fatalf("selected node = %q, want w2", got)
		}
	})
	t.Run("bound: the claim keeps its volume and node", func(t *testing.T) {
		client := fake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "db-data", Namespace: "shop", Annotations: map[string]string{SelectedNodeAnnotation: "w1"}},
			Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1"},
		})
		if err := ApplyDatabaseDeployment(t.Context(), client, params("w2")); err != nil {
			t.Fatal(err)
		}
		c := claim(t, client)
		if c.Spec.VolumeName != "pv-1" || c.Annotations[SelectedNodeAnnotation] != "w1" {
			t.Fatalf("a bound claim was changed: volume %q node %q", c.Spec.VolumeName, c.Annotations[SelectedNodeAnnotation])
		}
	})
}
