package k8s

import (
	"context"
	"errors"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// A sandboxed project's pods run on gVisor's user-space kernel instead of the
// host's, through a RuntimeClass whose handler is runsc. Only nodes prepared
// for it carry SandboxNodeLabel, and the RuntimeClass schedules onto them.
const (
	SandboxRuntimeClass = "gvisor"
	SandboxNodeLabel    = "meshploy.com/sandbox"
)

// ErrNoSandboxNode is returned when nothing in the cluster can run a
// sandboxed pod where it was asked to run.
var ErrNoSandboxNode = errors.New("no node can run sandboxed projects")

// SandboxReady reports whether a sandboxed pod can be placed: the
// RuntimeClass exists and at least one node is prepared, or, for a pod pinned
// to nodeName, that node is.
func SandboxReady(ctx context.Context, client kubernetes.Interface, nodeName string) error {
	if _, err := client.NodeV1().RuntimeClasses().Get(ctx, SandboxRuntimeClass, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("%w: the cluster has no %q runtime class; prepare a node first", ErrNoSandboxNode, SandboxRuntimeClass)
		}
		return err
	}
	if nodeName != "" {
		node, err := client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !IsSandboxNode(node.Labels) {
			return fmt.Errorf("%w: it is pinned to %s, which is not prepared for them", ErrNoSandboxNode, nodeName)
		}
		return nil
	}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: SandboxNodeLabel + "=true"})
	if err != nil {
		return err
	}
	if len(nodes.Items) == 0 {
		return fmt.Errorf("%w: prepare a node first", ErrNoSandboxNode)
	}
	return nil
}

// EnsureSandboxRuntimeClass keeps the cluster's sandbox RuntimeClass as
// Meshploy wants it: handler runsc, scheduled onto prepared nodes only, so a
// sandboxed pod never lands on a node without gVisor. It is inert until a
// project is sandboxed.
func EnsureSandboxRuntimeClass(ctx context.Context, client kubernetes.Interface) error {
	want := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: SandboxRuntimeClass, Labels: map[string]string{"managed-by": "meshploy"}},
		Handler:    "runsc",
		Scheduling: &nodev1.Scheduling{NodeSelector: map[string]string{SandboxNodeLabel: "true"}},
	}
	classes := client.NodeV1().RuntimeClasses()
	have, err := classes.Get(ctx, SandboxRuntimeClass, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = classes.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if have.Handler != want.Handler {
		// The handler cannot be changed in place.
		if err := classes.Delete(ctx, SandboxRuntimeClass, metav1.DeleteOptions{}); err != nil {
			return err
		}
		_, err = classes.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if equality.Semantic.DeepEqual(have.Scheduling, want.Scheduling) && have.Labels["managed-by"] == "meshploy" {
		return nil
	}
	have.Scheduling = want.Scheduling
	if have.Labels == nil {
		have.Labels = map[string]string{}
	}
	have.Labels["managed-by"] = "meshploy"
	_, err = classes.Update(ctx, have, metav1.UpdateOptions{})
	return err
}

// IsSandboxNode reports whether a node's labels say it can run sandboxed pods.
func IsSandboxNode(labels map[string]string) bool {
	return labels[SandboxNodeLabel] == "true"
}

// runtimeClass is what a pod spec carries for a runtime class: nothing for the
// default.
func runtimeClass(name string) *string {
	if name == "" {
		return nil
	}
	return &name
}

// SetNamespaceRuntime moves every workload Meshploy keeps in a namespace onto
// a runtime class, or back to the default with "", except the Deployments
// named in onHost, which always take the default. A Deployment rolls its
// pods; a CronJob's next run takes it. Jobs already running finish where
// they are.
func SetNamespaceRuntime(ctx context.Context, client kubernetes.Interface, namespace, class string, onHost []string) error {
	managed := metav1.ListOptions{LabelSelector: "managed-by=meshploy"}
	same := func(have, want *string) bool {
		return (have == nil && want == nil) || (have != nil && want != nil && *have == *want)
	}

	deployments := client.AppsV1().Deployments(namespace)
	list, err := deployments.List(ctx, managed)
	if err != nil {
		return err
	}
	for i := range list.Items {
		d := &list.Items[i]
		want := runtimeClass(class)
		if slices.Contains(onHost, d.Name) {
			want = nil
		}
		if same(d.Spec.Template.Spec.RuntimeClassName, want) {
			continue
		}
		d.Spec.Template.Spec.RuntimeClassName = want
		if _, err := deployments.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("deployment %s: %w", d.Name, err)
		}
	}

	cronJobs := client.BatchV1().CronJobs(namespace)
	crons, err := cronJobs.List(ctx, managed)
	if err != nil {
		return err
	}
	for i := range crons.Items {
		c := &crons.Items[i]
		spec := &c.Spec.JobTemplate.Spec.Template.Spec
		want := runtimeClass(class)
		if same(spec.RuntimeClassName, want) {
			continue
		}
		spec.RuntimeClassName = want
		if _, err := cronJobs.Update(ctx, c, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("cron job %s: %w", c.Name, err)
		}
	}
	return nil
}

// withRuntime sets a pod spec's runtime class.
func withRuntime(spec *corev1.PodSpec, class string) {
	spec.RuntimeClassName = runtimeClass(class)
}
