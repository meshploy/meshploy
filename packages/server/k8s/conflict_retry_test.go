package k8s

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// conflictOnce makes the first update of resource (and subresource) fail as
// a write that lost a race to the object's controller does.
func conflictOnce(client *fake.Clientset, resource, subresource string) *int {
	n := 0
	client.PrependReactor("update", resource, func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != subresource {
			return false, nil, nil
		}
		n++
		if n == 1 {
			return true, nil, k8serrors.NewConflict(schema.GroupResource{Resource: resource}, "x", nil)
		}
		return false, nil, nil
	})
	return &n
}

// withScale serves a Deployment's scale subresource, which the fake
// clientset's tracker does not.
func withScale(client *fake.Clientset) {
	client.PrependReactor("get", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		d, err := client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), a.GetNamespace(), a.(k8stesting.GetAction).GetName())
		if err != nil {
			return true, nil, err
		}
		dep := d.(*appsv1.Deployment)
		return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: dep.Name, Namespace: dep.Namespace},
			Spec: autoscalingv1.ScaleSpec{Replicas: *dep.Spec.Replicas}}, nil
	})
	client.PrependReactor("update", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		sc := a.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
		d, err := client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), a.GetNamespace(), sc.Name)
		if err != nil {
			return true, nil, err
		}
		dep := d.(*appsv1.Deployment).DeepCopy()
		dep.Spec.Replicas = &sc.Spec.Replicas
		return true, sc, client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), dep, a.GetNamespace())
	})
}

func web() *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "shop", Labels: map[string]string{"managed-by": "meshploy"}},
		Spec: appsv1.DeploymentSpec{Replicas: &one, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web", Image: "x"}}}}},
	}
}

// Writes that carry an object's version read it again after losing a race to
// its controller, instead of failing the deploy.
func TestUpdatesRetryAConflict(t *testing.T) {
	t.Run("scale", func(t *testing.T) {
		client := fake.NewSimpleClientset(web())
		withScale(client)
		tries := conflictOnce(client, "deployments", "scale")
		if err := ScaleDeployment(t.Context(), client, "web", "shop", 3); err != nil {
			t.Fatalf("ScaleDeployment: %v", err)
		}
		d, _ := client.AppsV1().Deployments("shop").Get(t.Context(), "web", metav1.GetOptions{})
		if *tries != 2 || *d.Spec.Replicas != 3 {
			t.Errorf("%d tries, %d replicas", *tries, *d.Spec.Replicas)
		}
	})
	t.Run("pin to a node", func(t *testing.T) {
		client := fake.NewSimpleClientset(web())
		tries := conflictOnce(client, "deployments", "")
		if err := SetDeploymentNode(t.Context(), client, "web", "shop", "w1"); err != nil {
			t.Fatalf("SetDeploymentNode: %v", err)
		}
		d, _ := client.AppsV1().Deployments("shop").Get(t.Context(), "web", metav1.GetOptions{})
		if *tries != 2 || d.Spec.Template.Spec.NodeName != "w1" {
			t.Errorf("%d tries, node %q", *tries, d.Spec.Template.Spec.NodeName)
		}
	})
	t.Run("cron job", func(t *testing.T) {
		p := CronJobParams{Name: "nightly", Namespace: "shop", Schedule: "0 3 * * *", Image: "x", Command: "true",
			CPURequest: "50m", CPULimit: "500m", MemRequest: "32Mi", MemLimit: "128Mi"}
		client := fake.NewSimpleClientset()
		if err := ApplyCronJob(t.Context(), client, p); err != nil {
			t.Fatal(err)
		}
		tries := conflictOnce(client, "cronjobs", "")
		p.Schedule = "0 4 * * *"
		if err := ApplyCronJob(t.Context(), client, p); err != nil {
			t.Fatalf("ApplyCronJob: %v", err)
		}
		c, _ := client.BatchV1().CronJobs("shop").Get(t.Context(), "nightly", metav1.GetOptions{})
		if *tries != 2 || c.Spec.Schedule != "0 4 * * *" {
			t.Errorf("%d tries, schedule %q", *tries, c.Spec.Schedule)
		}
	})
	t.Run("namespace runtime", func(t *testing.T) {
		cron := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "shop", Labels: map[string]string{"managed-by": "meshploy"}}}
		client := fake.NewSimpleClientset(web(), cron)
		dTries := conflictOnce(client, "deployments", "")
		cTries := conflictOnce(client, "cronjobs", "")
		if err := SetNamespaceRuntime(t.Context(), client, "shop", SandboxRuntimeClass, nil); err != nil {
			t.Fatalf("SetNamespaceRuntime: %v", err)
		}
		d, _ := client.AppsV1().Deployments("shop").Get(t.Context(), "web", metav1.GetOptions{})
		c, _ := client.BatchV1().CronJobs("shop").Get(t.Context(), "c", metav1.GetOptions{})
		if *dTries != 2 || *cTries != 2 || d.Spec.Template.Spec.RuntimeClassName == nil || c.Spec.JobTemplate.Spec.Template.Spec.RuntimeClassName == nil {
			t.Errorf("tries %d/%d; deployment runtime %v, cron runtime %v", *dTries, *cTries, d.Spec.Template.Spec.RuntimeClassName, c.Spec.JobTemplate.Spec.Template.Spec.RuntimeClassName)
		}
	})
}
