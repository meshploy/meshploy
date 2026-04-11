package service

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type RouteService struct {
	db *gorm.DB
}

type CreateRouteInput struct {
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	ServiceID *uuid.UUID

	// Domain-based fields (preferred path)
	DomainID *uuid.UUID
	Zone     db.RouteZone // public | internal | preview
	Subdomain string

	// Legacy / manual fallback: supply a raw hostname directly
	// (used when DomainID is nil)
	Hostname string

	TargetIP   string
	TargetPort int
}

func (s *RouteService) ListByProject(ctx context.Context, projectID uuid.UUID) ([]db.Route, error) {
	routes := make([]db.Route, 0)
	err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Find(&routes).Error
	return routes, err
}

func (s *RouteService) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]db.Route, error) {
	routes := make([]db.Route, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&routes).Error
	return routes, err
}

func (s *RouteService) Get(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	var route db.Route
	err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error
	return &route, err
}

func (s *RouteService) Create(ctx context.Context, in CreateRouteInput) (*db.Route, error) {
	hostname := in.Hostname

	if in.DomainID != nil {
		var domain db.Domain
		if err := s.db.WithContext(ctx).First(&domain, "id = ?", *in.DomainID).Error; err != nil {
			return nil, huma.Error404NotFound("domain not found")
		}

		// Ownership must be verified before routes can be created.
		if !domain.Verified {
			return nil, huma.Error422UnprocessableEntity("domain ownership not yet verified")
		}

		// Block reserved subdomains from being used as public routes.
		if in.Zone == db.RouteZonePublic {
			if in.Subdomain == domain.InternalSubdomain || in.Subdomain == domain.PreviewSubdomain {
				return nil, huma.Error422UnprocessableEntity(
					fmt.Sprintf("subdomain %q is reserved for %s routing",
						in.Subdomain, in.Subdomain))
			}
		}

		// Derive the full hostname from zone + subdomain.
		switch in.Zone {
		case db.RouteZoneInternal:
			hostname = fmt.Sprintf("%s.%s.%s", in.Subdomain, domain.InternalSubdomain, domain.BaseDomain)
		case db.RouteZonePreview:
			hostname = fmt.Sprintf("%s.%s.%s", in.Subdomain, domain.PreviewSubdomain, domain.BaseDomain)
		default: // public
			hostname = fmt.Sprintf("%s.%s", in.Subdomain, domain.BaseDomain)
		}
	}

	route := &db.Route{
		OrganizationID: in.OrgID,
		ProjectID:      in.ProjectID,
		ServiceID:      in.ServiceID,
		DomainID:       in.DomainID,
		Zone:           in.Zone,
		Subdomain:      in.Subdomain,
		Hostname:       hostname,
		TargetIP:       in.TargetIP,
		TargetPort:     in.TargetPort,
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
