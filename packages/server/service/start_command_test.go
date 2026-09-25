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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A start command replaces the image's own, run through its shell; clearing
// it gives the image back its own. Install and build commands are kept on the
// build config, and all three travel with a copy into a level.
func TestStartInstallAndBuildCommands(t *testing.T) {
	ctx := context.Background()
	svcs, gdb, prod, s1, _, _, _ := newChain(t)
	client := fake.NewSimpleClientset()
	service.UseK8sForTest(svcs, client)

	api, err := svcs.Workloads.Create(ctx, prod, service.CreateWorkloadInput{
		Name: "api", Image: "node:22-alpine", StartCommand: "  node dist/server.js | tee /tmp/log  ",
	})
	require.NoError(t, err)
	assert.Equal(t, "node dist/server.js | tee /tmp/log", api.StartCommand, "trimmed")

	web, err := svcs.Workloads.Create(ctx, prod, service.CreateWorkloadInput{
		Name: "shop", GitRepo: "acme/shop", Builder: meshdb.BuilderRailpack,
		InstallCommand: "pnpm install", BuildCommand: "pnpm build",
	})
	require.NoError(t, err)
	bc, err := svcs.Workloads.GetBuildConfig(ctx, web.ID)
	require.NoError(t, err)
	assert.Equal(t, "pnpm install", bc.InstallCommand)
	assert.Equal(t, "pnpm build", bc.BuildCommand)

	empty := ""
	_, err = svcs.Workloads.UpsertBuildConfig(ctx, web.ID, service.UpdateBuildConfigInput{BuildCommand: &empty})
	require.NoError(t, err)
	bc, err = svcs.Workloads.GetBuildConfig(ctx, web.ID)
	require.NoError(t, err)
	assert.Equal(t, "", bc.BuildCommand, "cleared: the builder decides again")
	assert.Equal(t, "pnpm install", bc.InstallCommand, "the other is untouched")

	// Deployed, the container runs the start command through the shell.
	_, err = svcs.Deployments.DeployImage(ctx, api.ID, "node:22-alpine", "test", service.Provenance{})
	require.NoError(t, err)
	var project meshdb.Project
	require.NoError(t, gdb.First(&project, "id = ?", prod).Error)
	require.Eventually(t, func() bool {
		d, err := client.AppsV1().Deployments(project.Slug).List(ctx, metav1.ListOptions{})
		return err == nil && len(d.Items) > 0
	}, 10*time.Second, 50*time.Millisecond)
	deps, _ := client.AppsV1().Deployments(project.Slug).List(ctx, metav1.ListOptions{})
	c := deps.Items[0].Spec.Template.Spec.Containers[0]
	assert.Equal(t, []string{"/bin/sh", "-c", "node dist/server.js | tee /tmp/log"}, c.Command)
	assert.Empty(t, c.Args)

	// Copied into a level, the commands come too.
	_, err = svcs.Promotions.CreateGroup(ctx, prod, service.GroupInput{Name: "shop", ServiceIDs: []uuid.UUID{web.ID}, Path: []uuid.UUID{s1, prod}})
	require.NoError(t, err)
	var copy meshdb.Service
	require.NoError(t, gdb.First(&copy, "project_id = ? AND name = ?", s1, "shop").Error)
	copyBC, err := svcs.Workloads.GetBuildConfig(ctx, copy.ID)
	require.NoError(t, err)
	assert.Equal(t, "pnpm install", copyBC.InstallCommand)

	// Cleared, the image's own command comes back.
	_, err = svcs.Workloads.Update(ctx, api.ID, service.UpdateWorkloadInput{StartCommand: &empty})
	require.NoError(t, err)
	var after meshdb.Service
	require.NoError(t, gdb.First(&after, "id = ?", api.ID).Error)
	assert.Equal(t, "", after.StartCommand)
}
