package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// names reads a chain of levels as its names, production first.
func names(levels []service.EnvironmentLevel) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = l.Name
	}
	return out
}

// Levels are placed where they are asked for, and the chain stays in order:
// production at 0, each level below one more, with no gaps.
func TestLevelsArePlacedAboveOrBelow(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, _, projectIDs := setupPermissionFixture(t)
	prod := parseUUID(t, projectIDs)

	staging, err := svcs.Projects.CreateLevel(ctx, prod, "staging", prod, false)
	require.NoError(t, err)
	assert.Equal(t, "test-project-staging", staging.Slug, "a level is its own namespace")
	assert.Equal(t, 1, staging.EnvLevel)
	assert.Equal(t, "test-project", staging.Name, "a level carries its project's name")

	_, err = svcs.Projects.CreateLevel(ctx, prod, "dev", staging.ID, false)
	require.NoError(t, err)
	// Above staging: takes its place, and pushes staging and dev down.
	_, err = svcs.Projects.CreateLevel(ctx, staging.ID, "qa", staging.ID, true)
	require.NoError(t, err)

	levels, err := svcs.Projects.Levels(ctx, staging.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"production", "qa", "staging", "dev"}, names(levels))
	for i, l := range levels {
		assert.Equal(t, i, l.Level, "levels are numbered without gaps")
	}
	assert.True(t, levels[0].Production)

	t.Run("refused", func(t *testing.T) {
		_, err := svcs.Projects.CreateLevel(ctx, prod, "staging", prod, false)
		assert.ErrorIs(t, err, service.ErrLevelTaken)
		_, err = svcs.Projects.CreateLevel(ctx, prod, "hotfix", prod, true)
		assert.ErrorIs(t, err, service.ErrAboveProduction)
		_, err = svcs.Projects.CreateLevel(ctx, prod, "production", prod, false)
		assert.ErrorIs(t, err, service.ErrLevelProduction)
		_, err = svcs.Projects.CreateLevel(ctx, prod, "Staging_2", prod, false)
		assert.ErrorIs(t, err, service.ErrLevelName)
	})

	t.Run("removing one closes the gap", func(t *testing.T) {
		var qa meshdb.Project
		for _, l := range levels {
			if l.Name == "qa" {
				qa.ID = l.ProjectID
			}
		}
		require.NoError(t, svcs.Projects.Delete(ctx, qa.ID))
		levels, err := svcs.Projects.Levels(ctx, prod)
		require.NoError(t, err)
		assert.Equal(t, []string{"production", "staging", "dev"}, names(levels))
		for i, l := range levels {
			assert.Equal(t, i, l.Level)
		}
	})
}

// A project and its levels are one project to the operator: the list shows
// the project, deleting it cannot take a staging environment with it, and
// renaming it renames every level.
func TestLevelsStayWithTheirProject(t *testing.T) {
	ctx := context.Background()
	svcs, orgIDs, _, _, projectIDs := setupPermissionFixture(t)
	org, prod := parseUUID(t, orgIDs), parseUUID(t, projectIDs)

	staging, err := svcs.Projects.CreateLevel(ctx, prod, "staging", prod, false)
	require.NoError(t, err)

	listed, err := svcs.Projects.ListWithCounts(ctx, org, service.ProjectListOptions{})
	require.NoError(t, err)
	for _, p := range listed {
		assert.NotEqual(t, staging.ID, p.ID, "a level is not listed as a project")
	}

	assert.ErrorIs(t, svcs.Projects.Delete(ctx, prod), service.ErrProjectHasLevels)

	_, err = svcs.Projects.Update(ctx, prod, "CoreLine")
	require.NoError(t, err)
	got, err := svcs.Projects.Get(ctx, staging.ID)
	require.NoError(t, err)
	assert.Equal(t, "CoreLine", got.Name)

	require.NoError(t, svcs.Projects.Delete(ctx, staging.ID))
	require.NoError(t, svcs.Projects.Delete(ctx, prod), "with its levels gone, the project can go")
}

// A grant on a project covers its levels. A grant on something inside a level
// makes the project visible, because only projects are listed. A grant on one
// production service does not open the levels below.
func TestAccessFollowsTheProjectIntoItsLevels(t *testing.T) {
	ctx := context.Background()
	svcs, orgIDs, _, memberIDs, projectIDs := setupPermissionFixture(t)
	org, member, prod := parseUUID(t, orgIDs), parseUUID(t, memberIDs), parseUUID(t, projectIDs)

	staging, err := svcs.Projects.CreateLevel(ctx, prod, "staging", prod, false)
	require.NoError(t, err)

	t.Run("no grant, no level", func(t *testing.T) {
		err := svcs.Permissions.CheckAccess(ctx, org, member, staging.ID, meshdb.ResourceProject, meshdb.ActionView, &staging.ID)
		assert.Error(t, err)
		visible, _, err := svcs.Permissions.VisibleProjectIDs(ctx, org, member)
		require.NoError(t, err)
		assert.False(t, visible[staging.ID])
	})

	t.Run("a grant on the project reaches the level", func(t *testing.T) {
		require.NoError(t, svcs.Permissions.Grant(ctx, org, member, prod, meshdb.ResourceProject, meshdb.ActionView))
		err := svcs.Permissions.CheckAccess(ctx, org, member, staging.ID, meshdb.ResourceProject, meshdb.ActionView, &staging.ID)
		assert.NoError(t, err)
		visible, _, err := svcs.Permissions.VisibleProjectIDs(ctx, org, member)
		require.NoError(t, err)
		assert.True(t, visible[staging.ID])
		assert.True(t, visible[prod])
	})
}

func TestAGrantInsideALevelShowsItsProjectButNotOtherLevels(t *testing.T) {
	ctx := context.Background()
	svcs, orgIDs, _, memberIDs, projectIDs := setupPermissionFixture(t)
	org, member, prod := parseUUID(t, orgIDs), parseUUID(t, memberIDs), parseUUID(t, projectIDs)

	staging, err := svcs.Projects.CreateLevel(ctx, prod, "staging", prod, false)
	require.NoError(t, err)
	dev, err := svcs.Projects.CreateLevel(ctx, prod, "dev", staging.ID, false)
	require.NoError(t, err)

	// A grant on the staging level itself, as a project.
	require.NoError(t, svcs.Permissions.Grant(ctx, org, member, staging.ID, meshdb.ResourceProject, meshdb.ActionView))
	visible, _, err := svcs.Permissions.VisibleProjectIDs(ctx, org, member)
	require.NoError(t, err)
	assert.True(t, visible[prod], "the project appears in the list, so the level can be reached")
	assert.True(t, visible[staging.ID])
	assert.False(t, visible[dev.ID], "a grant on one level opens no other")
}
