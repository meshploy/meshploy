package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type RouteService struct {
	db *gorm.DB
}

func (s *RouteService) ListByProject(ctx context.Context, projectID uuid.UUID) ([]db.Route, error) {
	var routes []db.Route
	err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Find(&routes).Error
	return routes, err
}

func (s *RouteService) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]db.Route, error) {
	var routes []db.Route
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&routes).Error
	return routes, err
}

func (s *RouteService) Get(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	var route db.Route
	err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error
	return &route, err
}

func (s *RouteService) Create(ctx context.Context, orgID, projectID uuid.UUID, serviceID *uuid.UUID, hostname, targetIP string, targetPort int) (*db.Route, error) {
	route := &db.Route{
		OrganizationID: orgID,
		ProjectID:      projectID,
		ServiceID:      serviceID,
		Hostname:       hostname,
		TargetIP:       targetIP,
		TargetPort:     targetPort,
	}
	return route, s.db.WithContext(ctx).Create(route).Error
}

func (s *RouteService) Update(ctx context.Context, routeID uuid.UUID, targetIP string, targetPort int) (*db.Route, error) {
	var route db.Route
	if err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error; err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Model(&route).Updates(map[string]any{
		"target_ip":   targetIP,
		"target_port": targetPort,
	}).Error
	return &route, err
}

func (s *RouteService) Delete(ctx context.Context, routeID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Route{}, "id = ?", routeID).Error
}
