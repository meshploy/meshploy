package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// A database's generated group carries its connection: an app attaches the
// group and sets DATABASE_URL=${PRIMARY_DB_URL}. The URL used to be http://
// with no credentials, which no Postgres client can use.
func TestDatabaseGroupCarriesItsConnection(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "vars", Slug: "vars"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "vars", "vars")
	require.NoError(t, err)

	database, err := svcs.Workloads.Create(ctx, project.ID, service.CreateWorkloadInput{
		Name: "Primary DB", Type: meshdb.ServiceTypeDatabase, Engine: meshdb.DatabasePostgres,
		DBName: "app", DBUser: "app", DBPassword: "p@ss:word",
	})
	require.NoError(t, err)

	var group meshdb.VariableGroup
	require.NoError(t, gdb.Preload("Items").Where("service_id = ?", database.ID).First(&group).Error)
	items := map[string]meshdb.VariableGroupItem{}
	for _, it := range group.Items {
		items[it.Key] = it
	}

	host := "primary-db.vars.svc.cluster.local"
	require.Equal(t, host+":5432", string(items["PRIMARY_DB_ADDR"].Value))
	require.Equal(t, "app", string(items["PRIMARY_DB_USER"].Value))
	require.Equal(t, "app", string(items["PRIMARY_DB_DB"].Value))
	require.True(t, items["PRIMARY_DB_PASSWORD"].IsSecret)
	url := items["PRIMARY_DB_URL"]
	require.Equal(t, "postgresql://app:p%40ss%3Aword@"+host+":5432/app", string(url.Value))
	require.True(t, url.IsSecret, "the URL carries the password")
}

// A group's list never carries a secret's value - it is fetched by every page
// that mentions the group - so the value has to be readable one at a time, and
// it has to survive the round trip through encryption intact.
func TestASecretItemKeepsItsValueExactly(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "secrets", Slug: "secrets"}
	require.NoError(t, gdb.Create(&org).Error)
	project, err := svcs.Projects.Create(ctx, org.ID, "secrets", "secrets")
	require.NoError(t, err)

	g, err := svcs.VariableGroups.Create(ctx, service.CreateGroupInput{ProjectID: project.ID, Name: "creds"})
	require.NoError(t, err)

	// A password with the characters that break URLs is exactly the kind that
	// needs reading back.
	const password = "Vigyan12#@/x"
	_, err = svcs.VariableGroups.UpsertItem(ctx, g.ID, service.UpsertItemInput{
		Key: "DB_PASSWORD", Value: password, IsSecret: true,
	})
	require.NoError(t, err)

	got, err := svcs.VariableGroups.Get(ctx, g.ID, project.ID)
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.True(t, got.Items[0].IsSecret)
	require.Equal(t, password, string(got.Items[0].Value))
}
