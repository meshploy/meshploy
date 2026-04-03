package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterNode holds a k8s node's name, readiness, and internal IPs.
type ClusterNode struct {
	Name        string
	Ready       bool
	InternalIPs []string
}

// GetClusterNodes returns all nodes in the k8s cluster with their Ready status
// and internal IPs.
func GetClusterNodes(ctx context.Context, client kubernetes.Interface) ([]ClusterNode, error) {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s list nodes: %w", err)
	}

	out := make([]ClusterNode, 0, len(list.Items))
	for _, n := range list.Items {
		cn := ClusterNode{Name: n.Name}
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
		out = append(out, cn)
	}
	return out, nil
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
