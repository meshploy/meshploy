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
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

var wildcardSubdomainRe = regexp.MustCompile(`^\*\.[a-z0-9-]+$`)

type RouteService struct {
	db  *gorm.DB
	k8s kubernetes.Interface
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
	// TargetTLS: the target speaks HTTPS, so the hop to it does too. Only
	// meaningful with an address target: a service or node target is reached on
	// a NodePort inside the cluster, which is plain HTTP by construction.
	TargetTLS bool
	// AllowUnresolved keeps a target whose service has no NodePort yet, for a
	// route created paused. Publishing resolves it.
	AllowUnresolved bool
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

	// Paused creates the route without serving it. The zero value publishes,
	// which is what creating a route has always done.
	Paused bool
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
	"www":  true,
	"mail": true,
	"smtp": true,
	"mx":   true,
	"ns":   true,
	"ns1":  true,
	"ns2":  true,
	// Bare wildcard
	"*": true,
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
		// The console hides a retiring domain from its picker; this is the same
		// rule for every other caller. It is what makes the list of routes still
		// holding the domain one that only shrinks.
		if domain.RetiringAt != nil {
			return nil, huma.Error422UnprocessableEntity(
				fmt.Sprintf("%s is being retired, so no new routes can use it", domain.BaseDomain))
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
		hostname = hostnameFor(in.Zone, in.Subdomain, &domain)
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

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(route).Error; err != nil {
			return err
		}
		// GORM leaves a false bool out of the insert when the column has a
		// default, so a paused route is written as published and then paused,
		// inside the transaction so the proxy never sees it live.
		if in.Paused {
			route.Published = false
			return tx.Model(route).Update("published", false).Error
		}
		return nil
	}); err != nil {
		// The hostname is unique across the table, which is the real guarantee:
		// the console checks what it can see, and two people creating the same
		// name at once still meet here. Say which name, rather than handing back
		// a constraint violation.
		if isUniqueViolation(err) {
			return nil, huma.Error409Conflict(fmt.Sprintf("%s is already routed", hostname))
		}
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
		target, err := s.resolveTarget(ctx, route.OrganizationID, t)
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
	target, err := s.resolveTarget(ctx, route.OrganizationID, &in)
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
	resolved, err := s.resolveTarget(ctx, route.OrganizationID, &in)
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
		"target_tls":        resolved.TargetTLS,
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
	// A route another route redirects to cannot simply go: the redirect would
	// point at nothing. The database refuses it, and an operator deserves to
	// hear which route is in the way rather than a foreign-key error.
	var redirected []string
	if err := s.db.WithContext(ctx).Model(&db.Route{}).
		Joins("JOIN route_targets ON route_targets.route_id = routes.id").
		Where("route_targets.redirect_route_id = ?", routeID).
		Distinct().Pluck("routes.hostname", &redirected).Error; err != nil {
		return err
	}
	if len(redirected) > 0 {
		return huma.Error409Conflict(fmt.Sprintf(
			"%s redirects here — point it somewhere else before deleting this route",
			strings.Join(redirected, ", ")))
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Where("resource_type = ? AND resource_id = ?", db.ResourceRoute, routeID).
			Delete(&db.ResourcePermission{})
		return tx.Delete(&db.Route{}, "id = ?", routeID).Error
	})
}

// ── Moving a route to another base domain ─────────────────────────────────────

// MoveRouteInput moves a route's hostname from one base domain to another,
// keeping its subdomain, zone and targets.
type MoveRouteInput struct {
	RouteID   uuid.UUID
	ProjectID uuid.UUID
	DomainID  uuid.UUID
	// KeepRedirect leaves the old hostname answering with a 301 to the new one,
	// so links already out there keep working. The redirect is itself a route
	// on the old domain, and holds it until it is deleted: a grace period that
	// ends when somebody decides it has.
	KeepRedirect bool
}

// MoveRoute is how a route gets off a domain that is being retired.
//
// Only a route on a base domain moves. A custom hostname is a whole name, not a
// subdomain that could live under another zone, so there is nothing to move it
// to.
func (s *RouteService) MoveRoute(ctx context.Context, in MoveRouteInput) (moved *db.Route, redirect *db.Route, err error) {
	route, err := s.Get(ctx, in.RouteID, in.ProjectID)
	if err != nil {
		return nil, nil, huma.Error404NotFound("route not found")
	}
	if route.DomainID == nil {
		return nil, nil, huma.Error422UnprocessableEntity(
			"a custom hostname is a whole name, not a subdomain - it cannot move to another base domain")
	}
	if *route.DomainID == in.DomainID {
		return nil, nil, huma.Error422UnprocessableEntity("the route is already on that domain")
	}
	var target db.Domain
	if err := s.db.WithContext(ctx).First(&target, "id = ? AND organization_id = ?", in.DomainID, route.OrganizationID).Error; err != nil {
		return nil, nil, huma.Error404NotFound("domain not found")
	}
	if !target.Verified {
		return nil, nil, huma.Error422UnprocessableEntity(fmt.Sprintf("%s is not verified yet", target.BaseDomain))
	}
	if target.RetiringAt != nil {
		return nil, nil, huma.Error422UnprocessableEntity(
			fmt.Sprintf("%s is being retired itself, so no routes can move onto it", target.BaseDomain))
	}
	if route.Subdomain == target.InternalSubdomain || route.Subdomain == target.PreviewSubdomain {
		return nil, nil, huma.Error422UnprocessableEntity(
			fmt.Sprintf("subdomain %q is reserved for zone routing on %s", route.Subdomain, target.BaseDomain))
	}
	if in.KeepRedirect {
		// Redirects are an HTTP answer from the public edge, and do not chain.
		if route.Zone != db.RouteZonePublic {
			return nil, nil, huma.Error422UnprocessableEntity("only a public route can leave a redirect behind")
		}
		for _, t := range route.Targets {
			if t.RedirectRouteID != nil {
				return nil, nil, huma.Error422UnprocessableEntity(
					"this route redirects itself, and redirects do not chain - move it without leaving one behind")
			}
		}
	}

	oldHostname, oldDomainID := route.Hostname, *route.DomainID
	newHostname := hostnameFor(route.Zone, route.Subdomain, &target)

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The hostname is unique across the table, so the old name has to be
		// released before a redirect can claim it.
		if err := tx.Model(route).Updates(map[string]any{
			"domain_id": target.ID,
			"hostname":  newHostname,
		}).Error; err != nil {
			return err
		}
		if !in.KeepRedirect {
			return nil
		}
		// Written directly rather than through Create, which refuses a retiring
		// domain. This is the one route a retiring domain may still gain: it
		// exists to wind the domain down, not to keep it in use.
		redirect = &db.Route{
			OrganizationID: route.OrganizationID,
			ProjectID:      route.ProjectID,
			DomainID:       &oldDomainID,
			Zone:           route.Zone,
			Subdomain:      route.Subdomain,
			Hostname:       oldHostname,
		}
		if err := tx.Create(redirect).Error; err != nil {
			return err
		}
		target := &db.RouteTarget{
			RouteID:         redirect.ID,
			Path:            "/",
			RedirectRouteID: &route.ID,
			RedirectCode:    301,
		}
		if err := tx.Create(target).Error; err != nil {
			return err
		}
		redirect.Targets = []db.RouteTarget{*target}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil, huma.Error409Conflict(fmt.Sprintf("%s is already routed", newHostname))
		}
		return nil, nil, err
	}
	route.DomainID, route.Hostname = &target.ID, newHostname
	return route, redirect, nil
}

// hostnameFor is the hostname a subdomain has in a zone of a base domain.
func hostnameFor(zone db.RouteZone, subdomain string, d *db.Domain) string {
	switch zone {
	case db.RouteZoneInternal:
		return fmt.Sprintf("%s.%s.%s", subdomain, d.InternalSubdomain, d.BaseDomain)
	case db.RouteZonePreview:
		return fmt.Sprintf("%s.%s.%s", subdomain, d.PreviewSubdomain, d.BaseDomain)
	default:
		return fmt.Sprintf("%s.%s", subdomain, d.BaseDomain)
	}
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
	// The column defaults to empty, and an empty token would be "found" in any
	// TXT record set that happens to hold an empty string - a proof of nothing.
	// Issue one instead, and say so: the record to add has only just come into
	// existence.
	if route.CustomDomainVerifyToken == "" {
		token, err := generateVerifyToken()
		if err != nil {
			return nil, fmt.Errorf("generate verify token: %w", err)
		}
		if err := s.db.WithContext(ctx).Model(route).Update("custom_domain_verify_token", token).Error; err != nil {
			return nil, err
		}
		return nil, huma.Error422UnprocessableEntity(
			"this route had no verification record yet; one has been issued - add it and check again")
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
		Where("hostname = ? AND domain_id IS NULL AND custom_domain_verified = ? AND published = ?", hostname, true, true).
		First(&route).Error
	return err == nil
}

// HasRoute reports whether a published route exists for the exact hostname.
// Used by the on-demand TLS ask endpoint (self-managed DNS mode) to authorize
// certs for active workload subdomains under the base domain. A paused route
// gets no certificate: it is not live.
func (s *RouteService) HasRoute(ctx context.Context, hostname string) bool {
	// The proxy serves a.my-app.acme.dev from a route for *.my-app.acme.dev (one
	// label up, the way its cache matches), so the certificate check has to
	// accept it too. Matching only the exact name left every wildcard route
	// routed and never certified: its names fell through to the on-demand
	// catch-all, which refused them.
	names := []string{hostname}
	if i := strings.IndexByte(hostname, '.'); i > 0 && !strings.HasPrefix(hostname, "*.") {
		names = append(names, "*."+hostname[i+1:])
	}
	var route db.Route
	err := s.db.WithContext(ctx).Where("hostname IN ? AND published = ?", names, true).First(&route).Error
	return err == nil
}

// SetPublished publishes or pauses a route and records who did it. The proxy
// picks the change up on its next refresh.
func (s *RouteService) SetPublished(ctx context.Context, routeID, projectID uuid.UUID, published bool, by uuid.UUID) (*db.Route, error) {
	route, err := s.Get(ctx, routeID, projectID)
	if err != nil {
		return nil, err
	}
	// A route created paused may point at a service that had not deployed yet,
	// so its address was never worked out. Publishing is when that has to be
	// true, so it is resolved here - and a target that still cannot be resolved
	// stops the publish rather than serving a hostname that goes nowhere.
	if published {
		if err := s.resolvePausedTargets(ctx, route); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(route).Updates(map[string]any{
		"published":            published,
		"published_changed_at": now,
		"published_changed_by": by,
	}).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, routeID, projectID)
}

// resolvePausedTargets fills in the address of any service target that was
// created before its service had one.
func (s *RouteService) resolvePausedTargets(ctx context.Context, route *db.Route) error {
	for _, t := range route.Targets {
		if t.ServiceID == nil || t.TargetPort != 0 {
			continue
		}
		resolved, err := s.resolveTarget(ctx, route.OrganizationID, &TargetInput{
			Path: t.Path, StripPath: t.StripPath, ServiceID: t.ServiceID,
		})
		if err != nil {
			return err
		}
		if err := s.db.WithContext(ctx).Model(&db.RouteTarget{}).Where("id = ?", t.ID).
			Updates(map[string]any{"target_ip": resolved.TargetIP, "target_port": resolved.TargetPort}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// validateRedirectTarget enforces zone and chain rules when a redirect target is requested.
// serviceInOrg reports whether this service belongs to the organisation, by the
// project that owns it. A target naming a service from elsewhere is a 404 and
// not a 403: the caller has no business knowing the id exists.
func (s *RouteService) serviceInOrg(ctx context.Context, orgID, serviceID uuid.UUID) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&db.Service{}).
		Joins("JOIN projects ON projects.id = services.project_id").
		Where("services.id = ? AND projects.organization_id = ?", serviceID, orgID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return huma.Error404NotFound("service not found")
	}
	return nil
}

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
// resolveTarget turns a target input into a stored row.
//
// orgID is the organisation of the route this target belongs to, and every
// lookup here is scoped to it. Authorising the route says nothing about what it
// may point at: without this, anyone who can edit one route could name any
// service, node or route id in the database and have the gateway forward to it.
func (s *RouteService) resolveTarget(ctx context.Context, orgID uuid.UUID, in *TargetInput) (*db.RouteTarget, error) {
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
		TargetTLS:       in.TargetTLS && in.ServiceID == nil && in.NodeID == nil,
		RedirectRouteID: in.RedirectRouteID,
		RedirectCode:    code,
	}

	if in.RedirectRouteID != nil {
		// Verify the target route exists, in this organisation.
		var target db.Route
		if err := s.db.WithContext(ctx).First(&target, "id = ? AND organization_id = ?", *in.RedirectRouteID, orgID).Error; err != nil {
			return nil, huma.Error404NotFound("redirect target route not found")
		}
		return t, nil
	}

	if in.ServiceID != nil {
		if err := s.serviceInOrg(ctx, orgID, *in.ServiceID); err != nil {
			return nil, err
		}
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
		// A missing NodePort in the row does not mean the service is undeployed.
		// The cluster assigns the port and the row only mirrors it afterwards, so
		// a deploy that assigned one but failed before recording it — or a service
		// deployed by an older version — leaves a running workload with a zero
		// here. Refusing on that basis told people to deploy a service that had
		// been serving traffic for months.
		//
		// So ask the cluster, and write the answer back: the drift is repaired by
		// the first thing that notices it.
		if sp.NodePort == 0 && s.k8s != nil {
			var svc db.Service
			if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", *in.ServiceID).Error; err == nil && svc.Project.Slug != "" {
				if np, err := appk8s.GetNodePort(ctx, s.k8s, appK8sName(&svc), svc.Project.Slug, int32(sp.Port)); err == nil && np != 0 {
					sp.NodePort = int(np)
					s.db.WithContext(ctx).Model(&db.ServicePort{}).Where("id = ?", sp.ID).Update("node_port", np)
				}
			}
		}
		if sp.NodePort == 0 {
			// A paused route serves nothing, so it does not need an address
			// yet. This is how a migration prepares: services are created
			// stopped and their routes paused, and neither has a NodePort until
			// the group moves. The address is resolved when the route is
			// published, which is the moment it starts to matter.
			if in.AllowUnresolved {
				t.TargetPort = 0
				return t, nil
			}
			return nil, huma.Error422UnprocessableEntity(
				"no NodePort is published for this service — deploy it, then add the route")
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
	} else if in.TargetIP != "" {
		// An address, taken as written. The proxy runs on the gateway with its
		// network namespace, so anything the gateway reaches is fair - loopback
		// included, which is the only way to name a container bound to it.
		if net.ParseIP(in.TargetIP) == nil {
			return nil, huma.Error400BadRequest(in.TargetIP + " is not an address")
		}
		if t.TargetPort == 0 {
			t.TargetPort = in.Port
		}
		if t.TargetPort < 1 || t.TargetPort > 65535 {
			return nil, huma.Error400BadRequest("a target port must be between 1 and 65535")
		}
	} else {
		// Nothing to dial. This used to be accepted, and produced a route that
		// answered every request with a proxy error.
		return nil, huma.Error400BadRequest("a target needs a service, a node, an address, or a route to redirect to")
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
