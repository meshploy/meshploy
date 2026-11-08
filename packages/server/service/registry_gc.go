package service

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
)

// Removing an image from the registry removes its manifest; its layers stay on
// disk until the registry's garbage collection runs, which needs the host (the
// API has neither the registry's files nor a container runtime). So the API
// asks the host agent for one, at most weekly, once images have been removed
// since the last, and never while a build may be pushing.

const (
	registryGCEvery = 7 * 24 * time.Hour
	registryGCCheck = time.Hour
)

// StartRegistryGC asks for the built-in registry's garbage collection when it
// is due, until ctx ends.
func (s *DeploymentService) StartRegistryGC(ctx context.Context) {
	t := time.NewTicker(registryGCCheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.requestRegistryGC(ctx, time.Now(), false); err != nil {
				log.Printf("warning: registry garbage collection: %v", err)
			}
		}
	}
}

// RequestRegistryGCNow asks for the registry's garbage collection without
// waiting for the week, as freeing a gateway's space does. It is still not
// asked while a build may be pushing. True when a request is waiting for the
// host agent.
func (s *DeploymentService) RequestRegistryGCNow(ctx context.Context) (bool, error) {
	return s.requestRegistryGC(ctx, time.Now(), true)
}

// requestRegistryGC files a registry.gc request when one is due, or at once
// with now set. True when one is waiting for the host agent.
func (s *DeploymentService) requestRegistryGC(ctx context.Context, now time.Time, force bool) (bool, error) {
	host := hostFolder(s.host, s.cfg)
	if host == nil || s.cfg == nil || (s.cfg.BuiltinRegistryEndpoint == "" && !s.cfg.ClusterAgent) {
		return false, nil
	}
	pending, err := hostagent.PendingRequests(host)
	if err != nil {
		return false, err
	}
	for _, req := range pending {
		if req.Type == hostagent.RequestRegistryGC {
			return true, nil // asked already, not yet taken
		}
	}
	var last time.Time
	if b, _, err := hostagent.ReadResult(host, hostagent.RegistryGCFile); err == nil && b != nil {
		var gc hostagent.RegistryGC
		if json.Unmarshal(b, &gc) == nil {
			last = gc.FinishedAt
		}
	}
	if !force {
		if !last.IsZero() && now.Sub(last) < registryGCEvery {
			return false, nil
		}
		var removed int64
		s.db.WithContext(ctx).Model(&db.Deployment{}).Where("image_removed_at > ?", last).Count(&removed)
		if removed == 0 {
			return false, nil
		}
	}
	var building int64
	s.db.WithContext(ctx).Model(&db.Deployment{}).
		Where("status IN ?", []db.DeploymentStatus{db.DeploymentPending, db.DeploymentBuilding}).Count(&building)
	if building > 0 {
		return false, nil // an hour from now, once no build is pushing
	}
	err = hostagent.QueueRequest(host, hostagent.Request{
		ID: uuid.NewString(), Type: hostagent.RequestRegistryGC, RequestedBy: "meshploy", RequestedAt: now.UTC(),
	})
	return err == nil, err
}
