package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (e primaryEnv) serviceWithDeployHook(t *testing.T, name string) uuid.UUID {
	t.Helper()
	project := &meshdb.Project{OrganizationID: e.org, Name: "p-" + name, Slug: "p-" + uuid.NewString()[:6]}
	require.NoError(t, e.gdb.Create(project).Error)
	svc := &meshdb.Service{ProjectID: project.ID, Name: name}
	require.NoError(t, e.gdb.Create(svc).Error)
	require.NoError(t, e.gdb.Create(&meshdb.BuildConfig{ServiceID: svc.ID, DeployToken: meshdb.EncryptedString("dtkn-" + name)}).Error)
	return svc.ID
}

// A deploy webhook lives in someone's CI, out of Meshploy's reach. Where its
// calls arrive is the only evidence of where it points - and it clears itself
// once the CI job is updated, with nothing to mark.
func TestADomainCIStillCallsCannotGoUntilTheCallsMove(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	api := e.serviceWithDeployHook(t, "api")
	web := e.serviceWithDeployHook(t, "web")
	e.serviceWithDeployHook(t, "never-called")

	require.NoError(t, e.svc.Domains.RecordDeployHookCall(ctx, api, "api.old.test:443"))
	require.NoError(t, e.svc.Domains.RecordDeployHookCall(ctx, web, "console.old.test"))

	_, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)
	old, _ := e.svc.Domains.Get(ctx, e.old.ID)

	hooks, err := e.svc.Domains.DeployHooksOnDomain(ctx, e.org, old)
	require.NoError(t, err)
	names := []string{}
	for _, h := range hooks {
		names = append(names, h.ServiceName)
		assert.NotNil(t, h.CalledAt)
	}
	assert.Equal(t, []string{"api", "web"}, names,
		"called through either platform name counts; a webhook never called shows no use to report")

	_, err = e.svc.Domains.StartRetiring(ctx, e.old.ID)
	require.NoError(t, err)
	assert.ErrorContains(t, e.svc.Domains.Delete(ctx, e.old.ID), "2 CI deploy webhooks were last called through old.test")

	// The CI job for api is updated: its next call arrives through the new
	// name, and it drops off without anyone saying so.
	require.NoError(t, e.svc.Domains.RecordDeployHookCall(ctx, api, "api.new.test"))
	// web's job was deleted; it will never call again, so it is forgotten.
	require.NoError(t, e.svc.Domains.ForgetDeployHookCall(ctx, e.org, web))

	hooks, err = e.svc.Domains.DeployHooksOnDomain(ctx, e.org, old)
	require.NoError(t, err)
	assert.Empty(t, hooks)
	require.NoError(t, e.svc.Domains.Delete(ctx, e.old.ID))
}

// A job forgotten by mistake calls again, and is recorded again.
func TestAForgottenDeployHookThatCallsAgainIsBack(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	api := e.serviceWithDeployHook(t, "api")
	require.NoError(t, e.svc.Domains.RecordDeployHookCall(ctx, api, "api.old.test"))
	require.NoError(t, e.svc.Domains.ForgetDeployHookCall(ctx, e.org, api))
	require.NoError(t, e.svc.Domains.RecordDeployHookCall(ctx, api, "api.old.test"))

	old, _ := e.svc.Domains.Get(ctx, e.old.ID)
	hooks, err := e.svc.Domains.DeployHooksOnDomain(ctx, e.org, old)
	require.NoError(t, err)
	assert.Len(t, hooks, 1)
}

// Another organisation's service is not one to forget.
func TestForgettingIsScopedToTheOrganisation(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	api := e.serviceWithDeployHook(t, "api")
	err := e.svc.Domains.ForgetDeployHookCall(ctx, uuid.New(), api)
	assert.ErrorContains(t, err, "service not found")
}
