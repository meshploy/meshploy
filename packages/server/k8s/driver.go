package k8s

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Driver is how the API reaches the cluster it runs workloads on.
//
// Everything Meshploy creates as an ordinary workload goes through Client:
// namespaces, deployments, jobs, services, volumes, secrets, policies, and
// reading them back. The pods that reach the node itself (the builder, the
// image cleanup, the node terminal) are asked for through the methods below
// instead, so that a cluster reached through an agent on its server gets them
// from the agent's own templates, never from a pod spec the API wrote.
type Driver interface {
	// Client is the cluster's API for what Meshploy creates.
	Client() kubernetes.Interface
	// RestConfig carries exec, port-forward and log streams.
	RestConfig() *rest.Config

	// StartBuild starts a build Job. Its builder image is the driver's to
	// choose; a driver may replace the one in p.
	StartBuild(ctx context.Context, p BuildJobParams) error
	// PruneNodeImages removes the images no container uses from a node and
	// returns how many it removed.
	PruneNodeImages(ctx context.Context, node string, timeout time.Duration) (int, error)
	// OpenNodeShell starts the node terminal's pod on a node, which exec then
	// enters.
	OpenNodeShell(ctx context.Context, node, podName string) (*corev1.Pod, error)
	// CloseNodeShell removes a node terminal's pod. Best effort: a pod that is
	// already gone is not an error.
	CloseNodeShell(ctx context.Context, podName string)
	// RemoveNode takes a node out of the cluster, once it has left the mesh.
	// A node already gone is not an error.
	RemoveNode(ctx context.Context, node string) error
}

// LocalDriver is a cluster whose credential the API holds itself, through a
// kubeconfig: each host-level pod is made by the API directly.
type LocalDriver struct {
	cs  kubernetes.Interface
	cfg *rest.Config
}

// NewLocalDriver drives the cluster cs reaches. cfg may be nil where no
// streams are needed (tests).
func NewLocalDriver(cs kubernetes.Interface, cfg *rest.Config) *LocalDriver {
	return &LocalDriver{cs: cs, cfg: cfg}
}

func (d *LocalDriver) Client() kubernetes.Interface { return d.cs }
func (d *LocalDriver) RestConfig() *rest.Config     { return d.cfg }

func (d *LocalDriver) StartBuild(ctx context.Context, p BuildJobParams) error {
	return CreateBuildJob(ctx, d.cs, p)
}

func (d *LocalDriver) PruneNodeImages(ctx context.Context, node string, timeout time.Duration) (int, error) {
	return PruneNodeImages(ctx, d.cs, node, timeout)
}

func (d *LocalDriver) OpenNodeShell(ctx context.Context, node, podName string) (*corev1.Pod, error) {
	return CreateShellPod(ctx, d.cs, node, podName)
}

func (d *LocalDriver) CloseNodeShell(ctx context.Context, podName string) {
	DeleteShellPod(ctx, d.cs, podName)
}

func (d *LocalDriver) RemoveNode(ctx context.Context, node string) error {
	if err := DeleteNode(ctx, d.cs, node); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
