package service

import (
	"context"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// Everything a level's map is drawn from, in one read: its services, stacks,
// routes and volumes, why any service is not staying up and the advice from
// its last build, and which services read another's published variables. The
// same data each tab reads for itself, so the map and the tabs never disagree.

type ProjectMap struct {
	Services []db.Service        `json:"services"`
	Stacks   []db.Stack          `json:"stacks"`
	Routes   []db.Route          `json:"routes"`
	Volumes  []db.Volume         `json:"volumes"`
	Troubles map[string]*Trouble `json:"troubles"`
	Hints    map[string][]Hint   `json:"hints"`
	// Reads maps a service to the services whose published variables it
	// reads: it needs them up to be right.
	Reads map[string][]string `json:"reads"`
}

type ProjectMapService struct {
	db        *gorm.DB
	workloads *WorkloadService
	stacks    *StackService
	routes    *RouteService
	volumes   *VolumeService
}

func (s *ProjectMapService) Get(ctx context.Context, projectID uuid.UUID) (*ProjectMap, error) {
	out := &ProjectMap{Troubles: map[string]*Trouble{}, Hints: map[string][]Hint{}, Reads: map[string][]string{}}
	var err error
	if out.Services, err = s.workloads.List(ctx, projectID); err != nil {
		return nil, err
	}
	if out.Stacks, err = s.stacks.List(ctx, projectID); err != nil {
		return nil, err
	}
	if out.Routes, err = s.routes.ListByProject(ctx, projectID); err != nil {
		return nil, err
	}
	if out.Volumes, err = s.volumes.List(ctx, projectID); err != nil {
		return nil, err
	}
	troubles, err := s.workloads.Troubles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for id, t := range troubles {
		out.Troubles[id.String()] = t
	}
	hints, err := s.workloads.Hints(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for id, h := range hints {
		out.Hints[id.String()] = h
	}

	// One query for every service's attached groups that another service
	// publishes, instead of one read per service.
	ids := make([]uuid.UUID, 0, len(out.Services))
	for _, svc := range out.Services {
		ids = append(ids, svc.ID)
	}
	if len(ids) > 0 {
		var rows []struct {
			Reader uuid.UUID
			Owner  uuid.UUID
		}
		if err := s.db.WithContext(ctx).Model(&db.ServiceVariableGroup{}).
			Select("service_variable_groups.service_id AS reader, variable_groups.service_id AS owner").
			Joins("JOIN variable_groups ON variable_groups.id = service_variable_groups.group_id").
			Where("service_variable_groups.service_id IN ? AND variable_groups.service_id IS NOT NULL", ids).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.Owner != r.Reader {
				out.Reads[r.Reader.String()] = append(out.Reads[r.Reader.String()], r.Owner.String())
			}
		}
	}
	return out, nil
}
