package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type SecretService struct {
	db *gorm.DB
}

// SecretWithMaskedValue is Secret with the value replaced by a masked placeholder
// so callers never receive plaintext over the wire.
type SecretWithMaskedValue struct {
	db.Secret
	Masked bool `json:"masked"`
}

func (s *SecretService) List(ctx context.Context, projectID uuid.UUID) ([]db.Secret, error) {
	var rows []db.Secret
	if err := s.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("name").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *SecretService) Create(ctx context.Context, projectID uuid.UUID, name, value string) (*db.Secret, error) {
	row := db.Secret{
		ProjectID: projectID,
		Name:      name,
		Value:     db.EncryptedString(value),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *SecretService) Update(ctx context.Context, secretID uuid.UUID, value string) (*db.Secret, error) {
	var row db.Secret
	if err := s.db.WithContext(ctx).First(&row, "id = ?", secretID).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&row).Update("value", db.EncryptedString(value)).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *SecretService) Delete(ctx context.Context, secretID uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&db.Secret{}, "id = ?", secretID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("not found")
	}
	return nil
}

// ─── Service attachments ──────────────────────────────────────────────────────

func (s *SecretService) ListAttachments(ctx context.Context, serviceID uuid.UUID) ([]db.ServiceSecret, error) {
	var rows []db.ServiceSecret
	if err := s.db.WithContext(ctx).
		Preload("Secret").
		Where("service_id = ?", serviceID).
		Order("env_key").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *SecretService) Attach(ctx context.Context, serviceID, secretID uuid.UUID, envKey string) (*db.ServiceSecret, error) {
	row := db.ServiceSecret{
		ServiceID: serviceID,
		SecretID:  secretID,
		EnvKey:    envKey,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *SecretService) Detach(ctx context.Context, attachmentID uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&db.ServiceSecret{}, "id = ?", attachmentID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("not found")
	}
	return nil
}

// ResolveForService returns a map of envKey→plaintext value for all secrets
// attached to a service. Used by the deployment service to inject env vars.
func (s *SecretService) ResolveForService(ctx context.Context, serviceID uuid.UUID) (map[string]string, error) {
	var attachments []db.ServiceSecret
	if err := s.db.WithContext(ctx).
		Preload("Secret").
		Where("service_id = ?", serviceID).
		Find(&attachments).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(attachments))
	for _, a := range attachments {
		result[a.EnvKey] = string(a.Secret.Value)
	}
	return result, nil
}
