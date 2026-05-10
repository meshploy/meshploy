package service_test

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	pgDSN string
	dbSeq atomic.Int64
)

// TestMain starts a single Postgres container for the entire service test suite.
func TestMain(m *testing.M) {
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("meshploy_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.WithSQLDriver("pgx"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: %v\n", err)
		os.Exit(1)
	}
	defer ctr.Terminate(ctx) //nolint:errcheck

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		os.Exit(1)
	}
	pgDSN = dsn

	meshdb.SetEncryptionKey("test-encryption-key-32-chars!!!!!")

	os.Exit(m.Run())
}

// newTestDB creates an isolated database per test — each gets its own schema.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	n := dbSeq.Add(1)
	dbName := fmt.Sprintf("test_%d", n)

	root, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, root.Exec("CREATE DATABASE "+dbName).Error)
	sqlDB, _ := root.DB()
	sqlDB.Close()

	// Build DSN pointing at the new isolated DB.
	isolatedDSN := replaceDSNDatabase(pgDSN, dbName)

	db, err := gorm.Open(postgres.Open(isolatedDSN), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, meshdb.Migrate(db))

	t.Cleanup(func() {
		sql, _ := db.DB()
		if sql != nil {
			sql.Close()
		}
	})
	return db
}

// newServices builds a Services aggregate with no K8s client and no config.
func newServices(db *gorm.DB) *service.Services {
	return service.New(db)
}

// parseUUID is a test helper that parses a UUID string and fails the test on error.
func parseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}

// replaceDSNDatabase swaps the database name in a postgres DSN.
func replaceDSNDatabase(dsn, newDB string) string {
	// DSN format: postgres://user:pass@host:port/dbname?params
	i := len(dsn) - 1
	for i >= 0 && dsn[i] != '/' {
		i--
	}
	base := dsn[:i+1]
	// Strip any existing query params from the old dbname part.
	return base + newDB + "?sslmode=disable"
}
