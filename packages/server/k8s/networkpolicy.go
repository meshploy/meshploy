package k8s

import (
	"context"
	"fmt"
	"net"
	"slices"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// A project's namespace accepts traffic from its own pods and from the
// namespaces that have a reason to reach it, and from nothing else in the
// cluster. Only incoming traffic is held: what a pod reaches out to (DNS, a
// registry, the internet) is unchanged.
//
// k3s enforces NetworkPolicy itself, and always lets a node reach its own
// pods, so the proxy and a published port reaching a pod on the node it
// arrives at need no rule. A NodePort that forwards to a pod on another node
// arrives from the first node's tunnel address, which is why those are
// allowed.

// IsolationPolicyName names the one policy Meshploy keeps in each project's
// namespace.
const IsolationPolicyName = "meshploy-isolation"

// Isolation is what a namespace's policy allows in, besides its own pods.
type Isolation struct {
	// Neighbours are namespaces whose pods may reach this one's: the
	// project's other environment levels, and projects whose services use a
	// service here through its variables.
	Neighbours []string
	// NodeAddresses are the nodes' tunnel addresses, one IP each.
	NodeAddresses []string
}

// IsolationPolicy is the policy for one namespace. Its lists are sorted, so
// the same inputs always give the same object.
func IsolationPolicy(namespace string, in Isolation) *networkingv1.NetworkPolicy {
	rules := []networkingv1.NetworkPolicyIngressRule{
		{From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}}},
	}
	neighbours := slices.Sorted(slices.Values(in.Neighbours))
	neighbours = slices.Compact(slices.DeleteFunc(neighbours, func(n string) bool { return n == namespace }))
	if len(neighbours) > 0 {
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{From: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpIn, Values: neighbours,
			}}},
		}}})
	}
	nodes := slices.Compact(slices.Sorted(slices.Values(in.NodeAddresses)))
	if len(nodes) > 0 {
		var from []networkingv1.NetworkPolicyPeer
		for _, ip := range nodes {
			from = append(from, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: ip + "/32"}})
		}
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{From: from})
	}
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: IsolationPolicyName, Namespace: namespace,
			Labels: map[string]string{"managed-by": "meshploy"},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     rules,
		},
	}
}

// ApplyIsolation puts a namespace's policy in place, or brings it up to date.
func ApplyIsolation(ctx context.Context, client kubernetes.Interface, namespace string, in Isolation) error {
	want := IsolationPolicy(namespace, in)
	policies := client.NetworkingV1().NetworkPolicies(namespace)
	have, err := policies.Get(ctx, IsolationPolicyName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = policies.Create(ctx, want, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if equality.Semantic.DeepEqual(have.Spec, want.Spec) && have.Labels["managed-by"] == "meshploy" {
		return nil
	}
	have.Labels, have.Spec = want.Labels, want.Spec
	_, err = policies.Update(ctx, have, metav1.UpdateOptions{})
	return err
}

// IsolatedNamespaces are the namespaces that hold Meshploy's policy.
func IsolatedNamespaces(ctx context.Context, client kubernetes.Interface) (map[string]bool, error) {
	list, err := client.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{LabelSelector: "managed-by=meshploy"})
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range list.Items {
		if p.Name == IsolationPolicyName {
			out[p.Namespace] = true
		}
	}
	return out, nil
}

// RemoveIsolation takes a namespace's policy away, returning it to what it
// was before.
func RemoveIsolation(ctx context.Context, client kubernetes.Interface, namespace string) error {
	err := client.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, IsolationPolicyName, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

// NodeTunnelAddresses are the address each node's pod network starts at,
// which Flannel gives the node's tunnel end and which traffic forwarded
// between nodes arrives from.
func NodeTunnelAddresses(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range nodes.Items {
		cidrs := n.Spec.PodCIDRs
		if len(cidrs) == 0 && n.Spec.PodCIDR != "" {
			cidrs = []string{n.Spec.PodCIDR}
		}
		for _, c := range cidrs {
			ip, _, err := net.ParseCIDR(c)
			if err != nil {
				return nil, fmt.Errorf("node %s: pod network %q: %w", n.Name, c, err)
			}
			if ip.To4() != nil {
				out = append(out, ip.String())
			}
		}
	}
	return out, nil
}

// MeshployNamespaces are the namespaces Meshploy made.
func MeshployNamespaces(ctx context.Context, client kubernetes.Interface) (map[string]bool, error) {
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: "managed-by=meshploy"})
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list.Items))
	for _, ns := range list.Items {
		out[ns.Name] = true
	}
	return out, nil
}
