package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type ProjectService struct {
	db *gorm.DB
}

// ProjectCounts holds per-project resource counts returned alongside the project list.
// Add new fields here as new resource types are introduced — the SQL query in
// ListWithCounts uses a single CASE-based aggregation so adding a field is one line.
type ProjectCounts struct {
	ProjectID      uuid.UUID `gorm:"column:project_id"`
	ServicesCount  int       `gorm:"column:services_count"  json:"services_count"`
	DatabasesCount int       `gorm:"column:databases_count" json:"databases_count"`
	RoutesCount    int       `gorm:"column:routes_count"    json:"routes_count"`
	SecretsCount   int       `gorm:"column:secrets_count"   json:"secrets_count"`
	JobsCount      int       `gorm:"column:jobs_count"      json:"jobs_count"`
}

// ProjectWithCounts bundles a project with its resource counts.
type ProjectWithCounts struct {
	db.Project
	ProjectCounts
}

func (s *ProjectService) List(ctx context.Context, orgID uuid.UUID) ([]db.Project, error) {
	projects := make([]db.Project, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&projects).Error
	return projects, err
}

// ListWithCounts returns all projects for an org with resource counts embedded.
// A single SQL aggregation query fetches counts for all projects at once — no N+1.
func (s *ProjectService) ListWithCounts(ctx context.Context, orgID uuid.UUID) ([]ProjectWithCounts, error) {
	projects := make([]db.Project, 0)
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&projects).Error; err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return []ProjectWithCounts{}, nil
	}

	projectIDs := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		projectIDs[i] = p.ID
	}

	// Single aggregation query across services and routes.
	// Extend by adding more COALESCE(sub.xxx_count, 0) columns here.
	var counts []ProjectCounts
	s.db.WithContext(ctx).Raw(`
		SELECT
			p.id AS project_id,
			COALESCE(s.services_count,  0) AS services_count,
			COALESCE(s.databases_count, 0) AS databases_count,
			COALESCE(r.routes_count,    0) AS routes_count,
			COALESCE(sec.secrets_count, 0) AS secrets_count,
			COALESCE(j.jobs_count,      0) AS jobs_count
		FROM projects p
		LEFT JOIN (
			SELECT project_id,
				COUNT(*) FILTER (WHERE type = 'application') AS services_count,
				COUNT(*) FILTER (WHERE type = 'database')    AS databases_count
			FROM services
			WHERE project_id IN ?
			GROUP BY project_id
		) s ON s.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS routes_count
			FROM routes
			WHERE project_id IN ?
			GROUP BY project_id
		) r ON r.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS secrets_count
			FROM secrets
			WHERE project_id IN ?
			GROUP BY project_id
		) sec ON sec.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS jobs_count
			FROM jobs
			WHERE project_id IN ?
			GROUP BY project_id
		) j ON j.project_id = p.id
		WHERE p.id IN ?
	`, projectIDs, projectIDs, projectIDs, projectIDs, projectIDs).Scan(&counts)

	// Index counts by project ID for O(1) lookup.
	countMap := make(map[uuid.UUID]ProjectCounts, len(counts))
	for _, c := range counts {
		countMap[c.ProjectID] = c
	}

	result := make([]ProjectWithCounts, len(projects))
	for i, p := range projects {
		result[i] = ProjectWithCounts{
			Project:       p,
			ProjectCounts: countMap[p.ID],
		}
	}
	return result, nil
}

func (s *ProjectService) Get(ctx context.Context, projectID uuid.UUID) (*db.Project, error) {
	var project db.Project
	err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error
	return &project, err
}

func (s *ProjectService) GetWithCounts(ctx context.Context, projectID uuid.UUID) (*ProjectWithCounts, error) {
	project, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var counts []ProjectCounts
	s.db.WithContext(ctx).Raw(`
		SELECT
			s.project_id,
			COUNT(*) FILTER (WHERE s.type = 'application') AS services_count,
			COUNT(*) FILTER (WHERE s.type = 'database')    AS databases_count,
			(SELECT COUNT(*) FROM routes r WHERE r.project_id = s.project_id) AS routes_count,
			(SELECT COUNT(*) FROM secrets sec WHERE sec.project_id = s.project_id) AS secrets_count,
			(SELECT COUNT(*) FROM jobs j WHERE j.project_id = s.project_id) AS jobs_count
		FROM services s
		WHERE s.project_id = ?
		GROUP BY s.project_id
	`, projectID).Scan(&counts)
	result := &ProjectWithCounts{Project: *project}
	if len(counts) > 0 {
		result.ProjectCounts = counts[0]
	}
	return result, nil
}

func (s *ProjectService) Create(ctx context.Context, orgID uuid.UUID, name, slug string) (*db.Project, error) {
	project := &db.Project{OrganizationID: orgID, Name: name, Slug: slug}
	return project, s.db.WithContext(ctx).Create(project).Error
}

func (s *ProjectService) Update(ctx context.Context, projectID uuid.UUID, name string) (*db.Project, error) {
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Model(&project).Update("name", name).Error
	return &project, err
}

func (s *ProjectService) Delete(ctx context.Context, projectID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Project{}, "id = ?", projectID).Error
}
