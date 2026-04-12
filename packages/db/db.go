package db

//go:generate go run ./cmd/gen-dbml

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Connection
// ---------------------------------------------------------------------------

// Open connects to Postgres using the provided DSN.
func Open(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

// FromEnv connects using DATABASE_URL.
// Expected format: postgres://user:password@host:5432/dbname?sslmode=disable
func FromEnv() (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}
	return Open(dsn)
}

// ---------------------------------------------------------------------------
// Extensible Migration Registry (Open-Core pattern)
//
// CE code calls Migrate() which runs all CE AutoMigrate + the eeHooks slice.
// The EE module registers its own migrations via RegisterMigration() from an
// init() function. Because the CE binary never imports the EE module, eeHooks
// stays empty in CE builds — the CE codebase remains completely unaware of EE.
// ---------------------------------------------------------------------------

var eeHooks []func(*gorm.DB) error

// RegisterMigration appends an EE migration function to the registry.
// Intended to be called from the EE module's init().
func RegisterMigration(fn func(*gorm.DB) error) {
	eeHooks = append(eeHooks, fn)
}

// ---------------------------------------------------------------------------
// Migrate
// ---------------------------------------------------------------------------

// Migrate runs AutoMigrate for all CE models, applies supplementary DB-level
// constraints, and then calls any registered EE migration hooks.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		// Identity & Access
		&User{},
		&Organization{},
		&OrganizationMember{},
		&ResourcePermission{},

		// Projects & Infrastructure
		&Project{},
		&Node{},
		&NodeRegistrationToken{},

		// Domains
		&Domain{},

		// Workloads
		&Service{},
		&BuildConfig{},
		&DatabaseConfig{},

		// Traffic (Domain must migrate before Route for FK constraint)
		&Route{},

		// Deployment History
		&Deployment{},

		// Integrations
		&StorageIntegration{},
		&RegistryIntegration{},
		&GitIntegration{},

		// Operations
		&BackupConfig{},
		&NotificationChannel{},

		// Templates
		&Template{},
	); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}

	if err := applyConstraints(db); err != nil {
		return fmt.Errorf("constraints: %w", err)
	}

	// Run EE migration hooks (no-op in CE builds)
	for _, fn := range eeHooks {
		if err := fn(db); err != nil {
			return fmt.Errorf("ee migration: %w", err)
		}
	}

	return nil
}

// applyConstraints creates DB-level constraints that GORM's AutoMigrate
// cannot express via struct tags alone.
func applyConstraints(db *gorm.DB) error {
	stmts := []string{
		// Exactly one owner per organization
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_one_owner_per_org
		 ON organization_members (organization_id)
		 WHERE role = 'owner' AND deleted_at IS NULL`,
	}

	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
