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

func (s *ProjectService) List(ctx context.Context, orgID uuid.UUID) ([]db.Project, error) {
	projects := make([]db.Project, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&projects).Error
	return projects, err
}

func (s *ProjectService) Get(ctx context.Context, projectID uuid.UUID) (*db.Project, error) {
	var project db.Project
	err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error
	return &project, err
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
