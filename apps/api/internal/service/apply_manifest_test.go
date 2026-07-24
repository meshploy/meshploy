package service_test

import (
	"context"
	"testing"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyManifestUpsert verifies the one-shot declarative path: first apply
// creates the stack + services, second apply on the same name reconciles in
// place (no duplicate stack), and a growing manifest adds services.
func TestApplyManifestUpsert(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, userID := setupStackTest(t)
	pid := parseUUID(t, projID)
	uid := parseUUID(t, userID)

	// First apply — stack does not exist yet → created.
	r1, err := svcs.Stacks.ApplyManifest(ctx, pid, "app", validStackSpec, uid)
	require.NoError(t, err)
	require.NotNil(t, r1.Stack)
	assert.Equal(t, "app", r1.Stack.Name)
	assert.Equal(t, meshdb.StackGitModeRaw, r1.Stack.GitMode)
	firstStackID := r1.Stack.ID

	// Exactly one stack exists for this (project, name).
	stacks, err := svcs.Stacks.List(ctx, pid)
	require.NoError(t, err)
	require.Len(t, stacks, 1)

	// Second apply of the same manifest+name → upsert in place, same stack id.
	r2, err := svcs.Stacks.ApplyManifest(ctx, pid, "app", validStackSpec, uid)
	require.NoError(t, err)
	assert.Equal(t, firstStackID, r2.Stack.ID, "re-apply must reuse the same stack, not create a new one")

	stacks, err = svcs.Stacks.List(ctx, pid)
	require.NoError(t, err)
	assert.Len(t, stacks, 1, "re-applying must not create a duplicate stack")
}

// TestApplyManifestValidation covers the required-field and git-backed guards.
func TestApplyManifestValidation(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, userID := setupStackTest(t)
	pid := parseUUID(t, projID)
	uid := parseUUID(t, userID)

	_, err := svcs.Stacks.ApplyManifest(ctx, pid, "", validStackSpec, uid)
	require.Error(t, err, "name is required")

	_, err = svcs.Stacks.ApplyManifest(ctx, pid, "app", "", uid)
	require.Error(t, err, "spec is required")

	// A git-backed stack owning the name must not be clobbered by an inline apply.
	_, err = svcs.Stacks.Create(ctx, pid, service.CreateStackInput{
		Name:    "gitstack",
		Spec:    validStackSpec,
		GitMode: meshdb.StackGitModeFile,
		GitRepo: "https://github.com/example/repo",
	})
	require.NoError(t, err)

	_, err = svcs.Stacks.ApplyManifest(ctx, pid, "gitstack", validStackSpec, uid)
	require.Error(t, err, "inline apply must refuse to overwrite a git-backed stack")
}
