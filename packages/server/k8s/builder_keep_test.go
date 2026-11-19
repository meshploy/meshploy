package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuilderRepo(t *testing.T) {
	for image, want := range map[string]string{
		"ghcr.io/meshploy/builder:main":         "ghcr.io/meshploy/builder",
		"ghcr.io/meshploy/builder:0.25":         "ghcr.io/meshploy/builder",
		"100.64.0.1:5000/meshploy/builder:edge": "100.64.0.1:5000/meshploy/builder",
		"localhost/builder:dev":                 "localhost/builder",
		"ghcr.io/meshploy/builder@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": "ghcr.io/meshploy/builder",
		// Docker Hub's short names are listed in full by containerd, so they
		// would never match; a name with neither tag nor digest says nothing.
		"meshploy/builder:main":                   "",
		"ghcr.io/meshploy/builder":                "",
		"ghcr.io/meshploy/builder:main; rm -rf /": "",
		"ghcr.io/Meshploy/builder:main":           "",
		"":                                        "",
	} {
		if got := builderRepo(image); got != want {
			t.Errorf("builderRepo(%q) = %q, want %q", image, got, want)
		}
	}
}

// Keeping the builder starts one download per node, with the image in the
// pod's environment rather than its script, removes the runs that finished,
// and leaves a node already downloading it alone.
func TestKeepBuilder(t *testing.T) {
	done := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "kube-system", Labels: map[string]string{"app": keepBuilderApp}},
		Spec:       corev1.PodSpec{NodeName: "w1"},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	elsewhere := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "kube-system", Labels: map[string]string{"app": keepBuilderApp}},
		Spec:       corev1.PodSpec{NodeName: "w2"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	client := fake.NewSimpleClientset(done, elsewhere)
	ctx := t.Context()

	started, err := KeepBuilder(ctx, client, "w1", "ghcr.io/meshploy/builder:main")
	if err != nil || !started {
		t.Fatalf("first keep on w1: started=%v err=%v", started, err)
	}
	if _, err := client.CoreV1().Pods("kube-system").Get(ctx, "old", metav1.GetOptions{}); err == nil {
		t.Error("the finished run was not removed")
	}
	pods, _ := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
	var made *corev1.Pod
	for i, p := range pods.Items {
		if p.Name != "other" {
			made = &pods.Items[i]
		}
	}
	if made == nil {
		t.Fatal("no pod made")
	}
	env := map[string]string{}
	for _, e := range made.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if made.Spec.NodeName != "w1" || env["IMG"] != "ghcr.io/meshploy/builder:main" || env["REPO"] != "ghcr.io/meshploy/builder" {
		t.Errorf("pod on %q with env %v", made.Spec.NodeName, env)
	}
	if made.Spec.ActiveDeadlineSeconds == nil || *made.Spec.ActiveDeadlineSeconds != keepBuilderDeadline {
		t.Error("the download has no deadline")
	}

	// The fake leaves the pod pending, which is a download under way.
	if started, err := KeepBuilder(ctx, client, "w1", "ghcr.io/meshploy/builder:main"); err != nil || started {
		t.Errorf("second keep on w1: started=%v err=%v, want left alone", started, err)
	}
	if _, err := KeepBuilder(ctx, client, "w1", "builder"); err == nil {
		t.Error("a reference without a registry was taken")
	}
}

func TestBuildNodesAreTheReadyBuilders(t *testing.T) {
	node := func(name string, builder, ready bool) *corev1.Node {
		n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}}}
		if builder {
			n.Labels[BuilderNodeLabel] = BuilderNodeValue
		}
		st := corev1.ConditionFalse
		if ready {
			st = corev1.ConditionTrue
		}
		n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: st}}
		return n
	}
	client := fake.NewSimpleClientset(node("gw", true, true), node("down", true, false), node("app", false, true))
	got, err := BuildNodes(t.Context(), client)
	if err != nil || len(got) != 1 || got[0] != "gw" {
		t.Fatalf("BuildNodes = %v, %v; want [gw]", got, err)
	}
}
