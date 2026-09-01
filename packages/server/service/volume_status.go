package service

import (
	"context"
	"log"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
	db "github.com/meshploy/packages/db"
)

// volumeReconcileInterval matches the service reconciler: fast enough that a
// broken volume stops claiming to be ready within a minute, slow enough to stay
// cheap on a project with many volumes.
const volumeReconcileInterval = 30 * time.Second

// StartVolumeStatusReconciler keeps a volume's stored status honest.
//
// Status used to be written from whether the create call returned — a volume was
// marked ready because Kubernetes accepted the claim, not because storage
// existed. Claims that never bound therefore read as ready for months, and a
// claim deleted outside Meshploy still read as ready while every pod mounting it
// stayed unschedulable.
func (s *VolumeService) StartVolumeStatusReconciler(ctx context.Context) {
	if s.k8s == nil {
		return
	}
	ticker := time.NewTicker(volumeReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileVolumeStatuses(ctx)
		}
	}
}

func (s *VolumeService) reconcileVolumeStatuses(ctx context.Context) {
	var volumes []db.Volume
	if err := s.db.WithContext(ctx).Preload("Project").Find(&volumes).Error; err != nil {
		log.Printf("volume reconciler: list volumes: %v", err)
		return
	}

	// Node names are needed to spot a claim bound to a node that has left the
	// cluster. Fetched once per pass rather than per volume.
	liveNodes := map[string]bool{}
	var nodes []db.Node
	if err := s.db.WithContext(ctx).Find(&nodes).Error; err == nil {
		for _, n := range nodes {
			liveNodes[n.Name] = true
		}
	}

	for i := range volumes {
		vol := &volumes[i]
		if vol.Project.Slug == "" {
			continue
		}
		state, err := appk8s.GetVolumePVCStatus(ctx, s.k8s, vol.Slug, vol.Project.Slug)
		if err != nil {
			continue // transient API error — leave the row alone
		}
		want := deriveVolumeStatus(state, liveNodes)
		if want == vol.Status {
			continue
		}
		if err := s.db.WithContext(ctx).Model(vol).Update("status", want).Error; err != nil {
			log.Printf("volume reconciler: update %s: %v", vol.Name, err)
		}
	}
}

// deriveVolumeStatus maps the cluster's view of a claim onto a volume status.
//
// liveNodes may be empty, in which case the dead-node check is skipped rather
// than wrongly failing every bound volume.
func deriveVolumeStatus(state appk8s.VolumePVCStatus, liveNodes map[string]bool) db.VolumeStatus {
	switch {
	case !state.Exists:
		// Meshploy has a record with no claim behind it. Nothing mounting this
		// volume can ever schedule, and it does not resolve on its own.
		return db.VolumeFailed
	case state.Bound:
		// Local storage lives on one node. If that node is gone the data is
		// unreachable and no pod using the volume will ever schedule.
		if state.Node != "" && len(liveNodes) > 0 && !liveNodes[state.Node] {
			return db.VolumeFailed
		}
		return db.VolumeReady
	default:
		// Unbound: normal for a WaitForFirstConsumer claim nothing has mounted.
		return db.VolumeIdle
	}
}
