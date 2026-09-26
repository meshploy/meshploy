package k8s

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A stack's service answers by name on any port, like Docker's network: a
// headless Service that also lists pods not yet ready. One that was a plain
// ClusterIP Service before is replaced; an alias never takes a name another
// workload owns, and aliases no longer wanted are removed, with the workload.
func TestNameServicesAnswerLikeDocker(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "redpanda", Namespace: "pi", Labels: map[string]string{"app": "redpanda", "managed-by": "meshploy"}},
			Spec: corev1.ServiceSpec{ClusterIP: "10.43.0.9", Selector: map[string]string{"app": "redpanda"}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "taken", Namespace: "pi", Labels: map[string]string{"app": "other", "managed-by": "meshploy"}}},
	)
	svcs := client.CoreV1().Services("pi")

	if err := ApplyNameService(ctx, client, "redpanda", "redpanda", "pi", nil, false); err != nil {
		t.Fatal(err)
	}
	got, _ := svcs.Get(ctx, "redpanda", metav1.GetOptions{})
	if got.Spec.ClusterIP != corev1.ClusterIPNone || !got.Spec.PublishNotReadyAddresses || got.Spec.Selector["app"] != "redpanda" {
		t.Fatalf("not a headless name for the pods: %+v", got.Spec)
	}

	for _, a := range []string{"pi-redpanda", "taken"} {
		if err := ApplyNameService(ctx, client, a, "redpanda", "pi", nil, true); err != nil {
			t.Fatal(err)
		}
	}
	alias, _ := svcs.Get(ctx, "pi-redpanda", metav1.GetOptions{})
	if alias.Spec.Selector["app"] != "redpanda" || alias.Labels["meshploy-alias"] != "true" {
		t.Fatalf("alias does not point at the pods: %+v", alias)
	}
	if taken, _ := svcs.Get(ctx, "taken", metav1.GetOptions{}); taken.Labels["app"] != "other" {
		t.Fatal("an alias took over another workload's name")
	}
	if err := ApplyNameService(ctx, client, "taken", "redpanda", "pi", nil, false); err == nil {
		t.Fatal("a workload's own name must not silently take another's Service")
	}

	if err := DeleteWorkload(ctx, client, "redpanda", "pi"); err != nil {
		t.Fatal(err)
	}
	list, _ := svcs.List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 || list.Items[0].Name != "taken" {
		t.Fatalf("left behind: %v", list.Items)
	}
}

// A service that runs once is a Job, not a Deployment: running it replaces its
// last run and any Deployment of the same name, and going back to running for
// good removes the Job.
func TestRunOnceReplacesItsLastRun(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	p := WorkloadParams{Name: "migrator", Namespace: "pi", Image: "postgres:16-alpine", Replicas: 1,
		LivenessProbe: &corev1.Probe{}, ReadinessProbe: &corev1.Probe{}}

	if err := ApplyDeployment(ctx, client, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunOnce(ctx, client, p, 2); err != nil {
			t.Fatal(err)
		}
	}
	job, err := client.BatchV1().Jobs("pi").Get(ctx, "migrator", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec := job.Spec.Template.Spec
	if *job.Spec.BackoffLimit != 2 || spec.RestartPolicy != corev1.RestartPolicyNever || spec.Containers[0].LivenessProbe != nil {
		t.Fatalf("not a run: backoff %d, restart %s", *job.Spec.BackoffLimit, spec.RestartPolicy)
	}
	if _, err := client.AppsV1().Deployments("pi").Get(ctx, "migrator", metav1.GetOptions{}); err == nil {
		t.Fatal("the Deployment it replaced is still there")
	}

	if err := ApplyDeployment(ctx, client, p); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs("pi").Get(ctx, "migrator", metav1.GetOptions{}); err == nil {
		t.Fatal("running for good left its last run behind")
	}
}

// A Job with retries left has not failed when one of its pods has: only one
// marked failed, or out of retries, has.
func TestAJobHasFailedOnlyWhenItGivesUp(t *testing.T) {
	two := int32(2)
	cases := []struct {
		name   string
		job    batchv1.Job
		gaveUp bool
	}{
		{"first pod failed, retries left", batchv1.Job{Spec: batchv1.JobSpec{BackoffLimit: &two}, Status: batchv1.JobStatus{Failed: 1}}, false},
		{"out of retries", batchv1.Job{Spec: batchv1.JobSpec{BackoffLimit: &two}, Status: batchv1.JobStatus{Failed: 3}}, true},
		{"no retries, one failure", batchv1.Job{Status: batchv1.JobStatus{Failed: 1}}, true},
		{"marked failed", batchv1.Job{Spec: batchv1.JobSpec{BackoffLimit: &two}, Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}}, true},
	}
	for _, c := range cases {
		if got := jobGaveUp(&c.job); got != c.gaveUp {
			t.Errorf("%s: gave up = %v", c.name, got)
		}
	}
}
