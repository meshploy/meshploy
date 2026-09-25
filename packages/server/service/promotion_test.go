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
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes/fake"
)

// newChain is a project with production, staging1 and staging2 (staging2 the
// lowest), one application in production with a build config, and a database.
func newChain(t *testing.T) (*service.Services, *gorm.DB, uuid.UUID, uuid.UUID, uuid.UUID, meshdb.Service, meshdb.Service) {
	t.Helper()
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, orgID, "CoreLine", "coreline")
	require.NoError(t, err)
	s1, err := svcs.Projects.CreateLevel(ctx, project.ID, "staging1", project.ID, false)
	require.NoError(t, err)
	s2, err := svcs.Projects.CreateLevel(ctx, project.ID, "staging2", s1.ID, false)
	require.NoError(t, err)

	web := meshdb.Service{ProjectID: project.ID, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication, Image: "registry/web:v1", EnvVars: "MODE=prod"}
	require.NoError(t, gdb.Create(&web).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: web.ID, Name: "http", Port: 3000, IsHTTP: true, IsPrimary: true}).Error)
	require.NoError(t, gdb.Create(&meshdb.BuildConfig{ServiceID: web.ID, Builder: "dockerfile", GitRepo: "acme/web", Branch: "main", AutoDeploy: true, DeployToken: "secret-token"}).Error)
	pg := meshdb.Service{ProjectID: project.ID, Name: "pg", Slug: "pg", Type: meshdb.ServiceTypeDatabase, Image: "postgres:16"}
	require.NoError(t, gdb.Create(&pg).Error)

	return svcs, gdb, project.ID, s1.ID, s2.ID, web, pg
}

// A group enters at the lowest level of its path, and only there does it
// build. Its services are copied into that level from production, carrying
// how they are built and run but nothing that recorded the original's history.
func TestAGroupEntersAtItsLowestLevel(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, pg := newChain(t)
	first := func(dest any, conds ...any) error { return gdb.First(dest, conds...).Error }

	_, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "pg-db", ServiceIDs: []uuid.UUID{pg.ID}, Path: []uuid.UUID{s1, prod}})
	assert.ErrorIs(t, err, service.ErrGroupDatabase, "databases are not promoted")

	_, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "down", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{prod, s1}})
	assert.ErrorIs(t, err, service.ErrGroupPath, "a path climbs toward production")
	_, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "short", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s2, s1}})
	assert.ErrorIs(t, err, service.ErrGroupPath, "a path ends at production")

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "pg1", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s2, s1, prod}})
	require.NoError(t, err)

	var entry meshdb.Service
	require.NoError(t, first(&entry, "project_id = ? AND name = ?", s2, "web"))
	require.NotNil(t, entry.LineageID)
	assert.Equal(t, web.ID, *entry.LineageID, "the copy belongs to the same service lineage")
	assert.Equal(t, meshdb.EncryptedString("MODE=prod"), entry.EnvVars)

	var entryBuild, prodBuild meshdb.BuildConfig
	require.NoError(t, first(&entryBuild, "service_id = ?", entry.ID))
	assert.Equal(t, "acme/web", entryBuild.GitRepo)
	assert.True(t, entryBuild.AutoDeploy, "the entry level is the one that builds")
	assert.Empty(t, string(entryBuild.DeployToken), "a copy does not inherit the original's deploy token")
	require.NoError(t, first(&prodBuild, "service_id = ?", web.ID))
	assert.False(t, prodBuild.AutoDeploy, "production now receives promotions instead of building on push")

	var none meshdb.Service
	assert.Error(t, first(&none, "project_id = ? AND name = ?", s1, "web"),
		"a level the group has not reached yet has none of it")

	_, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "again", ServiceIDs: []uuid.UUID{entry.ID}, Path: []uuid.UUID{s1, prod}})
	assert.ErrorIs(t, err, service.ErrGroupMember, "a service is in one group, whichever level it is named from")

	groups, err := svcs.Promotions.ListGroups(ctx, s1)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, group.ID, groups[0].ID)
	assert.Equal(t, []uuid.UUID{web.ID}, groups[0].Lineages)
}

// Promotion deploys the image the level below ran, as it is, to the next
// level on the group's path, creating the service there the first time.
func TestPromotionMovesTheSameImageUpThePath(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)
	first := func(dest any, conds ...any) error { return gdb.First(dest, conds...).Error }
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "pg1", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s2, s1, prod}})
	require.NoError(t, err)

	_, err = svcs.Promotions.Promote(ctx, s2, group.ID)
	assert.ErrorIs(t, err, service.ErrNothingToPromote, "nothing built in staging2 yet")
	_, err = svcs.Promotions.Promote(ctx, prod, group.ID)
	assert.ErrorIs(t, err, service.ErrGroupTopLevel)

	next, err := svcs.Promotions.NextLevel(ctx, s2, group.ID)
	require.NoError(t, err)
	assert.Equal(t, s1, next)

	// staging2 built and ran an image.
	var entry meshdb.Service
	require.NoError(t, first(&entry, "project_id = ? AND name = ?", s2, "web"))
	built := "registry/web:sha-abc123"
	now := time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: entry.ID, Status: meshdb.DeploymentSuccess, Image: built, DeployedAt: &now,
		Source: meshdb.DeploySourceBuild, SourceBranch: "develop", SourceCommit: "abc1234", SourceCommitMessage: "Add checkout"}).Error)

	result, err := svcs.Promotions.Promote(ctx, s2, group.ID)
	require.NoError(t, err)
	moved := result.Promoted
	require.Len(t, moved, 1)
	assert.Equal(t, built, moved[0].Image)
	assert.True(t, moved[0].CreatedThere, "web was not in staging1 yet")

	var up meshdb.Service
	require.NoError(t, first(&up, "project_id = ? AND name = ?", s1, "web"))
	var upBuild meshdb.BuildConfig
	require.NoError(t, first(&upBuild, "service_id = ?", up.ID))
	assert.False(t, upBuild.AutoDeploy, "above the entry level a service never builds on push")

	require.Eventually(t, func() bool {
		var dep meshdb.Deployment
		return first(&dep, "id = ?", moved[0].Deployment.ID) == nil && dep.Status == meshdb.DeploymentSuccess
	}, 10*time.Second, 50*time.Millisecond, "the promoted deployment succeeds on the fake cluster")

	board, err := svcs.Promotions.Board(ctx, prod)
	require.NoError(t, err)
	require.Len(t, board.Groups, 1)
	images := map[uuid.UUID]string{}
	for _, c := range board.Groups[0].Cells {
		images[c.LevelID] = c.Image
	}
	assert.Equal(t, built, images[s2])
	assert.Equal(t, built, images[s1], "the board shows staging1 running what staging2 built")

	// Each copy says where its image came from.
	var promoted meshdb.Deployment
	require.NoError(t, first(&promoted, "id = ?", moved[0].Deployment.ID))
	assert.Equal(t, meshdb.DeploySourcePromotion, promoted.Source)
	assert.Equal(t, "staging2", promoted.FromLevel)
	assert.Equal(t, "develop", promoted.SourceBranch, "a promotion carries the branch it was built from")
	assert.Equal(t, "abc1234", promoted.SourceCommit)
	assert.Equal(t, "Add checkout", promoted.SourceCommitMessage)
	for _, c := range board.Groups[0].Cells {
		if c.LevelID == s1 {
			assert.Equal(t, "staging2", c.FromLevel)
			assert.Equal(t, "develop", c.SourceBranch)
		}
		if c.LevelID == s2 {
			assert.Equal(t, meshdb.DeploySourceBuild, c.Source)
		}
	}
	for _, c := range board.Ungrouped {
		assert.NotEqual(t, "web", c.ServiceName)
	}
}

// Only what is newer moves. web was rebuilt in staging after production's;
// api was never built there. Promoting moves web alone and says why api stayed.
func TestPromotionMovesOnlyWhatIsNewer(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, web, _ := newChain(t)
	service.UseK8sForTest(svcs, fake.NewSimpleClientset())
	api := meshdb.Service{ProjectID: prod, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication}
	require.NoError(t, gdb.Create(&api).Error)
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: web.ID, Base: meshdb.Base{CreatedAt: old}, Status: meshdb.DeploymentSuccess, Image: "registry/web:v1", DeployedAt: &old}).Error)
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: api.ID, Base: meshdb.Base{CreatedAt: old}, Status: meshdb.DeploymentSuccess, Image: "registry/api:v1", DeployedAt: &old}).Error)

	group, err := svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "app", ServiceIDs: []uuid.UUID{web.ID, api.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)

	_, err = svcs.Promotions.Promote(ctx, s1, group.ID)
	assert.ErrorIs(t, err, service.ErrNothingToPromote, "nothing built in staging1 yet")

	var webS1 meshdb.Service
	require.NoError(t, gdb.First(&webS1, "project_id = ? AND name = ?", s1, "web").Error)
	now := time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: webS1.ID, Status: meshdb.DeploymentSuccess, Image: "registry/web:v2", DeployedAt: &now}).Error)

	result, err := svcs.Promotions.Promote(ctx, s1, group.ID)
	require.NoError(t, err)
	require.Len(t, result.Promoted, 1)
	assert.Equal(t, "web", result.Promoted[0].ServiceName)
	assert.Equal(t, []service.Skipped{{ServiceName: "api", Reason: "never_built"}}, result.Skipped)

	// staging1 builds an api from an old commit: a different image, but older
	// than what production runs, so it is not promoted over it.
	var apiS1 meshdb.Service
	require.NoError(t, gdb.First(&apiS1, "project_id = ? AND name = ?", s1, "api").Error)
	older := old.Add(-time.Hour)
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: apiS1.ID, Base: meshdb.Base{CreatedAt: older}, Status: meshdb.DeploymentSuccess, Image: "registry/api:v0", DeployedAt: &older}).Error)
	require.Eventually(t, func() bool {
		var d meshdb.Deployment
		return gdb.First(&d, "id = ?", result.Promoted[0].Deployment.ID).Error == nil && d.Status == meshdb.DeploymentSuccess
	}, 10*time.Second, 50*time.Millisecond)

	_, err = svcs.Promotions.Promote(ctx, s1, group.ID)
	assert.ErrorIs(t, err, service.ErrNothingToPromote, "web is now unchanged, and api older: nothing moves")
}

// A build logs the commit it cloned; older builder images logged only the hash.
func TestCommitFromBuildLog(t *testing.T) {
	commit, message := service.CommitFromBuildLog("[meshploy-build] Cloned successfully (a1b2c3d)\n[meshploy-build] Commit: a1b2c3d Fix the login redirect\n")
	assert.Equal(t, "a1b2c3d", commit)
	assert.Equal(t, "Fix the login redirect", message)

	commit, message = service.CommitFromBuildLog("[meshploy-build] Cloned successfully (a1b2c3d)\n")
	assert.Equal(t, "a1b2c3d", commit)
	assert.Empty(t, message)

	commit, _ = service.CommitFromBuildLog("Direct image deploy\n")
	assert.Empty(t, commit)
}
