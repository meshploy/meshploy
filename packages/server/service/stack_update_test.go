package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An update changes only what it is sent. The variables page sends only
// variables and the sync banner only git_mode; neither may touch the spec or
// the rest of the git source.
func TestStackUpdateKeepsWhatItIsNotSent(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	stack, err := svcs.Stacks.Create(ctx, parseUUID(t, projID), service.CreateStackInput{
		Name: "from-git", Spec: validStackSpec, GitMode: meshdb.StackGitModeFile,
		GitRepo: "https://github.com/acme/infra", GitBranch: "prod", GitPath: "deploy/compose.yml",
	})
	require.NoError(t, err)

	kept := func(what string, got *meshdb.Stack) {
		t.Helper()
		assert.Equal(t, "https://github.com/acme/infra", got.GitRepo, what)
		assert.Equal(t, "prod", got.GitBranch, what)
		assert.Equal(t, "deploy/compose.yml", got.GitPath, what)
	}

	got, err := svcs.Stacks.Update(ctx, stack.ID, service.UpdateStackInput{Variables: map[string]string{"POSTGRES_USER": "app"}})
	require.NoError(t, err)
	kept("variables", got)
	assert.Equal(t, validStackSpec, got.Spec, "variables")
	assert.Equal(t, meshdb.StackGitModeFile, got.GitMode, "variables")
	assert.NotEmpty(t, got.Variables)

	repo := meshdb.StackGitModeRepo
	got, err = svcs.Stacks.Update(ctx, stack.ID, service.UpdateStackInput{GitMode: &repo})
	require.NoError(t, err)
	kept("git mode", got)
	assert.Equal(t, validStackSpec, got.Spec, "git mode")
	assert.Equal(t, meshdb.StackGitModeRepo, got.GitMode)
	assert.NotEmpty(t, got.Variables, "git mode keeps the variables")

	spec := "services:\n  web:\n    image: nginx\n"
	got, err = svcs.Stacks.Update(ctx, stack.ID, service.UpdateStackInput{Spec: &spec})
	require.NoError(t, err)
	kept("spec", got)
	assert.Equal(t, spec, got.Spec, "a git stack's spec is stored as given")
	assert.Equal(t, meshdb.StackGitModeRepo, got.GitMode, "spec")
}
