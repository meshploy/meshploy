package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type DomainService struct {
	db *gorm.DB
}

func (s *DomainService) List(ctx context.Context, orgID uuid.UUID) ([]db.Domain, error) {
	domains := make([]db.Domain, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&domains).Error
	return domains, err
}

func (s *DomainService) Get(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	var domain db.Domain
	err := s.db.WithContext(ctx).First(&domain, "id = ?", domainID).Error
	return &domain, err
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
