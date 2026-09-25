package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A level uses what it does not have from the nearest level above. A service
// reaches another through the group the other publishes, whose values name
// the publisher's namespace, so borrowing is resolving that group to the
// nearest copy at or above the consumer - and switching when a nearer one
// appears.
func TestALevelBorrowsFromTheNearestLevelAbove(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)

	// api in production publishes its address, and web uses it.
	api := meshdb.Service{ProjectID: prod, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication, Image: "registry/api:v1"}
	require.NoError(t, gdb.Create(&api).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: api.ID, Name: "http", Port: 8080, IsHTTP: true, IsPrimary: true}).Error)
	require.NoError(t, svcs.VariableGroups.UpsertSystemGroup(ctx, &api, "coreline"))
	var apiGroup meshdb.VariableGroup
	require.NoError(t, gdb.First(&apiGroup, "service_id = ?", api.ID).Error)
	require.NoError(t, svcs.VariableGroups.Attach(ctx, web.ID, apiGroup.ID))

	// web moves through staging2 and staging1; api is in no group, so only
	// production has it.
	_, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "web", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s2, s1, prod}})
	require.NoError(t, err)
	var webS2 meshdb.Service
	require.NoError(t, gdb.First(&webS2, "project_id = ? AND name = ?", s2, "web").Error)

	env, err := svcs.VariableGroups.CollectEnvVars(ctx, webS2.ID)
	require.NoError(t, err)
	assert.Equal(t, "api.coreline.svc.cluster.local", env["API_HOST"],
		"staging2 has no api, so it uses production's: the copy kept web's attachment")

	// staging1 gets its own api, deployed (so it publishes).
	apiS1 := meshdb.Service{ProjectID: s1, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication, LineageID: &api.ID}
	require.NoError(t, gdb.Create(&apiS1).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: apiS1.ID, Name: "http", Port: 8080, IsHTTP: true, IsPrimary: true}).Error)
	require.NoError(t, svcs.VariableGroups.UpsertSystemGroup(ctx, &apiS1, "coreline-staging1"))

	env, err = svcs.VariableGroups.CollectEnvVars(ctx, webS2.ID)
	require.NoError(t, err)
	assert.Equal(t, "api.coreline-staging1.svc.cluster.local", env["API_HOST"],
		"the nearest level above staging2 with an api is now staging1")

	env, err = svcs.VariableGroups.CollectEnvVars(ctx, web.ID)
	require.NoError(t, err)
	assert.Equal(t, "api.coreline.svc.cluster.local", env["API_HOST"],
		"production never uses a level below it")

	// A copy that has never been deployed publishes nothing and is passed over.
	apiS2 := meshdb.Service{ProjectID: s2, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication, LineageID: &api.ID}
	require.NoError(t, gdb.Create(&apiS2).Error)
	env, err = svcs.VariableGroups.CollectEnvVars(ctx, webS2.ID)
	require.NoError(t, err)
	assert.Equal(t, "api.coreline-staging1.svc.cluster.local", env["API_HOST"])
}
