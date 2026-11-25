package service_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
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

// TestMain starts a Postgres container for the test suite, or falls back to
// DATABASE_URL when no container runtime is available (e.g. local dev with
// Postgres already running).
func TestMain(m *testing.M) {
	ctx := context.Background()

	meshdb.SetEncryptionKey("test-encryption-key-32-chars!!!!!")
	// A fake cluster's pods never restart; waiting for them to stay up only
	// slows every deploy test. The wait itself is tested in k8s.
	appk8s.RolloutSettle = 0

	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		// Use the existing local Postgres - create isolated DBs per test.
		pgDSN = dsn
		code := m.Run()
		dropTestDBs()
		os.Exit(code)
		return
	}

	ctr, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("meshploy_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.WithSQLDriver("pgx"),
		// Its data in memory: no volume is made, so a run that is killed or
		// times out leaves nothing behind, and the tests run faster.
		testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql/data": "rw"}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testcontainers: %v — set DATABASE_URL to use a local Postgres instead\n", err)
		os.Exit(1)
	}
	// Terminated explicitly rather than deferred: os.Exit skips deferred calls,
	// so a defer here left one Postgres container running after every run.
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		_ = ctr.Terminate(ctx)
		os.Exit(1)
	}
	pgDSN = dsn

	code := m.Run()
	_ = ctr.Terminate(ctx)
	os.Exit(code)
}

// newTestDB creates an isolated database per test - each gets its own schema.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	n := dbSeq.Add(1)
	dbName := fmt.Sprintf("meshploy_test_%d", n)

	root, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	root.Exec("DROP DATABASE IF EXISTS " + dbName)
	require.NoError(t, root.Exec("CREATE DATABASE "+dbName).Error)
	sqlDB, _ := root.DB()
	sqlDB.Close()

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

// dropTestDBs removes the run's databases from a shared Postgres
// (DATABASE_URL), so it does not keep hundreds of them between runs. Once at
// the end rather than per test: dropping is slow enough to add up. A run that
// is killed leaves them, and the next run reuses the same names. FORCE ends
// the connections background loops still hold.
func dropTestDBs() {
	root, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return
	}
	defer func() {
		if r, _ := root.DB(); r != nil {
			r.Close()
		}
	}()
	// Several at once: one after another, hundreds of drops took minutes,
	// counted against the run's timeout.
	start := time.Now()
	n := dbSeq.Load()
	next := make(chan int64)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS meshploy_test_%d WITH (FORCE)", i))
			}
		}()
	}
	for i := int64(1); i <= n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	if testing.Verbose() {
		fmt.Fprintf(os.Stderr, "dropped %d test databases in %s\n", n, time.Since(start).Round(time.Millisecond))
	}
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
	return base + newDB + "?sslmode=disable"
}
