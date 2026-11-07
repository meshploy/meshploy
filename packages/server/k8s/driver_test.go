package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The local driver makes each host-level pod as the API always has, so
// Community behaves the same through it.
func TestLocalDriverMakesTheHostLevelPods(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw"}})
	d := NewLocalDriver(client, nil)
	if d.Client() != client {
		t.Fatal("Client is not the clientset the driver was made with")
	}

	if err := d.StartBuild(t.Context(), BuildJobParams{
		JobName: "build-app-1", Namespace: "shop", Image: "example.com/builder:test", GitBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	job, err := client.BatchV1().Jobs("shop").Get(t.Context(), "build-app-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("build job not created: %v", err)
	}
	if got := job.Spec.Template.Spec.Containers[0].Image; got != "example.com/builder:test" {
		t.Errorf("local driver changed the builder image to %q", got)
	}

	pod, err := d.OpenNodeShell(t.Context(), "gw", "meshploy-shell-abc")
	if err != nil {
		t.Fatal(err)
	}
	if pod.Namespace != "kube-system" || pod.Spec.NodeName != "gw" {
		t.Errorf("shell pod %s/%s on %q", pod.Namespace, pod.Name, pod.Spec.NodeName)
	}
	d.CloseNodeShell(t.Context(), "meshploy-shell-abc")
	if _, err := client.CoreV1().Pods("kube-system").Get(t.Context(), "meshploy-shell-abc", metav1.GetOptions{}); err == nil {
		t.Error("shell pod still there after CloseNodeShell")
	}
	// Closing what is already gone is not an error and does not panic.
	d.CloseNodeShell(t.Context(), "meshploy-shell-abc")
}

// recordingDriver is a Driver that remembers which shells it was asked to
// close, so a test can see a caller went through the driver.
type recordingDriver struct {
	*LocalDriver
	closed []string
}

func (r *recordingDriver) CloseNodeShell(_ context.Context, name string) {
	r.closed = append(r.closed, name)
}

// Leftover terminals are closed through the driver, never by deleting
// kube-system pods directly, which an agent-reached cluster refuses; and only
// Meshploy's own terminals are touched.
func TestCleanupOrphanedShellPodsClosesThroughTheDriver(t *testing.T) {
	shell := func(name string, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system", Labels: labels}}
	}
	client := fake.NewSimpleClientset(
		shell("meshploy-shell-1", map[string]string{"managed-by": "meshploy", "app": "meshploy-shell"}),
		shell("coredns-x", map[string]string{"k8s-app": "kube-dns"}),
		shell("meshploy-shell-2", map[string]string{"app": "meshploy-shell"}), // not Meshploy's label set
	)
	d := &recordingDriver{LocalDriver: NewLocalDriver(client, nil)}
	CleanupOrphanedShellPods(t.Context(), d)
	if len(d.closed) != 1 || d.closed[0] != "meshploy-shell-1" {
		t.Fatalf("closed %v, want only meshploy-shell-1", d.closed)
	}
	for _, name := range []string{"meshploy-shell-1", "coredns-x", "meshploy-shell-2"} {
		if _, err := client.CoreV1().Pods("kube-system").Get(t.Context(), name, metav1.GetOptions{}); err != nil {
			t.Errorf("%s was deleted directly, not through the driver: %v", name, err)
		}
	}
}

// The local driver's image cleanup is the API's own cleanup pod, pinned to
// the node asked for. A pod that never finishes (a fake cluster runs nothing)
// ends in an error at the timeout, not a count.
func TestLocalDriverPrunesOnTheNodeAsked(t *testing.T) {
	client := fake.NewSimpleClientset()
	var made []*corev1.Pod
	client.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
		p.Namespace = a.GetNamespace()
		made = append(made, p)
		return false, nil, nil
	})
	d := NewLocalDriver(client, nil)
	if _, err := d.PruneNodeImages(t.Context(), "w1", 200*time.Millisecond); err == nil {
		t.Fatal("a cleanup that never finished reported success")
	}
	if len(made) != 1 {
		t.Fatalf("%d cleanup pods made, want 1", len(made))
	}
	if p := made[0]; p.Namespace != "kube-system" || p.Spec.NodeName != "w1" || p.Labels["app"] != "meshploy-image-prune" {
		t.Errorf("cleanup pod in %q on %q labelled %v", p.Namespace, p.Spec.NodeName, p.Labels)
	}
}
