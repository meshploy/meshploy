package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type RegistryService struct {
	db *gorm.DB
}

type CreateRegistryInput struct {
	Name      string
	Provider  db.RegistryProvider
	Endpoint  string
	Namespace string
	Username  string
	Password  string
}

func (s *RegistryService) List(ctx context.Context, orgID uuid.UUID) ([]db.RegistryIntegration, error) {
	var items []db.RegistryIntegration
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&items).Error
	return items, err
}

func (s *RegistryService) Create(ctx context.Context, orgID uuid.UUID, in CreateRegistryInput) (*db.RegistryIntegration, error) {
	item := &db.RegistryIntegration{
		OrganizationID: orgID,
		Name:           in.Name,
		Provider:       in.Provider,
		Endpoint:       in.Endpoint,
		Namespace:      in.Namespace,
		Username:       db.EncryptedString(in.Username),
		Password:       db.EncryptedString(in.Password),
	}
	return item, s.db.WithContext(ctx).Create(item).Error
}

func (s *RegistryService) Delete(ctx context.Context, id, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		Delete(&db.RegistryIntegration{}).Error
}
