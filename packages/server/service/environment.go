package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// Environment levels.
//
// A project is its own production level, and each level below it is a project
// row of its own that points at it (db.Project.ParentProjectID) with its own
// namespace. Everything that scopes by project - services, routes, volumes,
// deploys, permissions - therefore works inside a level unchanged, and what is
// new is small: the order of the levels, creating one in place, and keeping a
// project and its levels together (listing, naming, deleting, access).
//
// A level holds only what has been put in it. Services arrive there by being
// added or promoted, and whatever a level does not have is borrowed from the
// nearest level above; neither is part of creating a level.

// EnvironmentLevel is one level of a project, as the console lists them.
type EnvironmentLevel struct {
	ProjectID      uuid.UUID `json:"project_id"`
	Name           string    `json:"name"`
	Level          int       `json:"level"`
	Namespace      string    `json:"namespace"`
	Production     bool      `json:"production"`
	ServicesCount  int       `json:"services_count"`
	DatabasesCount int       `json:"databases_count"`
}

// Errors a caller can explain to the operator.
var (
	ErrLevelName        = errors.New("a level name is lower-case letters, digits and hyphens, starting with a letter")
	ErrLevelProduction  = errors.New("production is the project itself; name the new level something else")
	ErrLevelTaken       = errors.New("this project already has a level with that name")
	ErrAboveProduction  = errors.New("nothing goes above production: it is where every service ends up")
	ErrLevelNotInChain  = errors.New("that level belongs to another project")
	ErrProjectHasLevels = errors.New("this project still has environment levels: remove them first")
)

var levelNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`)

// maxNamespace is Kubernetes' limit on a namespace name, which a level's
// namespace, <project slug>-<level name>, has to fit in.
const maxNamespace = 63

// RootProject returns the project a level belongs to: the production level.
// For a project that is not a level, that is the project itself.
func (s *ProjectService) RootProject(ctx context.Context, projectID uuid.UUID) (*db.Project, error) {
	p, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.ParentProjectID == nil {
		return p, nil
	}
	return s.Get(ctx, *p.ParentProjectID)
}

// Levels lists every level of the project projectID belongs to, production
// first and then down the chain. projectID may be any of them.
func (s *ProjectService) Levels(ctx context.Context, projectID uuid.UUID) ([]EnvironmentLevel, error) {
	root, err := s.RootProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var rows []db.Project
	if err := s.db.WithContext(ctx).
		Where("id = ? OR parent_project_id = ?", root.ID, root.ID).
		Order("env_level").Order("id").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	type count struct {
		ProjectID uuid.UUID
		Type      db.ServiceType
		N         int
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var counts []count
	if err := s.db.WithContext(ctx).Model(&db.Service{}).
		Select("project_id, type, COUNT(*) AS n").
		Where("project_id IN ?", ids).
		Group("project_id, type").
		Scan(&counts).Error; err != nil {
		return nil, err
	}

	out := make([]EnvironmentLevel, len(rows))
	for i, r := range rows {
		out[i] = EnvironmentLevel{
			ProjectID:  r.ID,
			Name:       r.EnvName,
			Level:      r.EnvLevel,
			Namespace:  r.Slug,
			Production: r.ParentProjectID == nil,
		}
		for _, c := range counts {
			if c.ProjectID != r.ID {
				continue
			}
			if c.Type == db.ServiceTypeDatabase {
				out[i].DatabasesCount += c.N
			} else {
				out[i].ServicesCount += c.N
			}
		}
	}
	return out, nil
}

// CreateLevel adds a level to the chain projectID belongs to, directly above or
// below the level relativeTo, and returns it. It starts empty: nothing is
// copied into it, because a level holds only the services put there, and
// borrows the rest from the level above.
func (s *ProjectService) CreateLevel(ctx context.Context, projectID uuid.UUID, name string, relativeTo uuid.UUID, above bool) (*db.Project, error) {
	if !levelNamePattern.MatchString(name) {
		return nil, ErrLevelName
	}
	if name == "production" {
		return nil, ErrLevelProduction
	}
	root, err := s.RootProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	anchor, err := s.Get(ctx, relativeTo)
	if err != nil {
		return nil, err
	}
	if anchor.ID != root.ID && (anchor.ParentProjectID == nil || *anchor.ParentProjectID != root.ID) {
		return nil, ErrLevelNotInChain
	}
	if above && anchor.ID == root.ID {
		return nil, ErrAboveProduction
	}

	namespace := root.Slug + "-" + name
	if len(namespace) > maxNamespace {
		return nil, fmt.Errorf("the level's namespace, %q, is longer than Kubernetes allows (%d characters): choose a shorter name", namespace, maxNamespace)
	}

	// Above a level takes its place and pushes it and everything below down;
	// below a level takes the place under it and pushes the rest down.
	position := anchor.EnvLevel + 1
	if above {
		position = anchor.EnvLevel
	}

	level := &db.Project{
		OrganizationID:  root.OrganizationID,
		Name:            root.Name,
		Slug:            namespace,
		ParentProjectID: &root.ID,
		EnvName:         name,
		EnvLevel:        position,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var taken int64
		if err := tx.Model(&db.Project{}).
			Where("(id = ? OR parent_project_id = ?) AND env_name = ?", root.ID, root.ID, name).
			Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrLevelTaken
		}
		// The namespace is cluster-wide, so another project may hold it already.
		if err := tx.Model(&db.Project{}).Where("slug = ?", namespace).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return fmt.Errorf("the namespace %q is already taken by another project", namespace)
		}
		if err := tx.Model(&db.Project{}).
			Where("parent_project_id = ? AND env_level >= ?", root.ID, position).
			Update("env_level", gorm.Expr("env_level + 1")).Error; err != nil {
			return err
		}
		return tx.Create(level).Error
	})
	if err != nil {
		return nil, err
	}
	return level, nil
}

// closeGap renumbers a chain after a level has gone, so its levels stay 0, 1,
// 2... with nothing implied by a missing number.
func closeGap(tx *gorm.DB, rootID uuid.UUID, removed int) error {
	return tx.Model(&db.Project{}).
		Where("parent_project_id = ? AND env_level > ?", rootID, removed).
		Update("env_level", gorm.Expr("env_level - 1")).Error
}

// ErrRenameProduction refuses renaming production: it is the project itself.
var ErrRenameProduction = errors.New("production is the project itself; rename the project instead")

// RenameLevel renames a level and re-derives its routes' hostnames, so a level
// named qa never serves -staging names: app-staging becomes app-qa, and links
// to the old names stop working. The namespace does not change - Kubernetes
// cannot rename one - so the level keeps it under its new name. Returns how
// many hostnames changed.
func (s *ProjectService) RenameLevel(ctx context.Context, levelID uuid.UUID, name string) (int, error) {
	if !levelNamePattern.MatchString(name) {
		return 0, ErrLevelName
	}
	if name == "production" {
		return 0, ErrLevelProduction
	}
	level, err := s.Get(ctx, levelID)
	if err != nil {
		return 0, err
	}
	if level.ParentProjectID == nil {
		return 0, ErrRenameProduction
	}
	if level.EnvName == name {
		return 0, nil
	}
	root := *level.ParentProjectID
	changed := 0
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var taken int64
		if err := tx.Model(&db.Project{}).
			Where("(id = ? OR parent_project_id = ?) AND env_name = ?", root, root, name).
			Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrLevelTaken
		}
		// A route anywhere in the project already ending -<name> would collide
		// with the names this level is about to derive.
		var chain []uuid.UUID
		if err := tx.Model(&db.Project{}).Where("id = ? OR parent_project_id = ?", root, root).Pluck("id", &chain).Error; err != nil {
			return err
		}
		var clash int64
		if err := tx.Model(&db.Route{}).
			Where("project_id IN ? AND domain_id IS NOT NULL AND subdomain LIKE ?", chain, "%-"+name).
			Count(&clash).Error; err != nil {
			return err
		}
		if clash > 0 {
			return fmt.Errorf("a route in this project already ends in -%s, which this level would derive: choose another name", name)
		}
		if err := tx.Model(&db.Project{}).Where("id = ?", levelID).Update("env_name", name).Error; err != nil {
			return err
		}
		var routes []db.Route
		if err := tx.Where("project_id = ? AND domain_id IS NOT NULL", levelID).Find(&routes).Error; err != nil {
			return err
		}
		for _, r := range routes {
			var domain db.Domain
			if err := tx.First(&domain, "id = ?", *r.DomainID).Error; err != nil {
				return err
			}
			hostname := hostnameFor(r.Zone, levelSubdomain(r.Subdomain, name), &domain)
			if hostname == r.Hostname {
				continue
			}
			if err := tx.Model(&db.Route{}).Where("id = ?", r.ID).Update("hostname", hostname).Error; err != nil {
				if isUniqueViolation(err) {
					return fmt.Errorf("%s is already routed", hostname)
				}
				return err
			}
			changed++
		}
		return nil
	})
	return changed, err
}
