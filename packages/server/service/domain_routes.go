package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DomainRoute is one hostname a domain carries, as the Domains page lists it.
//
// Flattened rather than returning routes: the page shows what is served on a
// name and where to go to change it. A route's targets are the route page's
// business.
type DomainRoute struct {
	ID          uuid.UUID `json:"id"`
	Hostname    string    `json:"hostname"`
	Subdomain   string    `json:"subdomain"`
	Zone        string    `json:"zone"`
	Published   bool      `json:"published"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	CreatedAt   time.Time `json:"created_at"`
	// Verified is only meaningful for a custom domain, where ownership is
	// proved per hostname rather than once for the zone.
	Verified bool `json:"verified"`
	// RedirectsTo is the hostname this route sends every request to, when that
	// is all it does - the redirect a move leaves behind on a retiring domain.
	// It still holds the domain, so the checklist says what it is rather than
	// offering to move something that only exists to point elsewhere.
	RedirectsTo string `json:"redirects_to,omitempty"`
}

// RoutesOnDomain lists every hostname served under one base domain, across
// every project.
//
// Across projects on purpose: a base domain is org-wide, and "what is still on
// this domain" is a question no single project can answer.
func (s *DomainService) RoutesOnDomain(ctx context.Context, orgID, domainID uuid.UUID) ([]DomainRoute, error) {
	return s.listRoutes(s.db.WithContext(ctx).
		Where("routes.organization_id = ? AND routes.domain_id = ?", orgID, domainID))
}

// CustomDomains lists the hostnames that belong to a route rather than to a
// base domain.
//
// Read-only on the Domains page. A custom domain is not a zone with subdomains
// under it, it is one name on one route, so it is configured from that route
// and appears here only so the page is the whole picture of what this server
// answers to.
func (s *DomainService) CustomDomains(ctx context.Context, orgID uuid.UUID) ([]DomainRoute, error) {
	return s.listRoutes(s.db.WithContext(ctx).
		Where("routes.organization_id = ? AND routes.domain_id IS NULL AND routes.hostname <> ''", orgID))
}

func (s *DomainService) listRoutes(q *gorm.DB) ([]DomainRoute, error) {
	out := make([]DomainRoute, 0)
	err := q.Table("routes").
		Select(`routes.id, routes.hostname, routes.subdomain, routes.zone, routes.published,
		        routes.project_id, projects.name AS project_name, routes.created_at,
		        routes.custom_domain_verified AS verified`).
		Joins("JOIN projects ON projects.id = routes.project_id").
		Order("routes.hostname ASC").
		Scan(&out).Error
	if err != nil || len(out) == 0 {
		return out, err
	}

	// A route whose only target is a redirect to another route.
	ids := make([]uuid.UUID, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	var redirects []struct {
		RouteID  uuid.UUID
		Hostname string
	}
	if err := s.db.Table("route_targets").
		Select("route_targets.route_id, dest.hostname").
		Joins("JOIN routes dest ON dest.id = route_targets.redirect_route_id").
		Where("route_targets.route_id IN ? AND route_targets.path = '/'", ids).
		Scan(&redirects).Error; err != nil {
		return nil, err
	}
	var targetCounts []struct {
		RouteID uuid.UUID
		N       int
	}
	if err := s.db.Table("route_targets").
		Select("route_id, COUNT(*) AS n").
		Where("route_id IN ?", ids).
		Group("route_id").
		Scan(&targetCounts).Error; err != nil {
		return nil, err
	}
	only := map[uuid.UUID]bool{}
	for _, c := range targetCounts {
		only[c.RouteID] = c.N == 1
	}
	to := map[uuid.UUID]string{}
	for _, r := range redirects {
		if only[r.RouteID] {
			to[r.RouteID] = r.Hostname
		}
	}
	for i := range out {
		out[i].RedirectsTo = to[out[i].ID]
	}
	return out, nil
}
