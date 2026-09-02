package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

// OrphanWorkload is a workload running in the cluster that no service owns.
type OrphanWorkload struct {
	Name      string `json:"name"`
	Project   string `json:"project"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
	Ready     int32  `json:"ready"`
	AgeDays   int    `json:"age_days"`
	HasPVC    bool   `json:"has_pvc"`
}

// OrphanService reports cluster workloads that have no service behind them.
type OrphanService struct {
	db        *gorm.DB
	k8s       kubernetes.Interface
	workloads *WorkloadService
}

// List compares what meshploy has deployed against what it has records for and
// returns the difference, per project.
//
// Divergence is a state to surface, not a bug to be eliminated. The database
// holds the desired state and the cluster the actual one; a failed delete, a
// restore from backup, or someone reaching for kubectl separates them. A
// control plane that cannot say so lets workloads run for months with nothing
// pointing at them, quietly holding memory on a node.
//
// Deliberately read-only. Reporting is safe under every failure mode and
// deleting is not: the difference between a query that returned no services
// because there are none and one that returned none because it failed is
// invisible at the point of use, and one of those readings deletes everything.
func (s *OrphanService) List(ctx context.Context, orgID uuid.UUID) ([]OrphanWorkload, error) {
	out := []OrphanWorkload{}
	if s.k8s == nil {
		return out, nil
	}
	var projects []db.Project
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&projects).Error; err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	for _, p := range projects {
		if p.Slug == "" {
			continue
		}
		// Every service in this project, under the K8s name it would carry. An
		// error aborts the whole report rather than yielding a list that wrongly
		// claims everything is unowned.
		var services []db.Service
		if err := s.db.WithContext(ctx).
			Preload("DatabaseConfig").
			Where("project_id = ?", p.ID).
			Find(&services).Error; err != nil {
			return nil, fmt.Errorf("list services for %s: %w", p.Slug, err)
		}
		owned := make(map[string]bool, len(services))
		for i := range services {
			owned[s.workloads.k8sName(ctx, &services[i])] = true
		}

		managed, err := appk8s.ListManagedWorkloads(ctx, s.k8s, p.Slug)
		if err != nil {
			continue // the namespace may not exist yet — not a reason to fail the report
		}
		for _, w := range managed {
			if owned[w.Name] {
				continue
			}
			out = append(out, OrphanWorkload{
				Name:      w.Name,
				Project:   p.Name,
				Namespace: w.Namespace,
				Replicas:  w.Replicas,
				Ready:     w.Ready,
				AgeDays:   w.AgeDays,
				HasPVC:    w.HasPVC,
			})
		}
	}
	return out, nil
}
