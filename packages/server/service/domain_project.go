package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// A base domain is the organization's, for every project to route on, unless
// it is reserved for one project (domains.project_id). Then only that project
// and its environment levels may put routes on it, and no other is offered it:
// where several people's projects share an organization, as spaces on shared
// servers do, each one's address is theirs alone.

// ErrProjectHasDomain: a domain is reserved for the project being deleted.
var ErrProjectHasDomain = errors.New("a base domain is reserved for this project")

// rootProject is the project a level belongs to, or the project itself.
func rootProject(ctx context.Context, gdb *gorm.DB, projectID uuid.UUID) (uuid.UUID, error) {
	var p db.Project
	if err := gdb.WithContext(ctx).Select("id", "parent_project_id").First(&p, "id = ?", projectID).Error; err != nil {
		return uuid.Nil, err
	}
	if p.ParentProjectID != nil {
		return *p.ParentProjectID, nil
	}
	return p.ID, nil
}

// domainServesProject reports whether routes in projectID may use d.
func domainServesProject(ctx context.Context, gdb *gorm.DB, d *db.Domain, projectID uuid.UUID) (bool, error) {
	if d.ProjectID == nil {
		return true, nil
	}
	root, err := rootProject(ctx, gdb, projectID)
	if err != nil {
		return false, err
	}
	return root == *d.ProjectID, nil
}

// checkDomainForProject refuses a domain reserved for another project, with
// the same words whether or not the caller could see that project.
func checkDomainForProject(ctx context.Context, gdb *gorm.DB, d *db.Domain, projectID uuid.UUID) error {
	ok, err := domainServesProject(ctx, gdb, d, projectID)
	if err != nil {
		return err
	}
	if !ok {
		return huma.Error422UnprocessableEntity(fmt.Sprintf("%s is reserved for another project", d.BaseDomain))
	}
	return nil
}

// domainsReservedFor names the base domains reserved for a project.
func domainsReservedFor(ctx context.Context, gdb *gorm.DB, projectID uuid.UUID) ([]string, error) {
	var names []string
	err := gdb.WithContext(ctx).Model(&db.Domain{}).Where("project_id = ?", projectID).
		Order("base_domain").Pluck("base_domain", &names).Error
	return names, err
}

// refuseIfDomainReserved is the check before a project is deleted: its
// domain's records are removed with the domain, which is its own decision.
func refuseIfDomainReserved(ctx context.Context, gdb *gorm.DB, projectID uuid.UUID) error {
	names, err := domainsReservedFor(ctx, gdb, projectID)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("%w: remove %s first", ErrProjectHasDomain, strings.Join(names, ", "))
	}
	return nil
}

// ReserveForProject reserves a domain for one project of its organization,
// or, with nil, gives it back to every project. A level is reserved through
// its project, so only a top-level project is accepted. Refused while routes
// of other projects are on the domain: reserving it would strand them.
func (s *DomainService) ReserveForProject(ctx context.Context, domainID uuid.UUID, projectID *uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if projectID == nil {
		domain.ProjectID = nil
		return domain, s.db.WithContext(ctx).Model(domain).Update("project_id", nil).Error
	}
	if err := s.CheckReservable(ctx, domain.OrganizationID, *projectID); err != nil {
		return nil, err
	}
	if domain.IsPrimary || domain.FormerPrimary {
		return nil, huma.Error422UnprocessableEntity(
			"the platform's own names are served on this domain, so it cannot be reserved for one project")
	}
	var others int64
	if err := s.db.WithContext(ctx).Model(&db.Route{}).
		Joins("JOIN projects ON projects.id = routes.project_id").
		Where("routes.domain_id = ? AND projects.id <> ? AND (projects.parent_project_id IS NULL OR projects.parent_project_id <> ?)",
			domainID, *projectID, *projectID).
		Count(&others).Error; err != nil {
		return nil, err
	}
	if others > 0 {
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf(
			"%d route(s) of other projects use %s: move them first", others, domain.BaseDomain))
	}
	domain.ProjectID = projectID
	return domain, s.db.WithContext(ctx).Model(domain).Update("project_id", *projectID).Error
}

// CheckReservable refuses a project a domain cannot be reserved for: one of
// another organization, or an environment level.
func (s *DomainService) CheckReservable(ctx context.Context, orgID, projectID uuid.UUID) error {
	var p db.Project
	err := s.db.WithContext(ctx).Select("id", "parent_project_id").
		First(&p, "id = ? AND organization_id = ?", projectID, orgID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return huma.Error422UnprocessableEntity("no such project in this organization")
	}
	if err != nil {
		return err
	}
	if p.ParentProjectID != nil {
		return huma.Error422UnprocessableEntity("reserve the domain for the project, not one of its environment levels: its levels share it")
	}
	return nil
}
