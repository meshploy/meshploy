package k8s

import (
	"context"
	"fmt"

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
