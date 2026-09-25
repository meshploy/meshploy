package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// Promote orders images by when they were built, not when they were last
// deployed. A rollback or redeploy in production makes nothing newer; a
// hotfix built there does, and only Overwrite replaces it.
func TestPromoteComparesWhenImagesWereBuilt(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, web, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())
	first := func(dest any, conds ...any) error { return gdb.First(dest, conds...).Error }
	at := func(h int) time.Time { return time.Now().Add(time.Duration(h-100) * time.Hour) }

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "web", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)
	var webS1 meshdb.Service
	require.NoError(t, first(&webS1, "project_id = ? AND name = ?", s1, "web"))

	// v1 built in staging at 1, promoted to production at 2.
	t1, t2 := at(1), at(2)
	v1 := meshdb.Deployment{Base: meshdb.Base{CreatedAt: t1}, ServiceID: webS1.ID, Status: meshdb.DeploymentSuccess, Image: "web:v1", DeployedAt: &t1, Source: meshdb.DeploySourceBuild}
	require.NoError(t, gdb.Create(&v1).Error)
	p1 := meshdb.Deployment{Base: meshdb.Base{CreatedAt: t2}, ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Image: "web:v1", DeployedAt: &t2, Source: meshdb.DeploySourcePromotion, FromDeploymentID: &v1.ID}
	require.NoError(t, gdb.Create(&p1).Error)
	// v2 built in staging at 3; production is rolled back to v1 at 4.
	t3, t4 := at(3), at(4)
	require.NoError(t, gdb.Create(&meshdb.Deployment{Base: meshdb.Base{CreatedAt: t3}, ServiceID: webS1.ID, Status: meshdb.DeploymentSuccess, Image: "web:v2", DeployedAt: &t3, Source: meshdb.DeploySourceBuild}).Error)
	require.NoError(t, gdb.Create(&meshdb.Deployment{Base: meshdb.Base{CreatedAt: t4}, ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Image: "web:v1", DeployedAt: &t4, Source: meshdb.DeploySourceRollback, FromDeploymentID: &p1.ID}).Error)

	board, err := svcs.Promotions.Board(ctx, prod)
	require.NoError(t, err)
	for _, c := range board.Groups[0].Cells {
		if c.LevelID == prod {
			assert.Equal(t, meshdb.DeploySourcePromotion, c.Arrival, "a rollback is looked through to how the image arrived")
			assert.WithinDuration(t, t1, *c.ImageBuiltAt, time.Second, "and it was built when staging built it")
		}
	}

	result, err := svcs.Promotions.Promote(ctx, s1, group.ID)
	require.NoError(t, err, "production's rollback today does not make v1 newer than v2")
	require.Len(t, result.Promoted, 1)
	require.Eventually(t, func() bool {
		var d meshdb.Deployment
		return first(&d, "id = ?", result.Promoted[0].Deployment.ID) == nil && d.Status == meshdb.DeploymentSuccess
	}, 10*time.Second, 50*time.Millisecond)

	// A hotfix is built in production, after staging's newest build.
	now := time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Image: "web:hotfix", DeployedAt: &now, Source: meshdb.DeploySourceBuild, SourceBranch: "main", SourceCommit: "9f02e11"}).Error)
	board, err = svcs.Promotions.Board(ctx, prod)
	require.NoError(t, err)
	for _, c := range board.Groups[0].Cells {
		if c.LevelID == prod {
			assert.Equal(t, meshdb.DeploySourceBuild, c.Arrival, "production runs something built there")
		}
	}
	_, err = svcs.Promotions.Promote(ctx, s1, group.ID)
	assert.ErrorIs(t, err, service.ErrNothingToPromote, "staging's v2 is older than the hotfix")

	result, err = svcs.Promotions.Overwrite(ctx, s1, group.ID)
	require.NoError(t, err)
	require.Len(t, result.Promoted, 1)
	assert.Equal(t, "web:v2", result.Promoted[0].Image, "overwrite replaces the hotfix with staging's image")
}

// Redeploy runs the current image again and keeps where it came from.
func TestRedeployKeepsTheImagesOrigin(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, _, _, _, web, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())
	_, err := svcs.Deployments.Redeploy(ctx, web.ID)
	assert.ErrorIs(t, err, service.ErrNothingToRedeploy)

	built := time.Now().Add(-time.Hour)
	orig := meshdb.Deployment{Base: meshdb.Base{CreatedAt: built}, ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Image: "web:v1", DeployedAt: &built,
		Source: meshdb.DeploySourceBuild, SourceBranch: "main", SourceCommit: "abc1234"}
	require.NoError(t, gdb.Create(&orig).Error)
	dep, err := svcs.Deployments.Redeploy(ctx, web.ID)
	require.NoError(t, err)
	assert.Equal(t, "web:v1", dep.Image)
	assert.Equal(t, meshdb.DeploySourceRedeploy, dep.Source)
	assert.Equal(t, "main", dep.SourceBranch)
	origin := svcs.Deployments.ImageOrigin(ctx, *dep)
	assert.Equal(t, meshdb.DeploySourceBuild, origin.Arrival)
	assert.WithinDuration(t, built, origin.BuiltAt, time.Second)
}
