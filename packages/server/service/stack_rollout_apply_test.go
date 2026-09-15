package service_test

import (
	"context"
	"strings"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An apply rolls out what it changed, and says why it left the rest alone.
// This instance has no cluster, so nothing is triggered: what is under test is
// which services an apply picks, which is what the warnings report.
func TestApplyReportsWhatItChanged(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "roller", Email: "roller@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "rollout", "rollout")
	require.NoError(t, err)

	spec := func(image string) string {
		return `
services:
  web:
    image: ` + image + `
    expose: ["8080"]
  cache:
    image: redis:7
    x-meshploy:
      type: database
      database:
        engine: redis
`
	}
	apply := func(spec string, opts ...service.ApplyOptions) *service.ApplyResult {
		t.Helper()
		r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "infra", Spec: spec}, user.ID, opts...)
		require.NoError(t, err)
		require.Empty(t, r.Errors)
		return r
	}
	// What the cluster runs, which an apply compares against. A deploy records
	// it; these tests have no cluster, so they say so themselves.
	markDeployed := func(names ...string) {
		t.Helper()
		for _, name := range names {
			var svc meshdb.Service
			require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, name).First(&svc).Error)
			require.NoError(t, svcs.Workloads.MarkDeployed(ctx, svc.ID))
		}
	}
	setStatus := func(name string, status meshdb.ServiceStatus) {
		t.Helper()
		require.NoError(t, gdb.Model(&meshdb.Service{}).
			Where("project_id = ? AND name = ?", proj.ID, name).
			Update("status", status).Error)
	}
	changedWarnings := func(r *service.ApplyResult) []string {
		var out []string
		for _, w := range r.Warnings {
			if strings.Contains(w, "changed") {
				out = append(out, w)
			}
		}
		return out
	}

	r := apply(spec("nginx:1.27"))
	require.ElementsMatch(t, []string{"web", "cache"}, r.Created)
	markDeployed("web", "cache")

	// Re-applying the same file changes nothing, so nothing is rolled out and
	// nothing is reported.
	r = apply(spec("nginx:1.27"))
	require.ElementsMatch(t, []string{"web", "cache"}, r.Updated)
	assert.Empty(t, changedWarnings(r), "an unchanged re-apply must roll nothing out")

	// A running service takes the change with no warning: it is rolled out.
	setStatus("web", meshdb.ServiceRunning)
	setStatus("cache", meshdb.ServiceRunning)
	r = apply(spec("nginx:1.28"))
	assert.Empty(t, changedWarnings(r), "a running service is rolled out, not reported")
	markDeployed("web", "cache")

	// A stopped one is named instead, and never started.
	setStatus("web", meshdb.ServiceStopped)
	r = apply(spec("nginx:1.29"))
	require.Len(t, changedWarnings(r), 1)
	assert.Contains(t, changedWarnings(r)[0], "web changed but is not running")
	var web meshdb.Service
	require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, "web").First(&web).Error)
	assert.Equal(t, meshdb.ServiceStopped, web.Status, "an apply must not start a stopped service")

	markDeployed("web")

	// A changed database is reported rather than provisioned again.
	require.NoError(t, gdb.Model(&meshdb.DatabaseConfig{}).
		Where("service_id IN (?)", gdb.Model(&meshdb.Service{}).Select("id").Where("project_id = ? AND name = ?", proj.ID, "cache")).
		Update("storage_gb", 20).Error)
	setStatus("web", meshdb.ServiceRunning)
	r = apply(strings.Replace(spec("nginx:1.29"), "image: redis:7", "image: redis:7.2", 1))
	require.Len(t, changedWarnings(r), 1)
	assert.Contains(t, changedWarnings(r)[0], "cache changed: a managed database is not rolled out automatically")

	// no-deploy reconciles the records and says nothing about rolling out.
	setStatus("web", meshdb.ServiceStopped)
	r = apply(spec("nginx:1.30"), service.ApplyOptions{NoDeploy: true})
	assert.Empty(t, changedWarnings(r))
	assert.Empty(t, r.Deployed)
	var after meshdb.Service
	require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, "web").First(&after).Error)
	assert.Equal(t, "nginx:1.30", after.Image, "the records are still reconciled")
}

// A deploy assigns a NodePort; a re-apply that does not change the ports must
// not read that as a change.
func TestApplyDoesNotSeeAssignedNodePortsAsAChange(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "ports", Email: "ports@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "ports", "ports")
	require.NoError(t, err)

	spec := "services:\n  web:\n    image: nginx\n    ports: [\"8080:8080\"]\n"
	_, err = svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "infra", Spec: spec}, user.ID)
	require.NoError(t, err)

	var web meshdb.Service
	require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, "web").First(&web).Error)
	require.NoError(t, svcs.Workloads.MarkDeployed(ctx, web.ID))
	require.NoError(t, gdb.Model(&meshdb.ServicePort{}).Where("service_id = ?", web.ID).Update("node_port", 31234).Error)
	require.NoError(t, gdb.Model(&web).Update("status", meshdb.ServiceStopped).Error)

	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "infra", Spec: spec}, user.ID)
	require.NoError(t, err)
	assert.Empty(t, r.Warnings, "an assigned NodePort is not a spec change")

	var port meshdb.ServicePort
	require.NoError(t, gdb.Where("service_id = ?", web.ID).First(&port).Error)
	assert.Equal(t, 31234, port.NodePort, "and it is kept")
}

// A service is created with the resources its spec asks for. They used to be
// dropped at creation, so a stack service ran on the column defaults until
// something updated it, and the first re-apply looked like a change.
func TestStackServiceKeepsItsSpecResources(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "limits", Email: "limits@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "limits", "limits")
	require.NoError(t, err)

	_, err = svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "infra", Spec: `
services:
  web:
    image: nginx
    x-meshploy:
      deploy:
        cpu_limit: 2000m
        memory_limit: 2Gi
`}, user.ID)
	require.NoError(t, err)

	var web meshdb.Service
	require.NoError(t, gdb.Where("project_id = ? AND name = ?", proj.ID, "web").First(&web).Error)
	assert.Equal(t, "2000m", web.CPULimit)
	assert.Equal(t, "2Gi", web.MemoryLimit)
	assert.Equal(t, "100m", web.CPURequest, "and the defaults for what the spec leaves out")
	assert.Equal(t, "256Mi", web.MemoryRequest)
}

// A rollout skipped because a deploy was in flight is not forgotten.
//
// The record is updated either way, so comparing records finds nothing to do on
// every later apply, and the service stays behind for good with only the first
// apply's warning to say so. This is the Keycloak and Neo4j case.
func TestASkippedRolloutIsStillOwed(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "owed", Email: "owed@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "owed", "owed")
	require.NoError(t, err)

	spec := func(image string) string {
		return "services:\n  web:\n    image: " + image + "\n    expose: [\"8080\"]\n"
	}
	apply := func(s string) *service.ApplyResult {
		t.Helper()
		r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "infra", Spec: s}, user.ID)
		require.NoError(t, err)
		require.Empty(t, r.Errors)
		return r
	}
	status := func(s meshdb.ServiceStatus) {
		t.Helper()
		require.NoError(t, gdb.Model(&meshdb.Service{}).Where("project_id = ?", proj.ID).Update("status", s).Error)
	}
	warnings := func(r *service.ApplyResult) string { return strings.Join(r.Warnings, " ") }

	apply(spec("nginx:1.27"))
	var web meshdb.Service
	require.NoError(t, gdb.Where("project_id = ?", proj.ID).First(&web).Error)
	require.NoError(t, svcs.Workloads.MarkDeployed(ctx, web.ID))

	// A deploy is in flight, so this apply updates the record and rolls nothing
	// out. That is deliberate: the rollout owns the outcome.
	status(meshdb.ServiceDeploying)
	r := apply(spec("nginx:1.28"))
	assert.Contains(t, warnings(r), "web changed while it was deploying")

	// That deploy ends without carrying the change, which is what a crash loop
	// looks like. The next apply must still know the cluster is behind, even
	// though the record already says 1.28.
	status(meshdb.ServiceStopped)
	r = apply(spec("nginx:1.28"))
	assert.Contains(t, warnings(r), "web changed but is not running",
		"an apply after a skipped rollout must still owe it")

	// Once the cluster catches up, it stops being owed.
	require.NoError(t, svcs.Workloads.MarkDeployed(ctx, web.ID))
	r = apply(spec("nginx:1.28"))
	assert.NotContains(t, warnings(r), "web changed")
}
