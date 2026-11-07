package k8s

import (
	"context"
	"encoding/json"
	"fmt"

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

// IsBuildNode reports whether a node's labels let build jobs schedule on it.
func IsBuildNode(labels map[string]string) bool {
	return labels[meshRoleLabelKey] == meshRoleBuilderValue
}

// NodePlacement is what a node takes, which its label and taint express.
type NodePlacement struct {
	// InCluster is false for a node on the mesh only: nothing to apply.
	InCluster bool
	// Builds is whether build jobs may land on the node (the builder label).
	Builds bool
	// BuildsOnly keeps everything but builds off it (a NoSchedule taint).
	BuildsOnly bool
}

// SetNodeMeshRole applies the label and taint for a node's placement. Safe to
// call repeatedly - it reconciles to the desired state each time.
//
//   - builds and workloads: builder label, no taint
//   - workloads only:       no builder label, no taint
//   - builds only:          builder label and a NoSchedule taint
//   - not in the cluster:   nothing to apply
func SetNodeMeshRole(ctx context.Context, client kubernetes.Interface, nodeName string, p NodePlacement) error {
	if !p.InCluster {
		return nil
	}
	node, err := client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeName, err)
	}

	// ── Label ─────────────────────────────────────────────────────────────────
	// Only Meshploy's key is sent. A merge patch leaves keys it does not name
	// as they are, so removing the label takes an explicit null.
	var label any
	if p.Builds || p.BuildsOnly {
		label = meshRoleBuilderValue
	}

	// ── Taints ────────────────────────────────────────────────────────────────
	// Remove any existing meshploy taint, then re-add if builder-only.
	filtered := node.Spec.Taints[:0]
	for _, t := range node.Spec.Taints {
		if t.Key != meshRoleLabelKey {
			filtered = append(filtered, t)
		}
	}
	if p.BuildsOnly {
		filtered = append(filtered, corev1.Taint{
			Key:    meshRoleLabelKey,
			Value:  meshRoleBuilderValue,
			Effect: corev1.TaintEffectNoSchedule,
		})
	}
	node.Spec.Taints = filtered

	// ── Patch ─────────────────────────────────────────────────────────────────
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"labels": map[string]any{meshRoleLabelKey: label}},
		"spec":     map[string]any{"taints": node.Spec.Taints},
	})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	_, err = client.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// DeleteNode removes the node object from the k8s cluster. This does not stop
// the k3s-agent process on the worker - the user must run k3s-agent-uninstall.sh
// manually (or the future CLI will do it). Non-fatal if the node doesn't exist.
func DeleteNode(ctx context.Context, client kubernetes.Interface, nodeName string) error {
	err := client.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("k8s delete node %s: %w", nodeName, err)
	}
	return nil
}

// systemNamespaces are excluded from the active-projects list shown in the UI.
var systemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
	"default":         true,
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
