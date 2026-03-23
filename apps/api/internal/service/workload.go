package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type WorkloadService struct {
	db *gorm.DB
}

func (s *WorkloadService) List(ctx context.Context, projectID uuid.UUID) ([]db.Service, error) {
	var services []db.Service
	err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Find(&services).Error
	return services, err
}

func (s *WorkloadService) Get(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var service db.Service
	err := s.db.WithContext(ctx).First(&service, "id = ?", serviceID).Error
	return &service, err
}

func (s *WorkloadService) Create(ctx context.Context, projectID, nodeID uuid.UUID, name, image string, port int, envVars db.EnvVarsMap) (*db.Service, error) {
	service := &db.Service{
		ProjectID:    projectID,
		NodeID:       nodeID,
		Name:         name,
		Image:        image,
		InternalPort: port,
		EnvVars:      envVars,
		Status:       db.ServiceStopped,
	}
	return service, s.db.WithContext(ctx).Create(service).Error
}

func (s *WorkloadService) Delete(ctx context.Context, serviceID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Service{}, "id = ?", serviceID).Error
}
