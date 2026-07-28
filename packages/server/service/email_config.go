package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EmailConfigService struct {
	db *gorm.DB
}

type SaveEmailConfigInput struct {
	Host        string
	Port        int
	Username    string
	Password    string // empty = keep existing
	FromAddress string
	FromName    string
	UseTLS      bool
}

func (s *EmailConfigService) Get(ctx context.Context, orgID uuid.UUID) (*db.OrgEmailConfig, error) {
	var cfg db.OrgEmailConfig
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&cfg).Error
	return &cfg, err
}

func (s *EmailConfigService) Save(ctx context.Context, orgID uuid.UUID, in SaveEmailConfigInput) (*db.OrgEmailConfig, error) {
	port := in.Port
	if port == 0 {
		port = 587
	}

	// Upsert: if a row already exists, update it; otherwise create.
	existing, err := s.Get(ctx, orgID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	if err == gorm.ErrRecordNotFound {
		cfg := &db.OrgEmailConfig{
			OrganizationID: orgID,
			Host:           in.Host,
			Port:           port,
			Username:       in.Username,
			Password:       db.EncryptedString(in.Password),
			FromAddress:    in.FromAddress,
			FromName:       in.FromName,
			UseTLS:         in.UseTLS,
		}
		return cfg, s.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "organization_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"host", "port", "username", "password", "from_address", "from_name", "use_tls", "updated_at"}),
		}).Create(cfg).Error
	}

	updates := map[string]any{
		"host":         in.Host,
		"port":         port,
		"username":     in.Username,
		"from_address": in.FromAddress,
		"from_name":    in.FromName,
		"use_tls":      in.UseTLS,
	}
	if in.Password != "" {
		updates["password"] = db.EncryptedString(in.Password)
	}

	err = s.db.WithContext(ctx).Model(existing).Updates(updates).Error
	return existing, err
}

func (s *EmailConfigService) Delete(ctx context.Context, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Delete(&db.OrgEmailConfig{}).Error
}
