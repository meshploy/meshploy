package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMigrationEnv(t *testing.T, reporting bool) (upgradeEnv, *service.Services, string) {
	t.Helper()
	e := newUpgradeEnv(t, "edge", true)
	hostDir := t.TempDir()
	for _, d := range []string{hostagent.StateDir(hostDir), hostagent.InboxDir(hostDir)} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	if reporting {
		agent, _ := json.Marshal(hostagent.Agent{Version: "0.16.0", HeartbeatAt: time.Now().UTC()})
		require.NoError(t, os.WriteFile(filepath.Join(hostagent.StateDir(hostDir), hostagent.AgentFile), agent, 0o644))
	}
	return e, service.New(e.db, &config.Config{UpgradeDir: e.dir, HostDir: hostDir}), hostDir
}

// The owner's request lands in the inbox as a request the agent accepts, and
// shows as queued until the agent picks it up.
func TestRequestMigrationQueuesForTheAgent(t *testing.T) {
	ctx := context.Background()
	e, svc, hostDir := newMigrationEnv(t, true)

	st, err := svc.System.RequestMigration(ctx, e.owner, "plan", nil)
	require.NoError(t, err)
	require.Equal(t, "queued", st.State)

	path := filepath.Join(hostagent.InboxDir(hostDir), hostagent.RequestMigratePlan+"-"+st.ID+".json")
	req, err := hostagent.ReadRequestFile(path)
	require.NoError(t, err, "the agent must accept what the API writes")
	require.Equal(t, e.owner.String(), req.RequestedBy)

	state, err := svc.System.GetMigrationState(ctx, e.owner)
	require.NoError(t, err)
	require.True(t, state.AgentReporting)
	require.Equal(t, "queued", state.Requests["plan"].State)
	require.Nil(t, state.Plan)

	// The agent runs it and leaves a result.
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.MkdirAll(hostagent.RequestsDir(hostDir), 0o755))
	done := time.Now().UTC()
	status, _ := json.Marshal(hostagent.RequestStatus{ID: st.ID, Type: hostagent.RequestMigratePlan, State: hostagent.RequestSucceeded, StartedAt: done, FinishedAt: &done})
	require.NoError(t, os.WriteFile(filepath.Join(hostagent.RequestsDir(hostDir), st.ID+".json"), status, 0o644))
	require.NoError(t, os.MkdirAll(hostagent.MigrateDir(hostDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.PlanFile), []byte(`{"summary":{"moves":4}}`), 0o600))

	state, err = svc.System.GetMigrationState(ctx, e.owner)
	require.NoError(t, err)
	require.Equal(t, "succeeded", state.Requests["plan"].State)
	require.JSONEq(t, `{"summary":{"moves":4}}`, string(state.Plan))
	require.NotNil(t, state.PlanAt)
}

func TestRequestMigrationRefusals(t *testing.T) {
	ctx := context.Background()

	e, svc, _ := newMigrationEnv(t, false)
	_, err := svc.System.RequestMigration(ctx, e.owner, "detect", nil)
	require.ErrorIs(t, err, service.ErrHostAgentNotReporting, "no agent would pick it up")

	e, svc, _ = newMigrationEnv(t, true)
	_, err = svc.System.RequestMigration(ctx, e.owner, "apply", nil)
	require.ErrorIs(t, err, service.ErrUnknownHostRequest)

	other := upgradeUser(t, e.db, "member", meshdb.UserHuman)
	upgradeMember(t, e.db, e.first, other, meshdb.RoleAdmin)
	_, err = svc.System.RequestMigration(ctx, other, "plan", nil)
	require.ErrorIs(t, err, service.ErrMigrationNotOwner)
	_, err = svc.System.GetMigrationState(ctx, other)
	require.ErrorIs(t, err, service.ErrMigrationNotOwner, "the plan names every app and domain")
}

// Stage 2 acts on one group, so the console has to say which. A move with no
// group would otherwise queue a request the agent cannot answer.
func TestAMoveMustNameItsGroup(t *testing.T) {
	ctx := context.Background()
	e, svc, hostDir := newMigrationEnv(t, true)

	_, err := svc.System.RequestMigration(ctx, e.owner, "move", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "which group")

	st, err := svc.System.RequestMigration(ctx, e.owner, "move", map[string]string{"group": "g-web"})
	require.NoError(t, err)
	assert.NotEmpty(t, st.ID)

	path := filepath.Join(hostagent.InboxDir(hostDir), hostagent.RequestMigrateMove+"-"+st.ID+".json")
	req, err := hostagent.ReadRequestFile(path)
	require.NoError(t, err)
	assert.Equal(t, "g-web", req.Args["group"])
}

// The console follows a migration through the summary the host writes from its
// journal - which the API cannot read itself, and has no business reading.
func TestMigrationStateCarriesTheHostsProgress(t *testing.T) {
	ctx := context.Background()
	e, svc, hostDir := newMigrationEnv(t, true)
	require.NoError(t, os.MkdirAll(hostagent.MigrateDir(hostDir), 0o755))

	moved := time.Now().UTC()
	status, err := json.Marshal(hostagent.MigrationStatus{
		UpdatedAt: moved, Prepared: true,
		Groups: []hostagent.GroupProgress{
			{ID: "g1", Name: "shop", Moved: true, MovedAt: &moved, CanMove: true},
			{ID: "g2", Name: "docs", CanMove: false, Blockers: []string{"its certificate was uploaded by hand"}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.StatusFile), status, 0o644))

	state, err := svc.System.GetMigrationState(ctx, e.owner)
	require.NoError(t, err)
	require.NotNil(t, state.Status)
	require.True(t, state.Status.Prepared)
	require.Len(t, state.Status.Groups, 2)
	assert.True(t, state.Status.Groups[0].Moved)
	assert.False(t, state.Status.Groups[1].CanMove)
	assert.Equal(t, "its certificate was uploaded by hand", state.Status.Groups[1].Blockers[0])
}

// A summary that cannot be read is not worth failing the page for: everything
// else the console shows is still true.
func TestAnUnreadableProgressSummaryDoesNotFailTheState(t *testing.T) {
	ctx := context.Background()
	e, svc, hostDir := newMigrationEnv(t, true)
	require.NoError(t, os.MkdirAll(hostagent.MigrateDir(hostDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.StatusFile),
		[]byte("{not json"), 0o644))

	state, err := svc.System.GetMigrationState(ctx, e.owner)
	require.NoError(t, err)
	assert.Nil(t, state.Status)
	assert.True(t, state.AgentReporting)
}

// Finishing is a stage like any other from here: queued for the agent, with
// what the operator chose about the old platform's volumes.
func TestFinishIsQueuedWithWhatWasChosenAboutVolumes(t *testing.T) {
	ctx := context.Background()
	e, svc, hostDir := newMigrationEnv(t, true)

	st, err := svc.System.RequestMigration(ctx, e.owner, "finish", map[string]string{"volumes": "true"})
	require.NoError(t, err)

	path := filepath.Join(hostagent.InboxDir(hostDir), hostagent.RequestMigrateFinish+"-"+st.ID+".json")
	req, err := hostagent.ReadRequestFile(path)
	require.NoError(t, err, "the agent must accept what the API writes")
	assert.Equal(t, "true", req.Args["volumes"])
}
