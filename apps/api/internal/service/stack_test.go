package service_test

import (
	"context"
	"testing"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validStackSpec = `
services:
  web:
    image: nginx:latest
    ports:
      - "80:80"
  api:
    image: alpine:latest
    command: ["sh", "-c", "echo hello"]
`

func setupStackTest(t *testing.T) (*service.Services, string, string, string) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	user, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "stackuser",
		Email:    "stackuser@example.com",
		Password: "pass",
	})
	require.NoError(t, err)

	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)

	proj, err := svcs.Projects.Create(ctx, org.ID, "stack-project", "stack-project")
	require.NoError(t, err)

	return svcs, org.ID.String(), proj.ID.String(), user.ID.String()
}

func TestStackCreate(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	t.Run("creates stack with spec", func(t *testing.T) {
		stack, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "my-stack", Spec: validStackSpec})
		require.NoError(t, err)
		assert.NotEmpty(t, stack.ID)
		assert.Equal(t, "my-stack", stack.Name)
		assert.Equal(t, validStackSpec, stack.Spec)
		assert.Equal(t, meshdb.StackIdle, stack.Status)
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "", Spec: validStackSpec})
		require.Error(t, err)
	})
}

func TestStackGet(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	created, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "fetch-stack", Spec: validStackSpec})
	require.NoError(t, err)

	got, err := svcs.Stacks.Get(ctx, created.ID, pid)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "fetch-stack", got.Name)
}

func TestStackApply(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, userID := setupStackTest(t)
	pid := parseUUID(t, projID)
	uid := parseUUID(t, userID)

	t.Run("apply creates services from spec", func(t *testing.T) {
		stack, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "apply-stack", Spec: validStackSpec})
		require.NoError(t, err)

		result, err := svcs.Stacks.Apply(ctx, stack.ID, uid, nil)
		require.NoError(t, err)
		assert.NotNil(t, result.Stack)
		// Two services defined in the spec — both should be created.
		assert.Len(t, result.Created, 2)
		assert.Empty(t, result.Errors)
	})

	t.Run("apply is idempotent — second apply updates not recreates", func(t *testing.T) {
		stack, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "idempotent-stack", Spec: validStackSpec})
		require.NoError(t, err)

		_, err = svcs.Stacks.Apply(ctx, stack.ID, uid, nil)
		require.NoError(t, err)

		result2, err := svcs.Stacks.Apply(ctx, stack.ID, uid, nil)
		require.NoError(t, err)
		// On second apply nothing is newly created.
		assert.Empty(t, result2.Created)
		assert.Empty(t, result2.Errors)
	})
}

func TestStackDelete(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	stack, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "del-stack", Spec: validStackSpec})
	require.NoError(t, err)

	require.NoError(t, svcs.Stacks.Delete(ctx, stack.ID))

	_, err = svcs.Stacks.Get(ctx, stack.ID, pid)
	require.Error(t, err)
}

func TestStackList(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	for i := range 3 {
		_, err := svcs.Stacks.Create(ctx, pid, service.CreateStackInput{Name: "stack-" + string(rune('a'+i))})
		require.NoError(t, err)
	}

	stacks, err := svcs.Stacks.List(ctx, pid)
	require.NoError(t, err)
	assert.Len(t, stacks, 3)
}
