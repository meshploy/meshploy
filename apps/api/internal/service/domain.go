package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type DomainService struct {
	db *gorm.DB
}

type CreateDomainInput struct {
	BaseDomain        string
	InternalSubdomain string // defaults to "internal" if empty
	PreviewSubdomain  string // defaults to "preview" if empty
}

type UpdateDomainInput struct {
	InternalSubdomain string
	PreviewSubdomain  string
}

func (s *DomainService) List(ctx context.Context, orgID uuid.UUID) ([]db.Domain, error) {
	var domains []db.Domain
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&domains).Error
	return domains, err
}

func (s *DomainService) Get(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	var domain db.Domain
	err := s.db.WithContext(ctx).First(&domain, "id = ?", domainID).Error
	return &domain, err
}

func (s *DomainService) Create(ctx context.Context, orgID uuid.UUID, in CreateDomainInput) (*db.Domain, error) {
	// CE limit: one domain per org.
	var count int64
	if err := s.db.WithContext(ctx).Model(&db.Domain{}).
		Where("organization_id = ?", orgID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count >= 1 {
		return nil, huma.Error402PaymentRequired("multiple domains require Meshploy EE")
	}

	internal := in.InternalSubdomain
	if internal == "" {
		internal = "internal"
	}
	preview := in.PreviewSubdomain
	if preview == "" {
		preview = "preview"
	}

	token, err := generateVerifyToken()
	if err != nil {
		return nil, fmt.Errorf("generate verify token: %w", err)
	}

	domain := &db.Domain{
		OrganizationID:    orgID,
		BaseDomain:        in.BaseDomain,
		InternalSubdomain: internal,
		PreviewSubdomain:  preview,
		Verified:          false,
		VerifyToken:       token,
	}
	return domain, s.db.WithContext(ctx).Create(domain).Error
}

func (s *DomainService) Verify(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if domain.Verified {
		return domain, nil
	}

	// Look up the TXT record: _meshploy-verify.{base_domain}
	records, err := net.LookupTXT("_meshploy-verify." + domain.BaseDomain)
	if err != nil || !containsToken(records, domain.VerifyToken) {
		return nil, huma.Error422UnprocessableEntity("TXT record not found or not propagated yet")
	}

	domain.Verified = true
	err = s.db.WithContext(ctx).Model(domain).Update("verified", true).Error
	return domain, err
}

func (s *DomainService) Update(ctx context.Context, domainID uuid.UUID, in UpdateDomainInput) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if in.InternalSubdomain != "" {
		updates["internal_subdomain"] = in.InternalSubdomain
	}
	if in.PreviewSubdomain != "" {
		updates["preview_subdomain"] = in.PreviewSubdomain
	}
	if len(updates) == 0 {
		return domain, nil
	}
	err = s.db.WithContext(ctx).Model(domain).Updates(updates).Error
	return domain, err
}

// CreateSeeded creates the base domain as already verified. Used during
// gateway auto-seeding where DNS ownership is implicit (we control CoreDNS).
// Silently returns nil if a domain already exists for the org.
func (s *DomainService) CreateSeeded(ctx context.Context, orgID uuid.UUID, baseDomain string) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&db.Domain{}).
		Where("organization_id = ?", orgID).Count(&count).Error; err != nil {
		return err
	}
	if count >= 1 {
		return nil // already seeded
	}
	domain := &db.Domain{
		OrganizationID:    orgID,
		BaseDomain:        baseDomain,
		InternalSubdomain: "internal",
		PreviewSubdomain:  "preview",
		Verified:          true,
	}
	return s.db.WithContext(ctx).Create(domain).Error
}

func (s *DomainService) Delete(ctx context.Context, domainID uuid.UUID) error {
	err := s.db.WithContext(ctx).Delete(&db.Domain{}, "id = ?", domainID).Error
	if err != nil && strings.Contains(err.Error(), "violates foreign key constraint") {
		return huma.Error409Conflict("delete all routes for this domain first")
	}
	return err
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func generateVerifyToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func containsToken(records []string, token string) bool {
	for _, r := range records {
		if r == token {
			return true
		}
	}
	return false
}
