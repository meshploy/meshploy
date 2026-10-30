package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A deploy loads its service when it starts and rolls out after the build, so
// what is saved in between must reach what starts: a database added during
// the first build, or a secret changed.
func TestWhatIsSavedDuringABuildReachesTheApp(t *testing.T) {
	svcs, db, projectID, _ := setupDeploymentTest(t)
	ctx := context.Background()
	svc := meshdb.Service{ProjectID: projectID, Name: "notes", Slug: "notes", Type: meshdb.ServiceTypeApplication,
		EnvVars: meshdb.EncryptedString("PORT=3000")}
	require.NoError(t, db.Create(&svc).Error)
	loaded := svc // what the deploy holds through its build

	require.NoError(t, db.Model(&meshdb.Service{}).Where("id = ?", svc.ID).
		Update("env_vars", meshdb.EncryptedString("PORT=3000\nDATABASE_URL=postgres://db/notes")).Error)

	env := service.ServiceEnvForTest(ctx, svcs, &loaded)
	assert.Equal(t, "postgres://db/notes", env["DATABASE_URL"])
}
