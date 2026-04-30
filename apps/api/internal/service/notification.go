package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type NotificationService struct {
	db *gorm.DB
}

type CreateNotificationInput struct {
	Name   string
	Type   meshdb.NotificationChannelType
	Config map[string]string
	Events []string
}

type UpdateNotificationInput struct {
	Name    *string
	Config  map[string]string
	Events  []string
	Enabled *bool
}

func (s *NotificationService) List(ctx context.Context, orgID uuid.UUID) ([]meshdb.NotificationChannel, error) {
	var rows []meshdb.NotificationChannel
	err := s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}

func (s *NotificationService) Create(ctx context.Context, orgID uuid.UUID, in CreateNotificationInput) (*meshdb.NotificationChannel, error) {
	switch in.Type {
	case meshdb.NotificationEmail, meshdb.NotificationWebhook:
	default:
		return nil, fmt.Errorf("unsupported channel type %q — supported: email, webhook", in.Type)
	}
	if err := validateNotificationConfig(in.Type, in.Config); err != nil {
		return nil, err
	}

	cfg := make(meshdb.JSONObject, len(in.Config))
	for k, v := range in.Config {
		cfg[k] = v
	}

	row := meshdb.NotificationChannel{
		OrganizationID: orgID,
		Name:           in.Name,
		Type:           in.Type,
		Config:         cfg,
		Events:         in.Events,
		Enabled:        true,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *NotificationService) Update(ctx context.Context, id, orgID uuid.UUID, in UpdateNotificationInput) (*meshdb.NotificationChannel, error) {
	var row meshdb.NotificationChannel
	if err := s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		First(&row).Error; err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Config != nil {
		if err := validateNotificationConfig(row.Type, in.Config); err != nil {
			return nil, err
		}
		cfg := make(meshdb.JSONObject, len(in.Config))
		for k, v := range in.Config {
			cfg[k] = v
		}
		updates["config"] = cfg
	}
	if in.Events != nil {
		updates["events"] = meshdb.StringArray(in.Events)
	}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
	}

	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *NotificationService) Delete(ctx context.Context, id, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		Delete(&meshdb.NotificationChannel{}).Error
}

func validateNotificationConfig(t meshdb.NotificationChannelType, cfg map[string]string) error {
	switch t {
	case meshdb.NotificationEmail:
		if cfg["address"] == "" {
			return fmt.Errorf("email channel requires config.address")
		}
	case meshdb.NotificationWebhook:
		if cfg["url"] == "" {
			return fmt.Errorf("webhook channel requires config.url")
		}
	}
	return nil
}
