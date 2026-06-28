package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

var wildcardSubdomainRe = regexp.MustCompile(`^\*\.[a-z0-9-]+$`)

type RouteService struct {
	db *gorm.DB
}

// ── Input types ───────────────────────────────────────────────────────────────

type TargetInput struct {
	Path          string
	StripPath     bool
	ServiceID     *uuid.UUID
	ServicePortID *uuid.UUID // which port to route to; nil = primary port
	NodeID        *uuid.UUID
	Port          int
	// Pre-resolved (optional override — skips auto-resolution)
	TargetIP   string
	TargetPort int
	// Redirect target — mutually exclusive with ServiceID / NodeID
	RedirectRouteID *uuid.UUID
	RedirectCode    int // 301 or 302; defaults to 301 if zero
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

func (s *RouteService) Get(ctx context.Context, routeID, projectID uuid.UUID) (*db.Route, error) {
	var route db.Route
	err := s.db.WithContext(ctx).Preload("Targets").
		First(&route, "id = ? AND project_id = ?", routeID, projectID).Error
	return &route, err
}

func (s *RouteService) getByID(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	var route db.Route
	err := s.db.WithContext(ctx).Preload("Targets").First(&route, "id = ?", routeID).Error
	return &route, err
}

// ── Create ────────────────────────────────────────────────────────────────────

// platformReservedSubdomains are gateway-level names that cannot be used as route subdomains.
var platformReservedSubdomains = map[string]bool{
	// Meshploy infrastructure
	"console":   true,
	"api":       true,
	"mesh":      true,
	"headscale": true,
	"preview":   true,
	"internal":  true,
	// Standard DNS / internet conventions
	"www":       true,
	"mail":      true,
	"smtp":      true,
	"mx":        true,
	"ns":        true,
	"ns1":       true,
	"ns2":       true,
	// Bare wildcard
	"*":         true,
}

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
		if platformReservedSubdomains[in.Subdomain] {
			return nil, huma.Error422UnprocessableEntity(
				fmt.Sprintf("subdomain %q is reserved and cannot be used", in.Subdomain))
		}
		// Also block the domain's configured zone subdomains from being used as route subdomains.
		if in.Subdomain == domain.InternalSubdomain || in.Subdomain == domain.PreviewSubdomain {
			return nil, huma.Error422UnprocessableEntity(
				fmt.Sprintf("subdomain %q is reserved for zone routing", in.Subdomain))
		}
		// Wildcard subdomains (*.label) are only valid on the public zone.
		if strings.Contains(in.Subdomain, "*") {
			if in.Zone != db.RouteZonePublic {
				return nil, huma.Error422UnprocessableEntity("wildcard subdomains are only supported on the public zone")
			}
			if !wildcardSubdomainRe.MatchString(in.Subdomain) {
				return nil, huma.Error422UnprocessableEntity("wildcard subdomain must be in the format *.label (e.g. *.my-app)")
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
		if err := s.validateRedirectTarget(ctx, route.ID, in.Zone, t); err != nil {
			_ = s.db.WithContext(ctx).Delete(route).Error
			return nil, err
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
	var route db.Route
	if err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error; err != nil {
		return nil, huma.Error404NotFound("route not found")
	}
	if err := s.validateRedirectTarget(ctx, routeID, route.Zone, &in); err != nil {
		return nil, err
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
	var route db.Route
	if err := s.db.WithContext(ctx).First(&route, "id = ?", target.RouteID).Error; err != nil {
		return nil, huma.Error404NotFound("route not found")
	}
	if err := s.validateRedirectTarget(ctx, target.RouteID, route.Zone, &in); err != nil {
		return nil, err
	}
	resolved, err := s.resolveTarget(ctx, &in)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"path":              in.Path,
		"strip_path":        in.StripPath,
		"service_id":        in.ServiceID,
		"node_id":           in.NodeID,
		"target_ip":         resolved.TargetIP,
		"target_port":       resolved.TargetPort,
		"redirect_route_id": in.RedirectRouteID,
		"redirect_code":     resolved.RedirectCode,
	}
	if err = s.db.WithContext(ctx).Model(&target).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err = s.db.WithContext(ctx).First(&target, "id = ?", target.ID).Error; err != nil {
		return nil, err
	}
	return &target, nil
}

func (s *RouteService) DeleteTarget(ctx context.Context, targetID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.RouteTarget{}, "id = ?", targetID).Error
}

// ── Route delete ──────────────────────────────────────────────────────────────

func (s *RouteService) Delete(ctx context.Context, routeID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Where("resource_type = ? AND resource_id = ?", db.ResourceRoute, routeID).
			Delete(&db.ResourcePermission{})
		return tx.Delete(&db.Route{}, "id = ?", routeID).Error
	})
}

// ── Custom domain verification ────────────────────────────────────────────────

func (s *RouteService) VerifyCustomHostname(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	route, err := s.getByID(ctx, routeID)
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

// HasRoute reports whether any route exists for the exact hostname. Used by the
// on-demand TLS ask endpoint (self-managed DNS mode) to authorize certs for
// active workload subdomains under the base domain.
func (s *RouteService) HasRoute(ctx context.Context, hostname string) bool {
	var route db.Route
	err := s.db.WithContext(ctx).Where("hostname = ?", hostname).First(&route).Error
	return err == nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// validateRedirectTarget enforces zone and chain rules when a redirect target is requested.
func (s *RouteService) validateRedirectTarget(ctx context.Context, routeID uuid.UUID, zone db.RouteZone, in *TargetInput) error {
	if in.RedirectRouteID == nil {
		return nil
	}
	if zone == db.RouteZoneInternal {
		return huma.Error422UnprocessableEntity("redirect targets are not supported on internal routes")
	}
	if *in.RedirectRouteID == routeID {
		return huma.Error422UnprocessableEntity("a route cannot redirect to itself")
	}
	// Ensure the target route has no redirects of its own (no multi-hop).
	var count int64
	s.db.WithContext(ctx).Model(&db.RouteTarget{}).
		Where("route_id = ? AND redirect_route_id IS NOT NULL", *in.RedirectRouteID).
		Count(&count)
	if count > 0 {
		return huma.Error422UnprocessableEntity("redirect target cannot itself contain redirects — multi-hop chains are not allowed")
	}
	return nil
}

// resolveTarget fills TargetIP/TargetPort from ServiceID or NodeID, or sets redirect fields.
func (s *RouteService) resolveTarget(ctx context.Context, in *TargetInput) (*db.RouteTarget, error) {
	code := in.RedirectCode
	if code == 0 {
		code = 301
	}
	t := &db.RouteTarget{
		Path:            in.Path,
		StripPath:       in.StripPath,
		ServiceID:       in.ServiceID,
		NodeID:          in.NodeID,
		TargetIP:        in.TargetIP,
		TargetPort:      in.TargetPort,
		RedirectRouteID: in.RedirectRouteID,
		RedirectCode:    code,
	}

	if in.RedirectRouteID != nil {
		// Verify the target route exists.
		var target db.Route
		if err := s.db.WithContext(ctx).First(&target, "id = ?", *in.RedirectRouteID).Error; err != nil {
			return nil, huma.Error404NotFound("redirect target route not found")
		}
		return t, nil
	}

	if in.ServiceID != nil {
		// Resolve to the correct ServicePort (primary if unspecified).
		var sp db.ServicePort
		var err error
		if in.ServicePortID != nil {
			err = s.db.WithContext(ctx).
				Where("id = ? AND service_id = ? AND is_public = true AND is_http = true", *in.ServicePortID, *in.ServiceID).
				First(&sp).Error
		} else {
			err = s.db.WithContext(ctx).
				Where("service_id = ? AND is_primary = true AND is_public = true AND is_http = true", *in.ServiceID).
				First(&sp).Error
			if err != nil {
				// Fall back to any public HTTP port.
				err = s.db.WithContext(ctx).
					Where("service_id = ? AND is_public = true AND is_http = true", *in.ServiceID).
					Order("created_at ASC").First(&sp).Error
			}
		}
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("no routable port found — deploy the service and ensure it has a public HTTP port")
		}
		if sp.NodePort == 0 {
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
		t.TargetPort = sp.NodePort
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
	return slices.Contains(records, token)
}
