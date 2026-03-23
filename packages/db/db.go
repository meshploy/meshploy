package db

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open connects to Postgres using the provided DSN.
func Open(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

// FromEnv connects using the DATABASE_URL environment variable.
// Expected format: postgres://user:password@host:5432/dbname?sslmode=disable
func FromEnv() (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is not set")
	}
	return Open(dsn)
}

// Migrate runs AutoMigrate for all models and creates the partial unique index
// that enforces exactly one owner per organization.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&User{},
		&Organization{},
		&OrganizationMember{},
		&Project{},
		&Node{},
		&Service{},
		&Route{},
		&ResourcePermission{},
		&Deployment{},
	); err != nil {
		return err
	}

	// Enforce single owner per org at the DB level (Postgres partial unique index).
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_one_owner_per_org
		ON organization_members (organization_id)
		WHERE role = 'owner'
	`).Error
}
