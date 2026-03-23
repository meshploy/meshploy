package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type DeploymentService struct {
	db *gorm.DB
}

func (s *DeploymentService) List(ctx context.Context, serviceID uuid.UUID) ([]db.Deployment, error) {
	var deployments []db.Deployment
	err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).Order("created_at DESC").Find(&deployments).Error
	return deployments, err
}

func (s *DeploymentService) Get(ctx context.Context, deploymentID uuid.UUID) (*db.Deployment, error) {
	var d db.Deployment
	err := s.db.WithContext(ctx).First(&d, "id = ?", deploymentID).Error
	return &d, err
}
