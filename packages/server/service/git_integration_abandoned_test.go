package service_test

import (
	"context"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// A GitHub App setup GitHub never completed leaves a row with no App behind;
// the reaper removes it once it is old, and leaves everything that has an App,
// or is still young enough to be finishing.
func TestAbandonedGitHubAppSetupsAreRemoved(t *testing.T) {
	svcs, db, projectID, _ := setupDeploymentTest(t)
	var project meshdb.Project
	require.NoError(t, db.First(&project, "id = ?", projectID).Error)
	org := project.OrganizationID

	old := time.Now().Add(-3 * time.Hour)
	mk := func(name, appID string, at time.Time, provider, method string) meshdb.GitIntegration {
		g := meshdb.GitIntegration{OrganizationID: org, Provider: provider, AuthMethod: method, Name: name, GHAppID: appID}
		require.NoError(t, db.Create(&g).Error)
		require.NoError(t, db.Model(&g).Update("created_at", at).Error)
		return g
	}
	abandoned := mk("abandoned", "", old, "github", "app")
	created := mk("created", "123", old, "github", "app")
	young := mk("young", "", time.Now(), "github", "app")
	gitlab := mk("gitlab", "", old, "gitlab", "pat")

	service.PruneAbandonedAppsForTest(svcs, time.Now().Add(-time.Hour))

	var left []meshdb.GitIntegration
	require.NoError(t, db.WithContext(context.Background()).Where("organization_id = ?", org).Find(&left).Error)
	ids := map[string]bool{}
	for _, g := range left {
		ids[g.ID.String()] = true
	}
	require.False(t, ids[abandoned.ID.String()], "a setup GitHub never completed should be removed")
	require.True(t, ids[created.ID.String()], "an App GitHub created is kept, installed or not")
	require.True(t, ids[young.ID.String()], "a setup still within its hour may be finishing")
	require.True(t, ids[gitlab.ID.String()], "other providers have no App and are not this reaper's")
}
