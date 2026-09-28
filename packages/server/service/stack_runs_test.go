package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every apply keeps a record of itself, with what it said, so a stack's
// rollouts can be looked back on; one that staged records only keeps none.
func TestAnApplyKeepsARunOfItself(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	spec := "services:\n  web:\n    image: nginx\n"
	r, err := e.svcs.Stacks.ApplyManifest(ctx, e.project.ID, service.ManifestInput{Name: "site", Spec: spec}, e.org.ID)
	require.NoError(t, err)
	require.NotNil(t, r.RunID)

	runs, err := e.svcs.Stacks.ListRuns(ctx, r.Stack.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "apply", runs[0].Kind)
	assert.Equal(t, meshdb.StackRunSucceeded, runs[0].Status, "nothing to roll out without a cluster: done at once")
	assert.Equal(t, []string{"web"}, runs[0].Result.Created)
	assert.NotNil(t, runs[0].FinishedAt)

	run, err := e.svcs.Stacks.GetRun(ctx, r.Stack.ID, *r.RunID)
	require.NoError(t, err)
	assert.Equal(t, runs[0].ID, run.ID)

	_, err = e.svcs.Stacks.ApplyManifest(ctx, e.project.ID, service.ManifestInput{Name: "site", Spec: spec}, e.org.ID,
		service.ApplyOptions{NoDeploy: true})
	require.NoError(t, err)
	runs, err = e.svcs.Stacks.ListRuns(ctx, r.Stack.ID, 10)
	require.NoError(t, err)
	assert.Len(t, runs, 1, "records only: no rollout, no run")
}
