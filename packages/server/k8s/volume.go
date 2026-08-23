package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SelectedNodeAnnotation is the annotation a WaitForFirstConsumer provisioner
// (k3s ships local-path) uses to record which node a claim is provisioned on.
// Setting it up front pins provisioning to a chosen node; leaving it unset lets
// the scheduler decide when the first pod mounts the claim.
const SelectedNodeAnnotation = "volume.kubernetes.io/selected-node"

// VolumePVCStatus describes where a claim actually landed, which is not always
// where it was asked to land: the annotation is a request until the provisioner
// binds it, and node-local storage cannot be moved afterwards.
type VolumePVCStatus struct {
	Exists bool
	Phase  string // Pending | Bound | Lost
	Bound  bool
	// Node is the node the claim is provisioned on (or pinned to, while pending).
	// Empty means auto-schedule and not yet bound.
	Node string
}

// EnsureVolumePVC creates a RWO PVC for a project volume if it does not already
// exist. A non-empty nodeName pins provisioning to that node.
func EnsureVolumePVC(ctx context.Context, client kubernetes.Interface, name, namespace string, storageGB int, nodeName string) error {
	if storageGB <= 0 {
		storageGB = 5
	}
	_, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("check volume PVC: %w", err)
	}
	qty := resource.MustParse(fmt.Sprintf("%dGi", storageGB))
	meta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels:    map[string]string{"managed-by": "meshploy", "meshploy-volume": "true"},
	}
	if nodeName != "" {
		meta.Annotations = map[string]string{SelectedNodeAnnotation: nodeName}
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: meta,
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty},
			},
		},
	}
	_, err = client.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	return err
}

// GetVolumePVCStatus reports a claim's phase and the node it is bound or pinned
// to. A missing claim is not an error: the caller distinguishes "never created"
// from "deleted out from under us" by comparing with its own records.
func GetVolumePVCStatus(ctx context.Context, client kubernetes.Interface, name, namespace string) (VolumePVCStatus, error) {
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return VolumePVCStatus{}, nil
	}
	if err != nil {
		return VolumePVCStatus{}, fmt.Errorf("get volume PVC: %w", err)
	}
	return VolumePVCStatus{
		Exists: true,
		Phase:  string(pvc.Status.Phase),
		Bound:  pvc.Status.Phase == corev1.ClaimBound,
		Node:   pvc.Annotations[SelectedNodeAnnotation],
	}, nil
}

// DeleteVolumePVC deletes a volume PVC. No-op if the PVC does not exist.
func DeleteVolumePVC(ctx context.Context, client kubernetes.Interface, name, namespace string) error {
	err := client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete volume PVC: %w", err)
	}
	return nil
}
