package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newServiceForBuildConfig returns a real service, and a connected git
// integration to point it at: build_configs has a foreign key to both.
func newServiceForBuildConfig(t *testing.T, name string) (*service.Services, *gorm.DB, uuid.UUID, uuid.UUID) {
	t.Helper()
	svcs, db, projectID, _ := setupDeploymentTest(t)
	svc, err := svcs.Workloads.Create(context.Background(), projectID,
		service.CreateWorkloadInput{Name: name, Image: "nginx:alpine"})
	require.NoError(t, err)

	var project meshdb.Project
	require.NoError(t, db.First(&project, "id = ?", projectID).Error)
	integration := meshdb.GitIntegration{
		OrganizationID: project.OrganizationID,
		Provider:       "gitlab",
		AuthMethod:     "pat",
		Name:           "test-gitlab",
	}
	require.NoError(t, db.Create(&integration).Error)
	return svcs, db, svc.ID, integration.ID
}

// Connecting a repository to a service is asking for its pushes to deploy, so
// that is the default now. Before this, a new service ignored its pushes until
// somebody found the switch.
func TestNewBuildConfigDeploysOnPushWhenARepositoryIsConnected(t *testing.T) {
	svcs, _, serviceID, integrationID := newServiceForBuildConfig(t, "connected")
	repo := "owner/app"

	bc, err := svcs.Workloads.UpsertBuildConfig(context.Background(), serviceID, service.UpdateBuildConfigInput{
		GitIntegrationID: &integrationID,
		GitRepo:          &repo,
	})
	require.NoError(t, err)
	require.True(t, bc.AutoDeploy, "a service built from a connected repository should deploy on push")
}

// A repository with no connection has nothing that can report a push, so
// promising to deploy on one would be a lie. Those use the deploy URL.
func TestNewBuildConfigWithoutAConnectionDoesNot(t *testing.T) {
	svcs, _, serviceID, _ := newServiceForBuildConfig(t, "public")
	repo := "https://github.com/public/app"

	bc, err := svcs.Workloads.UpsertBuildConfig(context.Background(), serviceID, service.UpdateBuildConfigInput{
		GitRepo: &repo,
	})
	require.NoError(t, err)
	require.False(t, bc.AutoDeploy, "nothing can report a push on an unconnected repository")
}

// A default is only a default: asking for it off has to stick, including on
// the call that creates the configuration - which is exactly where GORM's
// zero-value handling has bitten this codebase before.
func TestAnExplicitChoiceBeatsTheDefault(t *testing.T) {
	svcs, db, serviceID, integrationID := newServiceForBuildConfig(t, "explicit")
	repo, off := "owner/app", false

	bc, err := svcs.Workloads.UpsertBuildConfig(context.Background(), serviceID, service.UpdateBuildConfigInput{
		GitIntegrationID: &integrationID,
		GitRepo:          &repo,
		AutoDeploy:       &off,
	})
	require.NoError(t, err)
	require.False(t, bc.AutoDeploy, "turning it off at creation must stick")

	var stored meshdb.BuildConfig
	require.NoError(t, db.Where("service_id = ?", serviceID).First(&stored).Error)
	require.False(t, stored.AutoDeploy, "stored as on despite being asked for off")
}
