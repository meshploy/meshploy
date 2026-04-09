package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/meshploy/packages/db"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const meshRoleLabelKey = "meshploy.com/role"
const meshRoleBuilderValue = "builder"

// ClusterNode holds a k8s node's name, readiness, internal IPs, labels, and hardware capacity.
type ClusterNode struct {
	Name        string
	Ready       bool
	InternalIPs []string
	Labels      map[string]string
	// Hardware capacity from node.Status.Capacity
	CPUCores   float32
	MemoryGB   float32
	DiskGB     float32
	K3sVersion string // e.g. "v1.34.6+k3s1"
}

// GetClusterNodes returns all nodes in the k8s cluster with their Ready status,
// internal IPs, and labels.
func GetClusterNodes(ctx context.Context, client kubernetes.Interface) ([]ClusterNode, error) {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s list nodes: %w", err)
	}

	out := make([]ClusterNode, 0, len(list.Items))
	for _, n := range list.Items {
		cn := ClusterNode{
			Name:       n.Name,
			Labels:     n.Labels,
			K3sVersion: n.Status.NodeInfo.KubeletVersion,
		}
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				cn.Ready = true
				break
			}
		}
		for _, addr := range n.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				cn.InternalIPs = append(cn.InternalIPs, addr.Address)
			}
		}
		if cpu, ok := n.Status.Capacity[corev1.ResourceCPU]; ok {
			cn.CPUCores = float32(cpu.MilliValue()) / 1000.0
		}
		if mem, ok := n.Status.Capacity[corev1.ResourceMemory]; ok {
			cn.MemoryGB = float32(mem.Value()) / (1024 * 1024 * 1024)
		}
		if disk, ok := n.Status.Capacity["ephemeral-storage"]; ok {
			cn.DiskGB = float32(disk.Value()) / (1024 * 1024 * 1024)
		}
		out = append(out, cn)
	}
	return out, nil
}

// SetNodeMeshRole applies the correct k8s label and taint to a node based on its
// MeshRole. Safe to call repeatedly — it reconciles to the desired state each time.
//
//   - workload_builder: adds builder label, no taint  → builds + workloads land here
//   - workload:         removes builder label, no taint → workloads only
//   - builder:          adds builder label + NoSchedule taint → builds only
func SetNodeMeshRole(ctx context.Context, client kubernetes.Interface, nodeName string, role db.MeshRole) error {
	node, err := client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeName, err)
	}

	// ── Labels ────────────────────────────────────────────────────────────────
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	switch role {
	case db.MeshRoleWorkloadBuilder, db.MeshRoleBuilder:
		node.Labels[meshRoleLabelKey] = meshRoleBuilderValue
	default:
		delete(node.Labels, meshRoleLabelKey)
	}

	// ── Taints ────────────────────────────────────────────────────────────────
	// Remove any existing meshploy taint, then re-add if builder-only.
	filtered := node.Spec.Taints[:0]
	for _, t := range node.Spec.Taints {
		if t.Key != meshRoleLabelKey {
			filtered = append(filtered, t)
		}
	}
	if role == db.MeshRoleBuilder {
		filtered = append(filtered, corev1.Taint{
			Key:    meshRoleLabelKey,
			Value:  meshRoleBuilderValue,
			Effect: corev1.TaintEffectNoSchedule,
		})
	}
	node.Spec.Taints = filtered

	// ── Patch ─────────────────────────────────────────────────────────────────
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"labels": node.Labels},
		"spec":     map[string]any{"taints": node.Spec.Taints},
	})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	_, err = client.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// systemNamespaces are excluded from the active-projects list shown in the UI.
var systemNamespaces = map[string]bool{
	"kube-system":      true,
	"kube-public":      true,
	"kube-node-lease":  true,
	"default":          true,
}

// GetNamespacesOnNode returns distinct non-system namespaces that have running
// pods scheduled on the given k8s node name.
func GetNamespacesOnNode(ctx context.Context, client kubernetes.Interface, nodeName string) ([]string, error) {
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nil, fmt.Errorf("k8s list pods on node %s: %w", nodeName, err)
	}

	seen := make(map[string]struct{})
	var out []string
	for _, pod := range pods.Items {
		ns := pod.Namespace
		if systemNamespaces[ns] {
			continue
		}
		if _, ok := seen[ns]; !ok {
			seen[ns] = struct{}{}
			out = append(out, ns)
		}
	}
	return out, nil
}
