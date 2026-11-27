package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func ndotsOf(spec corev1.PodSpec) string {
	if spec.DNSConfig == nil {
		return ""
	}
	for _, o := range spec.DNSConfig.Options {
		if o.Name == "ndots" && o.Value != nil {
			return *o.Value
		}
	}
	return ""
}

// An app, a one-off run and a scheduled job resolve outside names with
// ndots:2, through the cluster's DNS still; a database keeps Kubernetes' own.
func TestAppPodsResolveOutsideNamesFirst(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(ApplyDeployment(ctx, client, WorkloadParams{Name: "web", Namespace: "shop", Image: "nginx"}))
	must(ApplyDatabaseDeployment(ctx, client, DatabaseWorkloadParams{Name: "pg", Namespace: "shop", Image: "postgres", Port: 5432, DataPath: "/data"}))
	must(RunOnce(ctx, client, WorkloadParams{Name: "migrate", Namespace: "shop", Image: "app"}, 0))
	must(ApplyCronJob(ctx, client, CronJobParams{Name: "nightly", Namespace: "shop", Schedule: "0 3 * * *", Image: "app",
		CPURequest: "100m", CPULimit: "200m", MemRequest: "64Mi", MemLimit: "128Mi"}))
	must(CreateRunJob(ctx, client, RunJobParams{JobName: "nightly-1", Namespace: "shop", Image: "app",
		CPURequest: "100m", CPULimit: "200m", MemRequest: "64Mi", MemLimit: "128Mi"}))

	web, _ := client.AppsV1().Deployments("shop").Get(ctx, "web", metav1.GetOptions{})
	pg, _ := client.AppsV1().Deployments("shop").Get(ctx, "pg", metav1.GetOptions{})
	migrate, _ := client.BatchV1().Jobs("shop").Get(ctx, "migrate", metav1.GetOptions{})
	run, _ := client.BatchV1().Jobs("shop").Get(ctx, "nightly-1", metav1.GetOptions{})
	cron, _ := client.BatchV1().CronJobs("shop").Get(ctx, "nightly", metav1.GetOptions{})

	for what, spec := range map[string]corev1.PodSpec{
		"app": web.Spec.Template.Spec, "one-off run": migrate.Spec.Template.Spec,
		"job run": run.Spec.Template.Spec, "cron job": cron.Spec.JobTemplate.Spec.Template.Spec,
	} {
		if got := ndotsOf(spec); got != "2" {
			t.Errorf("%s: ndots %q, want 2", what, got)
		}
		if spec.DNSPolicy == corev1.DNSNone {
			t.Errorf("%s left the cluster's DNS", what)
		}
	}
	if got := ndotsOf(pg.Spec.Template.Spec); got != "" {
		t.Errorf("database: ndots %q, want Kubernetes' own", got)
	}
}
