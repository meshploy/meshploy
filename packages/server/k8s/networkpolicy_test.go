package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A namespace takes in its own pods, then its neighbours, then the nodes, and
// nothing else; the same inputs in any order give the same policy.
func TestTheIsolationPolicy(t *testing.T) {
	p := IsolationPolicy("shop", Isolation{
		Neighbours:    []string{"shop-staging", "shop", "tools", "shop-staging"},
		NodeAddresses: []string{"10.42.1.0", "10.42.0.0"},
	})
	if len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("policy types %v, want ingress only: what a pod reaches out to is unchanged", p.Spec.PolicyTypes)
	}
	rules := p.Spec.Ingress
	if len(rules) != 3 {
		t.Fatalf("%d rules, want own pods, neighbours, nodes", len(rules))
	}
	if sel := rules[0].From[0].PodSelector; sel == nil || len(sel.MatchLabels)+len(sel.MatchExpressions) != 0 || rules[0].From[0].NamespaceSelector != nil {
		t.Errorf("first rule %+v, want every pod of this namespace", rules[0].From[0])
	}
	got := rules[1].From[0].NamespaceSelector.MatchExpressions[0].Values
	if want := []string{"shop-staging", "tools"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("neighbours %v, want %v: sorted, once each, never itself", got, want)
	}
	if from := rules[2].From; len(from) != 2 || from[0].IPBlock.CIDR != "10.42.0.0/32" || from[1].IPBlock.CIDR != "10.42.1.0/32" {
		t.Errorf("nodes %+v, want each tunnel address alone", from)
	}

	alone := IsolationPolicy("solo", Isolation{Neighbours: []string{"solo"}})
	if len(alone.Spec.Ingress) != 1 {
		t.Errorf("a namespace with no neighbours or nodes has %d rules, want its own pods only", len(alone.Spec.Ingress))
	}
}

// Applying makes the policy or brings it up to date; removing takes away only
// the one Meshploy keeps.
func TestApplyingAndRemovingIsolation(t *testing.T) {
	ctx := context.Background()
	theirs := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "their-own", Namespace: "shop", Labels: map[string]string{"managed-by": "meshploy"}}}
	client := fake.NewSimpleClientset(theirs)

	if err := ApplyIsolation(ctx, client, "shop", Isolation{}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyIsolation(ctx, client, "shop", Isolation{Neighbours: []string{"tools"}}); err != nil {
		t.Fatal(err)
	}
	got, err := client.NetworkingV1().NetworkPolicies("shop").Get(ctx, IsolationPolicyName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.Ingress) != 2 {
		t.Errorf("after an update the policy has %d rules, want the neighbour added", len(got.Spec.Ingress))
	}
	if err := ApplyIsolation(ctx, client, "tools", Isolation{}); err != nil {
		t.Fatal(err)
	}

	isolated, err := IsolatedNamespaces(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(isolated) != 2 || !isolated["shop"] || !isolated["tools"] {
		t.Errorf("isolated namespaces %v, want shop and tools", isolated)
	}
	for ns := range isolated {
		if err := RemoveIsolation(ctx, client, ns); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveIsolation(ctx, client, "shop"); err != nil {
		t.Errorf("removing twice: %v", err)
	}
	left, _ := client.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if len(left.Items) != 1 || left.Items[0].Name != "their-own" {
		t.Errorf("left %d policies, want only the one Meshploy did not make for isolation", len(left.Items))
	}
}

// A node's tunnel address is where its pod network starts.
func TestNodeTunnelAddresses(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw"}, Spec: corev1.NodeSpec{PodCIDR: "10.42.0.0/24", PodCIDRs: []string{"10.42.0.0/24", "fd00:42::/64"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w1"}, Spec: corev1.NodeSpec{PodCIDR: "10.42.3.0/24"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "joining"}},
	)
	got, err := NodeTunnelAddresses(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "10.42.0.0" || got[1] != "10.42.3.0" {
		t.Errorf("tunnel addresses %v, want 10.42.0.0 and 10.42.3.0", got)
	}
}

// A namespace takes its first step as it is made, and only then.
func TestANewNamespaceIsIsolatedAsItIsMade(t *testing.T) {
	before := namespaceCreated
	t.Cleanup(func() { namespaceCreated = before })
	var made []string
	OnNamespaceCreated(func(_ context.Context, name string) error { made = append(made, name); return nil })

	client := fake.NewSimpleClientset()
	for range 2 {
		if err := EnsureNamespace(context.Background(), client, "shop"); err != nil {
			t.Fatal(err)
		}
	}
	if len(made) != 1 || made[0] != "shop" {
		t.Errorf("the step ran for %v, want once, for shop", made)
	}
}
