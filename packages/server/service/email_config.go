package service

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"

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

// ErrNoEmailConfig is an org with no SMTP settings to send with.
var ErrNoEmailConfig = errors.New("this workspace has no email settings: add them under Integrations, Email")

// Send mails a plain-text message with the org's SMTP settings, as
// notifications do, for an extension to reach people.
func (s *EmailConfigService) Send(ctx context.Context, orgID uuid.UUID, to, subject, body string) error {
	cfg, err := s.Get(ctx, orgID)
	if err != nil || cfg == nil || cfg.Host == "" {
		return ErrNoEmailConfig
	}
	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = mime.QEncoding.Encode("utf-8", cfg.FromName) + " <" + cfg.FromAddress + ">"
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n",
		from, to, mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z))
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return deliverEmail(*cfg, to, []byte(msg.String()))
}
