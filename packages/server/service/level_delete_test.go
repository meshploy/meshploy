package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Deleting a level removes it from the cluster and from every group's path.
// A group that built there builds at the next level up instead, keeping its
// auto-deploy; a group with nothing left between entry and production is
// dissolved, and production builds on push again.
func TestDeletingALevelHandsItsGroupsToTheNextLevel(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)
	first := func(dest any, conds ...any) error { return gdb.First(dest, conds...).Error }

	_, err := svcs.Promotions.DeleteLevel(ctx, prod)
	assert.ErrorIs(t, err, service.ErrDeleteProduction)

	_, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "app", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s2, s1, prod}})
	require.NoError(t, err)

	var level2 meshdb.Project
	require.NoError(t, first(&level2, "id = ?", s2))
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: level2.Slug}})
	service.UseWorkloadK8sForTest(svcs, client)

	out, err := svcs.Promotions.DeleteLevel(ctx, s2)
	require.NoError(t, err)
	assert.Equal(t, 1, out.ServicesRemoved)
	assert.Equal(t, []string{"app"}, out.GroupsShortened)
	assert.Empty(t, out.GroupsDeleted)

	_, err = client.CoreV1().Namespaces().Get(ctx, level2.Slug, metav1.GetOptions{})
	assert.Error(t, err, "the level's namespace is gone, with whatever was left in it")
	var gone meshdb.Project
	assert.Error(t, first(&gone, "id = ?", s2))
	var count int64
	gdb.Model(&meshdb.Service{}).Where("project_id = ?", s2).Count(&count)
	assert.Zero(t, count)

	groups, err := svcs.Promotions.ListGroups(ctx, prod)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	var up meshdb.Service
	require.NoError(t, first(&up, "project_id = ? AND name = ?", s1, "web"), "staging1 is the entry now, and has the service")
	var upBuild meshdb.BuildConfig
	require.NoError(t, first(&upBuild, "service_id = ?", up.ID))
	assert.True(t, upBuild.AutoDeploy, "it builds on push, as staging2 did")

	out, err = svcs.Promotions.DeleteLevel(ctx, s1)
	require.NoError(t, err)
	assert.Equal(t, []string{"app"}, out.GroupsDeleted)
	groups, err = svcs.Promotions.ListGroups(ctx, prod)
	require.NoError(t, err)
	assert.Empty(t, groups)
	var prodBuild meshdb.BuildConfig
	require.NoError(t, first(&prodBuild, "service_id = ?", web.ID))
	assert.True(t, prodBuild.AutoDeploy, "with no level below, production builds on push again")

	levels, err := svcs.Projects.Levels(ctx, prod)
	require.NoError(t, err)
	assert.Len(t, levels, 1)
}

// A level's order closes up when one in the middle goes.
func TestDeletingAMiddleLevelClosesTheGap(t *testing.T) {
	ctx := context.Background()
	svcs, _, prod, s1, s2, _, _ := newChain(t)
	_, err := svcs.Promotions.DeleteLevel(ctx, s1)
	require.NoError(t, err)
	levels, err := svcs.Projects.Levels(ctx, prod)
	require.NoError(t, err)
	require.Len(t, levels, 2)
	assert.Equal(t, s2, levels[1].ProjectID)
	assert.Equal(t, 1, levels[1].Level)
}
