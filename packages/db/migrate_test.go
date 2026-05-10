package db_test

import (
	"context"
	"testing"

	"github.com/meshploy/packages/db"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMigrate(t *testing.T) {
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
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db.SetEncryptionKey("test-encryption-key-32-chars!!!!!")

	gdb, err := db.Open(dsn)
	require.NoError(t, err)

	require.NoError(t, db.Migrate(gdb))

	// Verify core tables exist by querying information_schema.
	tables := []string{
		"users", "organizations", "organization_members",
		"projects", "nodes", "services", "build_configs",
		"database_configs", "stacks", "volumes", "volume_mounts",
		"secrets", "routes", "deployments", "jobs",
	}
	for _, tbl := range tables {
		var count int64
		err := gdb.Raw(
			"SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=?",
			tbl,
		).Scan(&count).Error
		require.NoError(t, err, "querying table %s", tbl)
		require.Equal(t, int64(1), count, "table %s should exist after migration", tbl)
	}

	// Running Migrate a second time must be idempotent.
	require.NoError(t, db.Migrate(gdb), "second Migrate() call must be idempotent")
}
