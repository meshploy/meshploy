package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// BrandService answers how an organisation shows itself. Community has no
// brands of its own: every organisation shows Meshploy's until an edition
// registers a source (db.RegisterBrandSource).
type BrandService struct {
	db *gorm.DB
}

// ForOrg is an organisation's brand.
func (s *BrandService) ForOrg(ctx context.Context, orgID uuid.UUID) db.Brand {
	return db.BrandOf(ctx, s.db, orgID)
}

// ForHost is the brand of the organisation whose domain host is under, for
// what is shown before anyone signs in.
func (s *BrandService) ForHost(ctx context.Context, host string) db.Brand {
	return db.BrandForHost(ctx, s.db, host)
}
