package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type StorageService struct {
	db *gorm.DB
}

type CreateStorageInput struct {
	Name            string
	Provider        db.StorageProvider
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

func (s *StorageService) List(ctx context.Context, orgID uuid.UUID) ([]db.StorageIntegration, error) {
	items := make([]db.StorageIntegration, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&items).Error
	return items, err
}

func (s *StorageService) Create(ctx context.Context, orgID uuid.UUID, in CreateStorageInput) (*db.StorageIntegration, error) {
	item := &db.StorageIntegration{
		OrganizationID:  orgID,
		Name:            in.Name,
		Provider:        in.Provider,
		Endpoint:        in.Endpoint,
		Region:          in.Region,
		Bucket:          in.Bucket,
		AccessKeyID:     db.EncryptedString(in.AccessKeyID),
		SecretAccessKey: db.EncryptedString(in.SecretAccessKey),
	}
	return item, s.db.WithContext(ctx).Create(item).Error
}

func (s *StorageService) Delete(ctx context.Context, id, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		Delete(&db.StorageIntegration{}).Error
}
