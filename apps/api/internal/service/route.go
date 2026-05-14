package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sort"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type RouteService struct {
	db *gorm.DB
}

// ── Input types ───────────────────────────────────────────────────────────────

type TargetInput struct {
	Path      string     // e.g. "/" or "/api" — defaults to "/" if empty
	StripPath bool
	ServiceID *uuid.UUID
	NodeID    *uuid.UUID
	Port      int        // used when NodeID is set
	// Pre-resolved (optional override — skips auto-resolution)
	TargetIP   string
	TargetPort int
}

type CreateRouteInput struct {
	OrgID     uuid.UUID
	ProjectID uuid.UUID

	// Domain-based (preferred): supply DomainID + Zone + Subdomain.
	DomainID  *uuid.UUID
	Zone      db.RouteZone
	Subdomain string

	// Manual fallback: raw hostname when DomainID is nil.
	Hostname string

	Targets []TargetInput
}

// ── List / Get ────────────────────────────────────────────────────────────────

func (s *RouteService) ListByProject(ctx context.Context, projectID uuid.UUID) ([]db.Route, error) {
	var routes []db.Route
	err := s.db.WithContext(ctx).
		Preload("Targets").
		Where("project_id = ?", projectID).
		Find(&routes).Error
	return routes, err
}

func (s *RouteService) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]db.Route, error) {
	var routes []db.Route
	err := s.db.WithContext(ctx).
		Preload("Targets").
		Where("organization_id = ?", orgID).
		Find(&routes).Error
	return routes, err
}

func (s *RouteService) Get(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	var route db.Route
	err := s.db.WithContext(ctx).Preload("Targets").First(&route, "id = ?", routeID).Error
	return &route, err
}

// ── Create ────────────────────────────────────────────────────────────────────

func (s *RouteService) Create(ctx context.Context, in CreateRouteInput) (*db.Route, error) {
	hostname := in.Hostname

	if in.DomainID != nil {
		var domain db.Domain
		if err := s.db.WithContext(ctx).First(&domain, "id = ?", *in.DomainID).Error; err != nil {
			return nil, huma.Error404NotFound("domain not found")
		}
		if !domain.Verified {
			return nil, huma.Error422UnprocessableEntity("domain ownership not yet verified")
		}
		if in.Zone == db.RouteZonePublic {
			if in.Subdomain == domain.InternalSubdomain || in.Subdomain == domain.PreviewSubdomain {
				return nil, huma.Error422UnprocessableEntity(
					fmt.Sprintf("subdomain %q is reserved for %s routing", in.Subdomain, in.Subdomain))
			}
		}
		switch in.Zone {
		case db.RouteZoneInternal:
			hostname = fmt.Sprintf("%s.%s.%s", in.Subdomain, domain.InternalSubdomain, domain.BaseDomain)
		case db.RouteZonePreview:
			hostname = fmt.Sprintf("%s.%s.%s", in.Subdomain, domain.PreviewSubdomain, domain.BaseDomain)
		default:
			hostname = fmt.Sprintf("%s.%s", in.Subdomain, domain.BaseDomain)
		}
	}

	route := &db.Route{
		OrganizationID: in.OrgID,
		ProjectID:      in.ProjectID,
		DomainID:       in.DomainID,
		Zone:           in.Zone,
		Subdomain:      in.Subdomain,
		Hostname:       hostname,
	}
	if in.DomainID == nil && hostname != "" {
		token, err := generateVerifyToken()
		if err != nil {
			return nil, fmt.Errorf("generate verify token: %w", err)
		}
		route.CustomDomainVerifyToken = token
	}

	if err := s.db.WithContext(ctx).Create(route).Error; err != nil {
		return nil, err
	}

	// Resolve and create each target.
	for i := range in.Targets {
		t := &in.Targets[i]
		if t.Path == "" {
			t.Path = "/"
		}
		target, err := s.resolveTarget(ctx, t)
		if err != nil {
			// Clean up the route row on target resolution failure.
			_ = s.db.WithContext(ctx).Delete(route).Error
			return nil, err
		}
		target.RouteID = route.ID
		if err := s.db.WithContext(ctx).Create(target).Error; err != nil {
			_ = s.db.WithContext(ctx).Delete(route).Error
			return nil, err
		}
		route.Targets = append(route.Targets, *target)
	}

	// Sort targets longest-path-first for consistent API response.
	sort.Slice(route.Targets, func(i, j int) bool {
		return len(route.Targets[i].Path) > len(route.Targets[j].Path)
	})

	return route, nil
}

// ── Target CRUD ───────────────────────────────────────────────────────────────

func (s *RouteService) AddTarget(ctx context.Context, routeID uuid.UUID, in TargetInput) (*db.RouteTarget, error) {
	if in.Path == "" {
		in.Path = "/"
	}
	target, err := s.resolveTarget(ctx, &in)
	if err != nil {
		return nil, err
	}
	target.RouteID = routeID
	return target, s.db.WithContext(ctx).Create(target).Error
}

func (s *RouteService) UpdateTarget(ctx context.Context, targetID uuid.UUID, in TargetInput) (*db.RouteTarget, error) {
	var target db.RouteTarget
	if err := s.db.WithContext(ctx).First(&target, "id = ?", targetID).Error; err != nil {
		return nil, huma.Error404NotFound("target not found")
	}
	if in.Path == "" {
		in.Path = "/"
	}
	resolved, err := s.resolveTarget(ctx, &in)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"path":        in.Path,
		"strip_path":  in.StripPath,
		"service_id":  in.ServiceID,
		"node_id":     in.NodeID,
		"target_ip":   resolved.TargetIP,
		"target_port": resolved.TargetPort,
	}
	err = s.db.WithContext(ctx).Model(&target).Updates(updates).Error
	return &target, err
}

func (s *RouteService) DeleteTarget(ctx context.Context, targetID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.RouteTarget{}, "id = ?", targetID).Error
}

// ── Route delete ──────────────────────────────────────────────────────────────

func (s *RouteService) Delete(ctx context.Context, routeID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Route{}, "id = ?", routeID).Error
}

// ── Custom domain verification ────────────────────────────────────────────────

func (s *RouteService) VerifyCustomHostname(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	route, err := s.Get(ctx, routeID)
	if err != nil {
		return nil, huma.Error404NotFound("route not found")
	}
	if route.DomainID != nil {
		return nil, huma.Error400BadRequest("route uses a managed domain — no custom-domain verification needed")
	}
	if route.CustomDomainVerified {
		return route, nil
	}
	records, err := net.LookupTXT("_meshploy-verify." + route.Hostname)
	if err != nil || !containsToken(records, route.CustomDomainVerifyToken) {
		return nil, huma.Error422UnprocessableEntity("TXT record not found or not yet propagated")
	}
	route.CustomDomainVerified = true
	err = s.db.WithContext(ctx).Model(route).Update("custom_domain_verified", true).Error
	return route, err
}

func (s *RouteService) IsCustomDomainVerified(ctx context.Context, hostname string) bool {
	var route db.Route
	err := s.db.WithContext(ctx).
		Where("hostname = ? AND domain_id IS NULL AND custom_domain_verified = ?", hostname, true).
		First(&route).Error
	return err == nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveTarget fills TargetIP and TargetPort from ServiceID or NodeID.
func (s *RouteService) resolveTarget(ctx context.Context, in *TargetInput) (*db.RouteTarget, error) {
	t := &db.RouteTarget{
		Path:       in.Path,
		StripPath:  in.StripPath,
		ServiceID:  in.ServiceID,
		NodeID:     in.NodeID,
		TargetIP:   in.TargetIP,
		TargetPort: in.TargetPort,
	}

	if in.ServiceID != nil {
		var svc db.Service
		if err := s.db.WithContext(ctx).First(&svc, "id = ?", *in.ServiceID).Error; err != nil {
			return nil, huma.Error404NotFound("service not found")
		}
		if svc.NodePort == 0 {
			return nil, huma.Error422UnprocessableEntity("service has not been deployed yet — deploy it first to create a route")
		}
		// Route through the K8s NodePort on the gateway node. kube-proxy distributes
		// traffic across all replicas regardless of which nodes they land on.
		var gateway db.Node
		if err := s.db.WithContext(ctx).
			Where("k3s_role = ? AND status = ?", db.K3sRoleServer, "online").
			First(&gateway).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("gateway node is not online — cannot resolve route target")
		}
		t.TargetIP = gateway.TailscaleIP
		t.TargetPort = svc.NodePort
	} else if in.NodeID != nil {
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, "id = ?", *in.NodeID).Error; err != nil {
			return nil, huma.Error404NotFound("node not found")
		}
		t.TargetIP = node.TailscaleIP
		t.TargetPort = in.Port
	}

	return t, nil
}

func generateVerifyToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func containsToken(records []string, token string) bool {
	for _, r := range records {
		if r == token {
			return true
		}
	}
	return false
}
