package agent

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/meshploy/apps/cluster-agent/policy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterFacts answers the policy's questions from the cluster and the
// machine.
type ClusterFacts struct {
	cs kubernetes.Interface
	// approvalFile holds, in RFC 3339, when the owner's approval to open a
	// node terminal ends. Only someone with root on the machine can write it.
	approvalFile string
	// cacheFor is how long a namespace's label is trusted before it is read
	// again.
	cacheFor time.Duration

	mu   sync.Mutex
	seen map[string]seenNamespace
}

type seenNamespace struct {
	managed bool
	at      time.Time
}

// NewClusterFacts reads the cluster through cs, and the terminal approval
// from approvalFile.
func NewClusterFacts(cs kubernetes.Interface, approvalFile string) *ClusterFacts {
	return &ClusterFacts{cs: cs, approvalFile: approvalFile, cacheFor: 5 * time.Second, seen: map[string]seenNamespace{}}
}

var _ policy.Facts = (*ClusterFacts)(nil)

// Managed is whether a namespace carries managed-by=meshploy. Anything that
// cannot be read counts as not managed.
func (f *ClusterFacts) Managed(ns string) bool {
	if ns == "" {
		return false
	}
	f.mu.Lock()
	c, ok := f.seen[ns]
	f.mu.Unlock()
	if ok && time.Since(c.at) < f.cacheFor {
		return c.managed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := f.cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	managed := err == nil && n.Labels[policy.ManagedLabel] == policy.ManagedValue && n.DeletionTimestamp == nil
	f.mu.Lock()
	f.seen[ns] = seenNamespace{managed, time.Now()}
	f.mu.Unlock()
	return managed
}

// TerminalApprovedUntil is when the owner's approval ends; zero when there is
// none or it cannot be read.
func (f *ClusterFacts) TerminalApprovedUntil() time.Time {
	raw, err := os.ReadFile(f.approvalFile)
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// ShellLabels are the labels the node terminal's pod carries.
var ShellLabels = map[string]string{"app": "meshploy-shell", "managed-by": "meshploy"}

// Shell is whether a kube-system pod is a node terminal. Only someone who
// can already write kube-system (the agent, or the machine's owner) can make
// a pod there with these labels.
func (f *ClusterFacts) Shell(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := f.cs.CoreV1().Pods("kube-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}
	for k, v := range ShellLabels {
		if p.Labels[k] != v {
			return false
		}
	}
	return true
}
