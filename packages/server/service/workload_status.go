package service

import (
	"context"
	"log"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
	db "github.com/meshploy/packages/db"
)

// statusReconcileInterval is how often stored service status is checked against
// the cluster. Slow enough to be cheap on a large project, fast enough that a
// stuck rollout stops claiming success within a minute.
const statusReconcileInterval = 30 * time.Second

// StartStatusReconciler keeps a service's stored status honest.
//
// Status used to be written from whether an API call returned without error —
// Start wrote "running" because scaling succeeded, even when the resulting pod
// could never be scheduled. The row then reported a service that was not
// running and never would be, with the real reason only visible in kubectl.
//
// Only services already marked running or deploying are examined. A stopped or
// failed service reflects a decision someone made, and this must not overrule
// it; drift in that direction is left alone deliberately.
func (s *WorkloadService) StartStatusReconciler(ctx context.Context) {
	if s.k8s == nil {
		return
	}
	ticker := time.NewTicker(statusReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileStatuses(ctx)
		}
	}
}

func (s *WorkloadService) reconcileStatuses(ctx context.Context) {
	var services []db.Service
	if err := s.db.WithContext(ctx).
		Preload("Project").
		Where("status IN ?", []db.ServiceStatus{db.ServiceRunning, db.ServiceDeploying}).
		Find(&services).Error; err != nil {
		log.Printf("status reconciler: list services: %v", err)
		return
	}

	for i := range services {
		svc := &services[i]
		if svc.Project.Slug == "" {
			continue
		}
		state, err := appk8s.GetDeploymentState(ctx, s.k8s, slugify(svc.Name), svc.Project.Slug)
		if err != nil {
			continue // transient API error — leave the row as it is
		}
		want := deriveServiceStatus(state)
		if want == "" || want == svc.Status {
			continue
		}
		if err := s.db.WithContext(ctx).Model(svc).Update("status", want).Error; err != nil {
			log.Printf("status reconciler: update %s: %v", svc.Name, err)
		}
	}
}

// deriveServiceStatus maps what the cluster reports onto a service status.
// Returns "" when the cluster says nothing conclusive and the stored value
// should be left alone.
func deriveServiceStatus(state appk8s.DeploymentState) db.ServiceStatus {
	switch {
	case !state.Exists:
		// Marked running with no Deployment behind it: something removed the
		// workload out from under us, which is a failure rather than a stop.
		return db.ServiceFailed
	case state.Replicas == 0:
		return db.ServiceStopped
	case state.Available > 0:
		return db.ServiceRunning
	case state.Stalled:
		// K8s gave up: unschedulable pod, unpullable image, missing claim.
		return db.ServiceFailed
	default:
		// Replicas wanted, none ready yet, still progressing.
		return db.ServiceDeploying
	}
}
