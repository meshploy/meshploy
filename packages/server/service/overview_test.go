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
)

// The overview says what needs someone, most urgent first, from state the
// platform already keeps; and node and domain items only to an admin.
func TestTheOverviewSaysWhatNeedsAttention(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	ids := []uuid.UUID{prod, s1, s2}

	quiet, err := svcs.Overview.Get(ctx, project.OrganizationID, ids, true)
	require.NoError(t, err)
	kinds := func(o *service.Overview) []string {
		out := []string{}
		for _, a := range o.Attention {
			out = append(out, a.Kind)
		}
		return out
	}
	assert.Equal(t, []string{"backup_missing"}, kinds(quiet), "only pg, which has no backups")

	// web goes into a group, staging1 builds something newer than production,
	// a job fails, a node drops off, and a service in production fails.
	_, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "app", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)
	var webS1 meshdb.Service
	require.NoError(t, gdb.First(&webS1, "project_id = ? AND name = ?", s1, "web").Error)
	old, now := time.Now().Add(-2*time.Hour), time.Now()
	require.NoError(t, gdb.Create(&meshdb.Deployment{Base: meshdb.Base{CreatedAt: old}, ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Image: "web:v1", DeployedAt: &old, Source: meshdb.DeploySourceBuild}).Error)
	require.NoError(t, gdb.Create(&meshdb.Deployment{ServiceID: webS1.ID, Status: meshdb.DeploymentSuccess, Image: "web:v2", DeployedAt: &now, Source: meshdb.DeploySourceBuild}).Error)
	require.NoError(t, gdb.Create(&meshdb.Job{ProjectID: s1, Name: "nightly", Image: "busybox", Status: meshdb.JobStatusFailed}).Error)
	require.NoError(t, gdb.Create(&meshdb.Node{OrganizationID: project.OrganizationID, Name: "worker-2", Status: meshdb.NodeOffline}).Error)
	api := meshdb.Service{ProjectID: prod, Name: "api", Slug: "api", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceFailed}
	require.NoError(t, gdb.Create(&api).Error)

	got, err := svcs.Overview.Get(ctx, project.OrganizationID, ids, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"service_failed", "node_offline", "job_failed", "backup_missing", "promotion_waiting", "hotfix_running"}, kinds(got),
		"most urgent first: down, then later trouble, then waiting on someone")
	for _, a := range got.Attention {
		switch a.Kind {
		case "promotion_waiting":
			assert.Equal(t, "web in staging1 is ready for production", a.Title)
		case "job_failed":
			assert.Equal(t, "in CoreLine (staging1)", a.Detail)
		}
	}
	assert.Equal(t, map[string]int{"failed": 1, "stopped": 2}, got.Stats["services"], "services counted across every level")

	require.NoError(t, gdb.Create(&meshdb.Domain{OrganizationID: project.OrganizationID, BaseDomain: "acme.dev", VerifyToken: "t"}).Error)
	admin, err := svcs.Overview.Get(ctx, project.OrganizationID, ids, true)
	require.NoError(t, err)
	assert.Contains(t, kinds(admin), "domain_unverified")
	member, err := svcs.Overview.Get(ctx, project.OrganizationID, ids, false)
	require.NoError(t, err)
	assert.NotContains(t, kinds(member), "domain_unverified", "domains are an admin's to manage")
	assert.Contains(t, kinds(member), "node_offline", "every member sees the mesh's nodes")

	list, err := svcs.Projects.ListWithCounts(ctx, project.OrganizationID, service.ProjectListOptions{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Len(t, list[0].Levels, 2)
	assert.Equal(t, "staging1", list[0].Levels[0].Name, "the project list carries its levels, highest first")
}

// Delivery counts what ran each day and measures production: deploys a week,
// the share that failed, how long a failure took to fix, and how long an image
// took from its build in staging to production.
func TestDeliveryMeasuresHowTheWorkspaceShips(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, s2, web, _ := newChain(t)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	webS1 := meshdb.Service{ProjectID: s1, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication, LineageID: &web.ID}
	require.NoError(t, gdb.Create(&webS1).Error)
	day := func(ago int, h int) time.Time {
		return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -ago).Add(time.Duration(h) * time.Hour)
	}
	mk := func(svc uuid.UUID, at time.Time, status meshdb.DeploymentStatus, src string, from *uuid.UUID) meshdb.Deployment {
		d := meshdb.Deployment{Base: meshdb.Base{CreatedAt: at}, ServiceID: svc, Status: status, Image: "web:x", Source: src, FromDeploymentID: from}
		require.NoError(t, gdb.Create(&d).Error)
		return d
	}
	built := mk(webS1.ID, day(3, 1), meshdb.DeploymentSuccess, meshdb.DeploySourceBuild, nil)
	mk(web.ID, day(2, 1), meshdb.DeploymentFailed, meshdb.DeploySourceBuild, nil)
	mk(web.ID, day(2, 2), meshdb.DeploymentSuccess, meshdb.DeploySourceBuild, nil)
	mk(web.ID, day(1, 1), meshdb.DeploymentSuccess, meshdb.DeploySourcePromotion, &built.ID)
	mk(web.ID, day(20, 1), meshdb.DeploymentFailed, meshdb.DeploySourceBuild, nil) // outside the window
	job := meshdb.Job{ProjectID: prod, Name: "nightly", Image: "busybox"}
	require.NoError(t, gdb.Create(&job).Error)
	require.NoError(t, gdb.Create(&meshdb.JobRun{Base: meshdb.Base{CreatedAt: day(0, 0).Add(time.Minute)}, JobID: job.ID, Status: meshdb.JobStatusSuccess}).Error)

	got, err := svcs.Overview.Get(ctx, project.OrganizationID, []uuid.UUID{prod, s1, s2}, true)
	require.NoError(t, err)
	d := got.Delivery
	require.Len(t, d.Days, service.DeliveryDays)
	last := d.Days[len(d.Days)-1]
	assert.Equal(t, time.Now().UTC().Format("2006-01-02"), last.Date)
	assert.Equal(t, 1, last.JobsSucceeded)
	assert.Equal(t, 1, d.Days[len(d.Days)-3].DeployFailed)
	assert.Equal(t, 1, d.Days[len(d.Days)-3].Deployed)
	assert.Equal(t, 1, d.Days[len(d.Days)-4].Deployed, "staging's build counts on the chart")

	assert.InDelta(t, 1.0, d.DeploysPerWeek, 0.001, "two good production deploys in two weeks")
	require.NotNil(t, d.ChangeFailureRate)
	assert.InDelta(t, 1.0/3, *d.ChangeFailureRate, 0.001)
	require.NotNil(t, d.RecoverySeconds)
	assert.InDelta(t, 3600, *d.RecoverySeconds, 1)
	require.NotNil(t, d.PromotionSeconds)
	assert.InDelta(t, 48*3600, *d.PromotionSeconds, 1, "built in staging two days before it reached production")
	assert.Equal(t, 4, sum(d.Projects[prod]), "the project's sparkline counts every level")

	empty, err := svcs.Overview.Get(ctx, project.OrganizationID, nil, true)
	require.NoError(t, err)
	assert.Nil(t, empty.Delivery.ChangeFailureRate, "nothing to measure is not a zero")
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}
