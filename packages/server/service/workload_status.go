package service

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	db "github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// statusReconcileInterval is how often stored service status is checked against
// the cluster. Slow enough to be cheap on a large project, fast enough that a
// stuck rollout stops claiming success within a minute.
const statusReconcileInterval = 30 * time.Second

// reconciledStatuses are the stored statuses worth checking against the
// cluster. Stopped is absent on purpose: it is a decision someone made rather
// than something observed, and the cluster agreeing that nothing runs would
// otherwise be read as news.
var reconciledStatuses = []db.ServiceStatus{db.ServiceRunning, db.ServiceDeploying, db.ServiceFailed}

// StartStatusReconciler keeps a service's stored status honest.
//
// Status used to be written from whether an API call returned without error —
// Start wrote "running" because scaling succeeded, even when the resulting pod
// could never be scheduled. The row then reported a service that was not
// running and never would be, with the real reason only visible in kubectl.
//
// A stopped service is left alone: that is a decision someone made, and this
// must not overrule it. Running, deploying and failed ones are all examined.
//
// Failure is not a decision, it is what the cluster last reported, so a service
// that recovers has to be allowed to say so. Leaving failed out meant a service
// stayed failed forever once it had been: a Keycloak whose pod was healthy
// again still read as failed, and the only way back was another deploy.
func (s *WorkloadService) StartStatusReconciler(ctx context.Context) {
	if s.k8s == nil {
		return
	}
	s.backfillDeployedSpecs(ctx)

	ticker := time.NewTicker(statusReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileStatuses(ctx)
			s.reconcileStackStatuses(ctx)
		}
	}
}

func (s *WorkloadService) reconcileStatuses(ctx context.Context) {
	var services []db.Service
	if err := s.db.WithContext(ctx).
		Preload("Project").
		Preload("DatabaseConfig").
		Where("status IN ?", reconciledStatuses).
		Find(&services).Error; err != nil {
		log.Printf("status reconciler: list services: %v", err)
		return
	}

	// Services with a deploy in flight are left to the deploy path, which owns
	// the outcome and sets it. Judging them here would race the rollout: a
	// service created moments ago has no Deployment in the cluster yet, and
	// "no Deployment" is read below as failure.
	var deploying []uuid.UUID
	s.db.WithContext(ctx).Model(&db.Deployment{}).
		Where("status IN ?", []db.DeploymentStatus{
			db.DeploymentPending, db.DeploymentBuilding, db.DeploymentDeploying,
		}).
		Distinct().Pluck("service_id", &deploying)
	inFlight := make(map[uuid.UUID]bool, len(deploying))
	for _, id := range deploying {
		inFlight[id] = true
	}

	for i := range services {
		svc := &services[i]
		if svc.Project.Slug == "" || inFlight[svc.ID] {
			continue
		}
		state, err := appk8s.GetDeploymentState(ctx, s.k8s, s.k8sName(ctx, svc), svc.Project.Slug)
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

// reconcileStackStatuses rolls a stack's status up from the services it owns.
//
// Apply wrote "idle" on success, so a stack serving traffic read exactly like
// one that had never been applied. Apply finishing only means the records were
// reconciled; whether the stack is actually up is a property of its services,
// and they are already kept honest against the cluster above.
func (s *WorkloadService) reconcileStackStatuses(ctx context.Context) {
	var stacks []db.Stack
	if err := s.db.WithContext(ctx).Find(&stacks).Error; err != nil {
		log.Printf("status reconciler: list stacks: %v", err)
		return
	}
	for i := range stacks {
		st := &stacks[i]
		// "applying" is NOT skipped, despite being what a running apply writes.
		//
		// Skipping it unconditionally made that state permanent when an apply was
		// interrupted. Skipping it for a grace period was worse in a subtler way:
		// deriveStackStatus returns "applying" for a stack whose services are
		// still rolling out, so the reconciler wrote that value and then read it
		// back as "an apply is in flight" and refused to look again — locking
		// itself out of correcting its own write for the length of the grace.
		//
		// Nothing is skipped now. A reconciler write during an apply is harmless:
		// Apply writes its final status after its work, so it wins, and the worst
		// case is a status that is briefly right instead of briefly stale.
		var svcs []db.Service
		if err := s.db.WithContext(ctx).
			Select("status").
			Where("stack_id = ?", st.ID).
			Find(&svcs).Error; err != nil {
			continue
		}
		// No services: never applied, or destroyed. Both are already described
		// by the stored status, and neither can be derived from an empty set.
		if len(svcs) == 0 {
			continue
		}
		want := deriveStackStatus(svcs)
		if want == "" || want == st.Status {
			continue
		}
		if err := s.db.WithContext(ctx).Model(st).Update("status", want).Error; err != nil {
			log.Printf("status reconciler: update stack %s: %v", st.Name, err)
		}
	}
}

// deriveStackStatus summarises a stack from its services.
//
// A failure anywhere wins: a stack that is half up is not working, and saying
// "running" would hide the part that is not. Otherwise anything running makes
// the stack running, since it is serving; a stack still rolling out reads as
// applying; and one whose services are all stopped is idle.
func deriveStackStatus(svcs []db.Service) db.StackStatus {
	var running, deploying, stopped int
	for _, svc := range svcs {
		switch svc.Status {
		case db.ServiceFailed:
			return db.StackFailed
		case db.ServiceRunning:
			running++
		case db.ServiceDeploying:
			deploying++
		case db.ServiceStopped:
			stopped++
		}
	}
	switch {
	case running > 0:
		return db.StackRunning
	case deploying > 0:
		return db.StackApplying
	case stopped == len(svcs):
		return db.StackIdle
	}
	return ""
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

// backfillDeployedSpecs records what is already running as deployed, once at
// startup.
//
// A service that has never recorded a fingerprint reads as behind, which is
// what makes a skipped rollout recoverable. Without this, the first apply after
// this shipped would redeploy every service that predates it. Running and
// stopped services are taken at their record: the cluster has a Deployment
// built from it, scaled to zero in the stopped case. Failed and deploying ones
// are left empty on purpose, since those are exactly the ones that may be
// behind.
func (s *WorkloadService) backfillDeployedSpecs(ctx context.Context) {
	var services []db.Service
	if err := s.db.WithContext(ctx).
		Preload("Ports").
		Where("deployed_spec_hash = ? AND status IN ?", "", []db.ServiceStatus{db.ServiceRunning, db.ServiceStopped}).
		Find(&services).Error; err != nil {
		log.Printf("deployed spec backfill: %v", err)
		return
	}
	for i := range services {
		if err := s.MarkDeployed(ctx, services[i].ID); err != nil {
			log.Printf("deployed spec backfill: %s: %v", services[i].Name, err)
		}
	}
	if len(services) > 0 {
		log.Printf("recorded what %d already-deployed services run", len(services))
	}
}

// MarkDeployed records a service's current spec as the one the cluster runs.
//
// The deploy paths call it when a rollout lands. It is exported because that is
// not the only way a service and the cluster come to agree: a backfill says so
// for services that were already running, and an operator tool can say so after
// putting things right by hand.
func (s *WorkloadService) MarkDeployed(ctx context.Context, serviceID uuid.UUID) error {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Ports").First(&svc, "id = ?", serviceID).Error; err != nil {
		return err
	}
	hash := fingerprint(storedServiceSpec(svc), portPrintsFromRows(svc.Ports))
	return s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ?", serviceID).
		Update("deployed_spec_hash", hash).Error
}
