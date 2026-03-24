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

type CreateWorkloadInput struct {
	Name    string
	Image   string
	NodeID  *uuid.UUID // nil = let K3s schedule
	EnvVars db.EnvVarsMap

	// K8s resource spec — optional, defaults applied by the model
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	Replicas      int
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

func (s *WorkloadService) Create(ctx context.Context, projectID uuid.UUID, in CreateWorkloadInput) (*db.Service, error) {
	replicas := in.Replicas
	if replicas == 0 {
		replicas = 1
	}
	service := &db.Service{
		ProjectID: projectID,
		NodeID:    in.NodeID,
		Name:      in.Name,
		Type:      db.ServiceTypeApplication,
		Image:     in.Image,
		Status:    db.ServiceStopped,
		Replicas:  replicas,
		EnvVars:   in.EnvVars,
	}
	return service, s.db.WithContext(ctx).Create(service).Error
}

func (s *WorkloadService) Delete(ctx context.Context, serviceID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Service{}, "id = ?", serviceID).Error
}
