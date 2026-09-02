package k8s

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ManagedWorkload is a Deployment meshploy created, as the cluster has it.
type ManagedWorkload struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
	Ready     int32  `json:"ready"`
	AgeDays   int    `json:"age_days"`
	HasPVC    bool   `json:"has_pvc"`
}

// ListManagedWorkloads returns every Deployment in a namespace that meshploy
// created, identified by the label it stamps on everything it applies.
//
// The label is what makes an orphan sweep safe to reason about: anything
// without it was put there by someone else and is none of meshploy's business,
// however it looks.
func ListManagedWorkloads(ctx context.Context, client kubernetes.Interface, namespace string) ([]ManagedWorkload, error) {
	if client == nil {
		return nil, nil
	}
	deps, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=meshploy",
	})
	if err != nil {
		return nil, fmt.Errorf("list deployments in %s: %w", namespace, err)
	}

	// A claim whose name starts with the workload's is almost certainly its
	// data. Reported so the UI can say what removing it would destroy, never
	// to drive an automatic deletion.
	pvcs, _ := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})

	out := make([]ManagedWorkload, 0, len(deps.Items))
	for _, d := range deps.Items {
		w := ManagedWorkload{
			Name:      d.Name,
			Namespace: namespace,
			Ready:     d.Status.ReadyReplicas,
			AgeDays:   int(metav1.Now().Sub(d.CreationTimestamp.Time).Hours() / 24),
		}
		if d.Spec.Replicas != nil {
			w.Replicas = *d.Spec.Replicas
		}
		if pvcs != nil {
			for _, c := range pvcs.Items {
				if len(c.Name) >= len(d.Name) && c.Name[:len(d.Name)] == d.Name {
					w.HasPVC = true
					break
				}
			}
		}
		out = append(out, w)
	}
	return out, nil
}

// DeleteManagedWorkload removes a workload and the Services that front it.
//
// It refuses anything not carrying meshploy's label. The caller has already
// decided this workload is unowned, but that decision came from comparing two
// systems, and the check here is against the object itself: whatever else is
// true, meshploy does not delete what it did not create.
//
// Claims are removed only when deleteData is set, and never as a side effect of
// removing compute. A deleted Deployment costs a redeploy; a deleted claim costs
// whatever was in it.
func DeleteManagedWorkload(ctx context.Context, client kubernetes.Interface, name, namespace string, deleteData bool) error {
	if client == nil {
		return fmt.Errorf("kubernetes is not configured")
	}
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil // already gone — the desired end state
	}
	if err != nil {
		return fmt.Errorf("get deployment %s: %w", name, err)
	}
	if dep.Labels["managed-by"] != "meshploy" {
		return fmt.Errorf("%s is not managed by meshploy — refusing to delete it", name)
	}

	if err := client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete deployment %s: %w", name, err)
	}
	for _, svcName := range []string{name, name + "-nodeport"} {
		if err := client.CoreV1().Services(namespace).Delete(ctx, svcName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("delete service %s: %w", svcName, err)
		}
	}

	if !deleteData {
		return nil
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list claims in %s: %w", namespace, err)
	}
	for _, c := range pvcs.Items {
		if len(c.Name) < len(name) || c.Name[:len(name)] != name {
			continue
		}
		if err := client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, c.Name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("delete claim %s: %w", c.Name, err)
		}
	}
	return nil
}
