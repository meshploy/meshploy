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
		"routes", "deployments", "jobs",
		// Tables added by the EE foundation.
		"agent_tokens", "installed_licenses",
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

	// A database port stored as HTTP and public by the old `default:true` tags,
	// beside an application port that is public on purpose.
	org := db.Organization{Name: "o", Slug: "o"}
	require.NoError(t, gdb.Create(&org).Error)
	project := db.Project{OrganizationID: org.ID, Name: "p", Slug: "p"}
	require.NoError(t, gdb.Create(&project).Error)
	database := db.Service{ProjectID: project.ID, Name: "pg", Type: db.ServiceTypeDatabase, Image: "postgres:17"}
	require.NoError(t, gdb.Create(&database).Error)
	app := db.Service{ProjectID: project.ID, Name: "api", Type: db.ServiceTypeApplication, Image: "nginx:alpine"}
	require.NoError(t, gdb.Create(&app).Error)
	dbPort := db.ServicePort{ServiceID: database.ID, Name: "db", Port: 5432, IsHTTP: true, IsPrimary: true, IsPublic: true}
	require.NoError(t, gdb.Create(&dbPort).Error)
	appPort := db.ServicePort{ServiceID: app.ID, Name: "http", Port: 3000, IsHTTP: true, IsPrimary: true, IsPublic: true}
	require.NoError(t, gdb.Create(&appPort).Error)

	// Running Migrate a second time must be idempotent.
	require.NoError(t, db.Migrate(gdb), "second Migrate() call must be idempotent")

	// ...and it repairs the database's port without touching the application's.
	require.NoError(t, gdb.First(&dbPort, "id = ?", dbPort.ID).Error)
	require.False(t, dbPort.IsHTTP, "a database port is not HTTP")
	require.False(t, dbPort.IsPublic, "a database port is internal")
	require.NoError(t, gdb.First(&appPort, "id = ?", appPort.ID).Error)
	require.True(t, appPort.IsHTTP && appPort.IsPublic, "an application's port is left as it was")
}
