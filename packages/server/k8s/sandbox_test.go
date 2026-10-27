package k8s

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func runtimeOf(spec corev1.PodSpec) string {
	if spec.RuntimeClassName == nil {
		return ""
	}
	return *spec.RuntimeClassName
}

// Every kind of pod a project runs takes the runtime it is given, and the
// default when given none; a build never takes one.
func TestEveryPodKindTakesTheRuntime(t *testing.T) {
	for _, class := range []string{SandboxRuntimeClass, ""} {
		ctx := context.Background()
		client := fake.NewSimpleClientset()
		cpuReq, cpuLim, memReq, memLim := "100m", "200m", "64Mi", "128Mi"

		if err := ApplyDeployment(ctx, client, WorkloadParams{Name: "web", Namespace: "shop", Image: "nginx", RuntimeClass: class}); err != nil {
			t.Fatal(err)
		}
		if err := ApplyDatabaseDeployment(ctx, client, DatabaseWorkloadParams{Name: "pg", Namespace: "shop", Image: "postgres", Port: 5432, DataPath: "/data", RuntimeClass: class}); err != nil {
			t.Fatal(err)
		}
		if err := RunOnce(ctx, client, WorkloadParams{Name: "migrate", Namespace: "shop", Image: "app", RuntimeClass: class}, 0); err != nil {
			t.Fatal(err)
		}
		if err := ApplyCronJob(ctx, client, CronJobParams{Name: "nightly", Namespace: "shop", Schedule: "0 3 * * *", Image: "app",
			CPURequest: cpuReq, CPULimit: cpuLim, MemRequest: memReq, MemLimit: memLim, RuntimeClass: class}); err != nil {
			t.Fatal(err)
		}
		if err := CreateRunJob(ctx, client, RunJobParams{JobName: "nightly-1", Namespace: "shop", Image: "app",
			CPURequest: cpuReq, CPULimit: cpuLim, MemRequest: memReq, MemLimit: memLim, RuntimeClass: class}); err != nil {
			t.Fatal(err)
		}
		if err := CreateBuildJob(ctx, client, BuildJobParams{JobName: "build-web-1", Namespace: "shop"}); err != nil {
			t.Fatal(err)
		}

		pods := map[string]corev1.PodSpec{}
		for _, name := range []string{"web", "pg"} {
			d, err := client.AppsV1().Deployments("shop").Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			pods["deployment "+name] = d.Spec.Template.Spec
		}
		for _, name := range []string{"migrate", "nightly-1"} {
			j, err := client.BatchV1().Jobs("shop").Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			pods["job "+name] = j.Spec.Template.Spec
		}
		c, err := client.BatchV1().CronJobs("shop").Get(ctx, "nightly", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		pods["cron job"] = c.Spec.JobTemplate.Spec.Template.Spec
		for what, spec := range pods {
			if got := runtimeOf(spec); got != class {
				t.Errorf("%s runs on %q, want %q", what, got, class)
			}
		}
		build, err := client.BatchV1().Jobs("shop").Get(ctx, "build-web-1", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := runtimeOf(build.Spec.Template.Spec); got != "" {
			t.Errorf("a build runs on %q, want the default: it needs privileges gVisor cannot give", got)
		}
	}
}

// A sandboxed pod can be placed only where the runtime class exists and a
// node is prepared, or, when pinned, its node is.
func TestSandboxReady(t *testing.T) {
	ctx := context.Background()
	gvisor := &nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: SandboxRuntimeClass}, Handler: "runsc"}
	prepared := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw", Labels: map[string]string{SandboxNodeLabel: "true"}}}
	plain := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w1"}}

	cases := []struct {
		what   string
		client *fake.Clientset
		pinned string
		ok     bool
	}{
		{"no runtime class", fake.NewSimpleClientset(prepared), "", false},
		{"no prepared node", fake.NewSimpleClientset(gvisor, plain), "", false},
		{"a prepared node", fake.NewSimpleClientset(gvisor, prepared, plain), "", true},
		{"pinned to a prepared node", fake.NewSimpleClientset(gvisor, prepared, plain), "gw", true},
		{"pinned to a plain node", fake.NewSimpleClientset(gvisor, prepared, plain), "w1", false},
	}
	for _, c := range cases {
		err := SandboxReady(ctx, c.client, c.pinned)
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.what, err)
		}
		if !c.ok && !errors.Is(err, ErrNoSandboxNode) {
			t.Errorf("%s: got %v, want ErrNoSandboxNode", c.what, err)
		}
	}
}

// Switching a namespace moves the workloads Meshploy keeps there, except those
// kept on the host, and leaves anything else, Jobs included, as it is.
func TestSetNamespaceRuntime(t *testing.T) {
	ctx := context.Background()
	managed := map[string]string{"managed-by": "meshploy"}
	deployment := func(name string, labels map[string]string) *appsv1.Deployment {
		return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop", Labels: labels}}
	}
	client := fake.NewSimpleClientset(
		deployment("web", managed),
		deployment("pg", managed),
		deployment("their-own", nil),
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "shop", Labels: managed}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "build-web-1", Namespace: "shop", Labels: managed}},
	)
	runtimes := func() map[string]string {
		out := map[string]string{}
		for _, name := range []string{"web", "pg", "their-own"} {
			d, _ := client.AppsV1().Deployments("shop").Get(ctx, name, metav1.GetOptions{})
			out[name] = runtimeOf(d.Spec.Template.Spec)
		}
		c, _ := client.BatchV1().CronJobs("shop").Get(ctx, "nightly", metav1.GetOptions{})
		out["nightly"] = runtimeOf(c.Spec.JobTemplate.Spec.Template.Spec)
		j, _ := client.BatchV1().Jobs("shop").Get(ctx, "build-web-1", metav1.GetOptions{})
		out["build"] = runtimeOf(j.Spec.Template.Spec)
		return out
	}

	if err := SetNamespaceRuntime(ctx, client, "shop", SandboxRuntimeClass, []string{"pg"}); err != nil {
		t.Fatal(err)
	}
	got := runtimes()
	if got["web"] != SandboxRuntimeClass || got["nightly"] != SandboxRuntimeClass {
		t.Errorf("switched on: %v, want web and nightly on %s", got, SandboxRuntimeClass)
	}
	if got["pg"] != "" || got["their-own"] != "" || got["build"] != "" {
		t.Errorf("switched on: %v, want pg on the host, and a workload Meshploy does not keep, and a build, left alone", got)
	}

	if err := SetNamespaceRuntime(ctx, client, "shop", "", []string{"pg"}); err != nil {
		t.Fatal(err)
	}
	for name, class := range runtimes() {
		if class != "" {
			t.Errorf("switched off: %s still runs on %q", name, class)
		}
	}
}

// The runtime class schedules onto prepared nodes, and one made by hand
// without that is brought into line.
func TestEnsureSandboxRuntimeClass(t *testing.T) {
	ctx := context.Background()
	handMade := &nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: SandboxRuntimeClass}, Handler: "runsc"}
	for what, client := range map[string]*fake.Clientset{
		"none yet":  fake.NewSimpleClientset(),
		"hand-made": fake.NewSimpleClientset(handMade),
	} {
		for range 2 {
			if err := EnsureSandboxRuntimeClass(ctx, client); err != nil {
				t.Fatalf("%s: %v", what, err)
			}
		}
		rc, err := client.NodeV1().RuntimeClasses().Get(ctx, SandboxRuntimeClass, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if rc.Handler != "runsc" || rc.Scheduling == nil || rc.Scheduling.NodeSelector[SandboxNodeLabel] != "true" {
			t.Errorf("%s: got handler %q, scheduling %+v; want runsc onto %s=true", what, rc.Handler, rc.Scheduling, SandboxNodeLabel)
		}
	}
}

// The pods list says which runtime each pod runs on.
func TestListServicePodsReportsTheRuntime(t *testing.T) {
	ctx := context.Background()
	pod := func(name, class string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop", Labels: map[string]string{"app": "web", "managed-by": "meshploy"}},
			Spec:       corev1.PodSpec{RuntimeClassName: runtimeClass(class)},
		}
	}
	client := fake.NewSimpleClientset(pod("web-a", SandboxRuntimeClass), pod("web-b", ""))
	pods, err := ListServicePods(ctx, client, "shop", "web")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range pods {
		got[p.Name] = p.RuntimeClass
	}
	if got["web-a"] != SandboxRuntimeClass || got["web-b"] != "" {
		t.Errorf("got %v, want web-a on %s and web-b on the default", got, SandboxRuntimeClass)
	}
}
