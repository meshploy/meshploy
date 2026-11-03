package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// A node keeps every image it has run until its disk is nearly full. The
// daily cleanup the installer configures removes those no container has used
// for a day; FreeSpace does the same at once, for an operator whose disk
// cannot wait. Only images no container uses go: a running app keeps its
// image, and an earlier one comes back from the registry on rollback.

var (
	// ErrNotInCluster is FreeSpace on a machine that runs no cluster workloads.
	ErrNotInCluster = errors.New("this node is not in the cluster, so it holds no app images")
	// ErrFreeSpaceRunning is FreeSpace on a node already being cleaned.
	ErrFreeSpaceRunning = errors.New("this node is already being cleaned")
)

// freeSpaceTimeout bounds one cleanup; removing a few hundred images takes
// well under a minute.
const freeSpaceTimeout = 3 * time.Minute

// FreeSpaceResult is what a cleanup removed.
type FreeSpaceResult struct {
	ImagesRemoved int `json:"images_removed"`
	// RegistryCleanup is true when the gateway's registry was asked to drop
	// the layers of images it no longer lists, which the host agent does
	// within a minute.
	RegistryCleanup bool `json:"registry_cleanup"`
}

var freeing sync.Map // node ID -> struct{}, while a cleanup runs

// FreeSpace removes the images no container uses from a cluster node.
func (s *NodeService) FreeSpace(ctx context.Context, nodeID uuid.UUID) (*FreeSpaceResult, error) {
	node, err := s.Get(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if s.k8s == nil || node.MeshRole == db.MeshRoleMesh || node.K3sRole == "" {
		return nil, ErrNotInCluster
	}
	if _, busy := freeing.LoadOrStore(nodeID, struct{}{}); busy {
		return nil, ErrFreeSpaceRunning
	}
	defer freeing.Delete(nodeID)

	name, err := appk8s.ResolveK8sNodeName(ctx, s.k8s, node.TailscaleIP, node.Name)
	if err != nil {
		return nil, ErrNotInCluster
	}
	n, err := appk8s.PruneNodeImages(ctx, s.k8s, name, freeSpaceTimeout)
	if err != nil {
		return nil, err
	}
	return &FreeSpaceResult{ImagesRemoved: n}, nil
}
