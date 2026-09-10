package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/meshploy/packages/server/version"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// upgradeEnv is a server with an instance owner, and an upgrade directory laid
// out the way `meshploy updater start` leaves it.
type upgradeEnv struct {
	db    *gorm.DB
	svc   *service.Services
	dir   string
	owner uuid.UUID
	first uuid.UUID // the first organization's id
}

func newUpgradeEnv(t *testing.T, channel string, enabled bool) upgradeEnv {
	t.Helper()
	orig := version.Channel
	version.Channel = channel
	t.Cleanup(func() { version.Channel = orig })

	db := newTestDB(t)
	dir := t.TempDir()
	for _, d := range []string{"inbox", "state"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0o755))
	}
	if enabled {
		writeUpgradeFile(t, filepath.Join(dir, "state", "enabled"), "2026-09-11T00:00:00Z\n")
	}

	owner := upgradeUser(t, db, "owner", meshdb.UserHuman)
	first := upgradeOrg(t, db, "first", time.Now().Add(-time.Hour))
	upgradeMember(t, db, first, owner, meshdb.RoleOwner)

	return upgradeEnv{
		db:    db,
		svc:   service.New(db, &config.Config{UpgradeDir: dir}),
		dir:   dir,
		owner: owner,
		first: first,
	}
}

func upgradeUser(t *testing.T, db *gorm.DB, name string, kind meshdb.UserType) uuid.UUID {
	t.Helper()
	email := name + "@example.com"
	if kind == meshdb.UserAgent {
		email = "" // agents carry no email
	}
	u := meshdb.User{Username: name, Email: email, Kind: kind}
	require.NoError(t, db.Create(&u).Error)
	return u.ID
}

func upgradeOrg(t *testing.T, db *gorm.DB, slug string, created time.Time) uuid.UUID {
	t.Helper()
	o := meshdb.Organization{Base: meshdb.Base{CreatedAt: created}, Name: slug, Slug: slug}
	require.NoError(t, db.Create(&o).Error)
	return o.ID
}

func upgradeMember(t *testing.T, db *gorm.DB, org, user uuid.UUID, role meshdb.MemberRole) {
	t.Helper()
	require.NoError(t, db.Create(&meshdb.OrganizationMember{OrganizationID: org, UserID: user, Role: role}).Error)
}

func writeUpgradeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func (e upgradeEnv) writeRun(t *testing.T, state string, heartbeat time.Time) {
	t.Helper()
	st, err := json.Marshal(map[string]string{
		"id": "run-1", "state": state, "step": "Upgrading the server", "channel": "edge",
		"cli_from": "0.10.0", "cli_to": "0.11.0", "started_at": "2026-09-11T00:00:00Z",
		"heartbeat_at": heartbeat.UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	writeUpgradeFile(t, filepath.Join(e.dir, "state", "status.json"), string(st))
	writeUpgradeFile(t, filepath.Join(e.dir, "state", "upgrade.log"), "▸ Updating the CLI\n▸ Upgrading the server\n")
}

func TestUpgradeStatusWithoutTheUpdater(t *testing.T) {
	e := newUpgradeEnv(t, "stable", false)
	st, err := e.svc.System.GetUpgradeStatus(context.Background(), e.owner)
	require.NoError(t, err)
	require.False(t, st.Enabled)
	require.False(t, st.CanUpgrade, "the button must not show before the updater is on")
	require.NotNil(t, st.LogTail)
}

// A local API has no upgrade directory at all; that is "not set up", not an
// error.
func TestUpgradeStatusWithNoUpgradeDirectory(t *testing.T) {
	svc := service.New(newTestDB(t), &config.Config{UpgradeDir: filepath.Join(t.TempDir(), "missing")})
	st, err := svc.System.GetUpgradeStatus(context.Background(), uuid.New())
	require.NoError(t, err)
	require.False(t, st.Enabled)
}

func TestUpgradeStatusForTheOwner(t *testing.T) {
	e := newUpgradeEnv(t, "edge", true)
	e.writeRun(t, "running", time.Now())

	st, err := e.svc.System.GetUpgradeStatus(context.Background(), e.owner)
	require.NoError(t, err)
	require.True(t, st.Enabled)
	require.True(t, st.CanUpgrade)
	require.Equal(t, "running", st.State)
	require.Equal(t, "run-1", st.ID)
	require.Equal(t, "0.11.0", st.CLITo)
	require.Equal(t, []string{"▸ Updating the CLI", "▸ Upgrading the server"}, st.LogTail)
}

// Every member can see that an upgrade is under way, but the log and the
// button are the owner's.
func TestUpgradeStatusForAnotherMember(t *testing.T) {
	e := newUpgradeEnv(t, "edge", true)
	e.writeRun(t, "running", time.Now())
	admin := upgradeUser(t, e.db, "admin", meshdb.UserHuman)
	upgradeMember(t, e.db, e.first, admin, meshdb.RoleAdmin)

	st, err := e.svc.System.GetUpgradeStatus(context.Background(), admin)
	require.NoError(t, err)
	require.Equal(t, "running", st.State)
	require.False(t, st.CanUpgrade)
	require.Empty(t, st.LogTail)
}

// A run that stopped heartbeating, most likely because the host rebooted, must
// not read as running forever: that would block every later upgrade.
func TestUpgradeStatusReportsAnInterruptedRun(t *testing.T) {
	e := newUpgradeEnv(t, "edge", true)
	e.writeRun(t, "running", time.Now().Add(-10*time.Minute))

	st, err := e.svc.System.GetUpgradeStatus(context.Background(), e.owner)
	require.NoError(t, err)
	require.Equal(t, "interrupted", st.State)

	_, err = e.svc.System.RequestUpgrade(context.Background(), e.owner)
	require.NoError(t, err, "an interrupted run must not block the next upgrade")
}

func TestUpgradeStatusSurvivesAnUnreadableStatusFile(t *testing.T) {
	e := newUpgradeEnv(t, "edge", true)
	writeUpgradeFile(t, filepath.Join(e.dir, "state", "status.json"), "{not json")

	st, err := e.svc.System.GetUpgradeStatus(context.Background(), e.owner)
	require.NoError(t, err)
	require.Empty(t, st.State)
	require.True(t, st.CanUpgrade)
}

// The channel is the running build's, whatever the client wanted.
func TestRequestUpgradeQueuesTheServersChannel(t *testing.T) {
	e := newUpgradeEnv(t, "stable", true)

	st, err := e.svc.System.RequestUpgrade(context.Background(), e.owner)
	require.NoError(t, err)
	require.True(t, st.Pending)
	require.NotEmpty(t, st.PendingID)

	data, err := os.ReadFile(filepath.Join(e.dir, "inbox", "request.json"))
	require.NoError(t, err)
	var req map[string]string
	require.NoError(t, json.Unmarshal(data, &req))
	require.Equal(t, "stable", req["channel"])
	require.Equal(t, st.PendingID, req["id"])
	require.Equal(t, e.owner.String(), req["requested_by"])

	entries, err := os.ReadDir(filepath.Join(e.dir, "inbox"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "a temporary file was left in the inbox")

	again, err := e.svc.System.GetUpgradeStatus(context.Background(), e.owner)
	require.NoError(t, err)
	require.True(t, again.Pending)
	require.Equal(t, st.PendingID, again.PendingID)
}

func TestRequestUpgradeRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("not the instance owner", func(t *testing.T) {
		e := newUpgradeEnv(t, "stable", true)
		later := upgradeUser(t, e.db, "later", meshdb.UserHuman)
		upgradeMember(t, e.db, upgradeOrg(t, e.db, "later", time.Now()), later, meshdb.RoleOwner)
		_, err := e.svc.System.RequestUpgrade(ctx, later)
		require.ErrorIs(t, err, service.ErrNotInstanceOwner)
	})

	t.Run("updater off", func(t *testing.T) {
		e := newUpgradeEnv(t, "stable", false)
		_, err := e.svc.System.RequestUpgrade(ctx, e.owner)
		require.ErrorIs(t, err, service.ErrUpgradeNotEnabled)
	})

	t.Run("development build", func(t *testing.T) {
		e := newUpgradeEnv(t, "dev", true)
		_, err := e.svc.System.RequestUpgrade(ctx, e.owner)
		require.ErrorIs(t, err, service.ErrUpgradeDevBuild)
	})

	t.Run("already queued", func(t *testing.T) {
		e := newUpgradeEnv(t, "stable", true)
		_, err := e.svc.System.RequestUpgrade(ctx, e.owner)
		require.NoError(t, err)
		_, err = e.svc.System.RequestUpgrade(ctx, e.owner)
		require.ErrorIs(t, err, service.ErrUpgradeRunning)
	})

	t.Run("already running", func(t *testing.T) {
		e := newUpgradeEnv(t, "stable", true)
		e.writeRun(t, "running", time.Now())
		_, err := e.svc.System.RequestUpgrade(ctx, e.owner)
		require.ErrorIs(t, err, service.ErrUpgradeRunning)
		_, statErr := os.Stat(filepath.Join(e.dir, "inbox", "request.json"))
		require.True(t, os.IsNotExist(statErr), "a refused request must not reach the inbox")
	})
}

// The owner of the first organization owns the server. An owner of a later
// organization owns only that, and an agent never qualifies, even as owner.
func TestIsInstanceOwner(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := service.New(db, &config.Config{})

	agent := upgradeUser(t, db, "agent", meshdb.UserAgent)
	upgradeMember(t, db, upgradeOrg(t, db, "first", time.Now().Add(-time.Hour)), agent, meshdb.RoleOwner)
	human := upgradeUser(t, db, "human", meshdb.UserHuman)
	upgradeMember(t, db, upgradeOrg(t, db, "second", time.Now()), human, meshdb.RoleOwner)

	for name, id := range map[string]uuid.UUID{"agent owning the first org": agent, "owner of a later org": human, "unknown user": uuid.New()} {
		ok, err := svc.System.IsInstanceOwner(ctx, id)
		require.NoError(t, err, name)
		require.False(t, ok, name)
	}
}
