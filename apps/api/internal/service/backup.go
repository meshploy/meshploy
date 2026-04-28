package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type BackupService struct {
	db *gorm.DB
}

// ─── Per-service backup configs ───────────────────────────────────────────────

type CreateBackupInput struct {
	StorageIntegrationID uuid.UUID
	Schedule             string
	RetentionDays        int
	PathPrefix           string
}

type UpdateBackupInput struct {
	Schedule      *string
	RetentionDays *int
	PathPrefix    *string
	Enabled       *bool
}

func (s *BackupService) List(ctx context.Context, serviceID uuid.UUID) ([]db.BackupConfig, error) {
	items := make([]db.BackupConfig, 0)
	err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).Find(&items).Error
	return items, err
}

func (s *BackupService) Create(ctx context.Context, orgID, serviceID uuid.UUID, in CreateBackupInput) (*db.BackupConfig, error) {
	// Verify the storage integration belongs to this org.
	var sto db.StorageIntegration
	if err := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", in.StorageIntegrationID, orgID).First(&sto).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("storage integration not found")
		}
		return nil, err
	}
	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	item := &db.BackupConfig{
		ServiceID:            serviceID,
		StorageIntegrationID: in.StorageIntegrationID,
		Schedule:             in.Schedule,
		RetentionDays:        retentionDays,
		PathPrefix:           in.PathPrefix,
		Enabled:              true,
	}
	return item, s.db.WithContext(ctx).Create(item).Error
}

func (s *BackupService) Update(ctx context.Context, id, serviceID uuid.UUID, in UpdateBackupInput) (*db.BackupConfig, error) {
	var item db.BackupConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND service_id = ?", id, serviceID).First(&item).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if in.Schedule != nil {
		updates["schedule"] = *in.Schedule
	}
	if in.RetentionDays != nil {
		updates["retention_days"] = *in.RetentionDays
	}
	if in.PathPrefix != nil {
		updates["path_prefix"] = *in.PathPrefix
	}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&item).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return &item, nil
}

func (s *BackupService) Delete(ctx context.Context, id, serviceID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND service_id = ?", id, serviceID).
		Delete(&db.BackupConfig{}).Error
}

// ─── System backup config ─────────────────────────────────────────────────────

type UpsertSystemBackupInput struct {
	StorageIntegrationID uuid.UUID
	Schedule             string
	RetentionDays        int
	PathPrefix           string
	Enabled              bool
}

func (s *BackupService) GetSystem(ctx context.Context, orgID uuid.UUID) (*db.SystemBackupConfig, error) {
	var item db.SystemBackupConfig
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (s *BackupService) UpsertSystem(ctx context.Context, orgID uuid.UUID, in UpsertSystemBackupInput) (*db.SystemBackupConfig, error) {
	// Verify the storage integration belongs to this org.
	var sto db.StorageIntegration
	if err := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", in.StorageIntegrationID, orgID).First(&sto).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("storage integration not found")
		}
		return nil, err
	}
	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}

	var item db.SystemBackupConfig
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		item = db.SystemBackupConfig{
			OrganizationID:       orgID,
			StorageIntegrationID: in.StorageIntegrationID,
			Schedule:             in.Schedule,
			RetentionDays:        retentionDays,
			PathPrefix:           in.PathPrefix,
			Enabled:              in.Enabled,
		}
		return &item, s.db.WithContext(ctx).Create(&item).Error
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"storage_integration_id": in.StorageIntegrationID,
		"schedule":               in.Schedule,
		"retention_days":         retentionDays,
		"path_prefix":            in.PathPrefix,
		"enabled":                in.Enabled,
	}
	return &item, s.db.WithContext(ctx).Model(&item).Updates(updates).Error
}

func (s *BackupService) DeleteSystem(ctx context.Context, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Delete(&db.SystemBackupConfig{}).Error
}
