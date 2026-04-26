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

		// Secrets
		&Secret{},
		&ServiceSecret{},

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
// cannot express via struct tags alone, and ensures all FK constraints carry
// the correct ON DELETE behavior (idempotent — safe to run on every startup).
func applyConstraints(db *gorm.DB) error {
	stmts := []string{
		// Exactly one owner per organization
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_one_owner_per_org
		 ON organization_members (organization_id)
		 WHERE role = 'owner'`,
		// Secret names must be unique within a project
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_secrets_project_name
		 ON secrets (project_id, name)`,
		// No duplicate env keys per service
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_service_secrets_env_key
		 ON service_secrets (service_id, env_key)`,
	}

	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}

	type fkSpec struct {
		table, column, refTable, onDelete string
	}
	fks := []fkSpec{
		// Organization → children CASCADE
		{"organization_members", "organization_id", "organizations", "CASCADE"},
		{"projects", "organization_id", "organizations", "CASCADE"},
		{"nodes", "organization_id", "organizations", "CASCADE"},
		{"node_registration_tokens", "organization_id", "organizations", "CASCADE"},
		{"storage_integrations", "organization_id", "organizations", "CASCADE"},
		{"registry_integrations", "organization_id", "organizations", "CASCADE"},
		{"git_integrations", "organization_id", "organizations", "CASCADE"},
		{"notification_channels", "organization_id", "organizations", "CASCADE"},
		{"domains", "organization_id", "organizations", "CASCADE"},
		{"resource_permissions", "organization_id", "organizations", "CASCADE"},
		// Project → children CASCADE
		{"services", "project_id", "projects", "CASCADE"},
		{"routes", "project_id", "projects", "CASCADE"},
		{"secrets", "project_id", "projects", "CASCADE"},
		// Service → children CASCADE
		{"service_secrets", "service_id", "services", "CASCADE"},
		{"service_secrets", "secret_id", "secrets", "CASCADE"},
		{"build_configs", "service_id", "services", "CASCADE"},
		{"database_configs", "service_id", "services", "CASCADE"},
		{"deployments", "service_id", "services", "CASCADE"},
		{"backup_configs", "service_id", "services", "CASCADE"},
		// Sibling connections SET NULL
		{"services", "node_id", "nodes", "SET NULL"},
		{"routes", "service_id", "services", "SET NULL"},
		{"build_configs", "git_integration_id", "git_integrations", "SET NULL"},
		{"build_configs", "registry_integration_id", "registry_integrations", "SET NULL"},
		{"backup_configs", "storage_integration_id", "storage_integrations", "CASCADE"},
	}

	for _, fk := range fks {
		if err := ensureFK(db, fk.table, fk.column, fk.refTable, fk.onDelete); err != nil {
			return fmt.Errorf("ensureFK %s.%s: %w", fk.table, fk.column, err)
		}
	}

	return nil
}

// ensureFK drops any existing FK constraint on table.column and recreates it
// with the specified ON DELETE behavior. Idempotent across restarts.
func ensureFK(db *gorm.DB, table, column, refTable, onDelete string) error {
	var constraints []string
	db.Raw(`
		SELECT tc.constraint_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = 'public'
			AND tc.table_name = ?
			AND kcu.column_name = ?
	`, table, column).Scan(&constraints)

	for _, c := range constraints {
		if err := db.Exec(fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS "%s"`, table, c)).Error; err != nil {
			return err
		}
	}

	name := fmt.Sprintf("fk_%s_%s", table, column)
	return db.Exec(fmt.Sprintf(
		`ALTER TABLE %s ADD CONSTRAINT "%s" FOREIGN KEY (%s) REFERENCES %s(id) ON DELETE %s`,
		table, name, column, refTable, onDelete,
	)).Error
}
