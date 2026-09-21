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
)

// The overview's activity feed. What it must get right is the scoping - the
// caller decides which projects - and the interleaving of two sources that each
// have their own idea of what a row is.
func TestListRecentActivity(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	orgID := seedOrg(t, db, "acme", nil)
	shop := mustProject(t, db, orgID, "shop")
	admin := mustProject(t, db, orgID, "admin")

	shopSvc := mustService(t, db, shop, "storefront")
	adminSvc := mustService(t, db, admin, "back-office")
	nightly := mustJob(t, db, shop, "nightly-report", "0 2 * * *")

	// Oldest first, so "newest first" is a claim the fixture can disprove, and
	// the job run lands between two deployments so the merge has to interleave
	// rather than concatenate.
	mustDeployment(t, db, shopSvc, meshdb.DeploymentSuccess, "nginx:1")
	mustDeployment(t, db, adminSvc, meshdb.DeploymentFailed, "nginx:2")
	mustJobRun(t, db, nightly, meshdb.JobStatusFailed)
	mustDeployment(t, db, shopSvc, meshdb.DeploymentSuccess, "nginx:3")

	t.Run("both sources, newest first, carrying the names needed to read it", func(t *testing.T) {
		got, err := svcs.Activity.ListRecent(ctx, []uuid.UUID{shop, admin}, 20)
		require.NoError(t, err)
		require.Len(t, got, 4)

		assert.Equal(t, "nginx:3", got[0].Detail, "newest first")
		assert.Equal(t, service.ActivityDeployment, got[0].Kind)
		assert.Equal(t, "storefront", got[0].ResourceName)
		assert.Equal(t, "shop", got[0].ProjectName)
		assert.Equal(t, shop, got[0].ProjectID, "the feed has to be able to link back")
		assert.Equal(t, "application", got[0].ResourceType,
			"the type picks the icon, so a mis-aliased column is a silently wrong row")

		// The job run was written between two deployments and has to appear
		// there: a feed that concatenated its sources would put it last.
		assert.Equal(t, service.ActivityJobRun, got[1].Kind)
		assert.Equal(t, "nightly-report", got[1].ResourceName)
		assert.Equal(t, "job", got[1].ResourceType)
		assert.Equal(t, "0 2 * * *", got[1].Detail, "a job's schedule is its one useful detail")
		assert.Equal(t, string(meshdb.JobStatusFailed), got[1].Status,
			"a failed 3am cron is the entry this feed exists for")

		assert.Equal(t, string(meshdb.DeploymentFailed), got[2].Status,
			"a failure is the entry most worth seeing and must not be filtered out")
	})

	t.Run("only the projects the caller was given", func(t *testing.T) {
		got, err := svcs.Activity.ListRecent(ctx, []uuid.UUID{shop}, 20)
		require.NoError(t, err)
		require.Len(t, got, 3, "two deployments and the job run, all in shop")
		for _, d := range got {
			assert.Equal(t, "shop", d.ProjectName,
				"a member who can see one project must not learn what deployed in another")
		}
	})

	// A member with no grants gets no projects, which is not the same question
	// as "every project": it has to return nothing rather than everything.
	t.Run("no projects means nothing, not everything", func(t *testing.T) {
		got, err := svcs.Activity.ListRecent(ctx, nil, 20)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	// Each source is asked for the limit and the merge trims, so the trim has
	// to happen after the interleave or the newest rows can be lost.
	t.Run("the limit is honoured across both sources", func(t *testing.T) {
		got, err := svcs.Activity.ListRecent(ctx, []uuid.UUID{shop, admin}, 2)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "nginx:3", got[0].Detail)
		assert.Equal(t, service.ActivityJobRun, got[1].Kind)
	})
}

func mustProject(t *testing.T, db *gorm.DB, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	p := meshdb.Project{OrganizationID: orgID, Name: name, Slug: name}
	require.NoError(t, db.Create(&p).Error)
	return p.ID
}

func mustService(t *testing.T, db *gorm.DB, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	s := meshdb.Service{
		ProjectID: projectID,
		Name:      name,
		Type:      meshdb.ServiceTypeApplication,
	}
	require.NoError(t, db.Create(&s).Error)
	return s.ID
}

func mustDeployment(t *testing.T, db *gorm.DB, serviceID uuid.UUID, status meshdb.DeploymentStatus, image string) {
	t.Helper()
	d := meshdb.Deployment{ServiceID: serviceID, Status: status, Image: image}
	require.NoError(t, db.Create(&d).Error)
	time.Sleep(time.Millisecond)
}

func mustJob(t *testing.T, db *gorm.DB, projectID uuid.UUID, name, schedule string) uuid.UUID {
	t.Helper()
	j := meshdb.Job{ProjectID: projectID, Name: name, IsCron: schedule != "", Schedule: schedule, Image: "busybox"}
	require.NoError(t, db.Create(&j).Error)
	return j.ID
}

func mustJobRun(t *testing.T, db *gorm.DB, jobID uuid.UUID, status meshdb.JobStatus) {
	t.Helper()
	r := meshdb.JobRun{JobID: jobID, Status: status}
	require.NoError(t, db.Create(&r).Error)
	time.Sleep(time.Millisecond)
}
