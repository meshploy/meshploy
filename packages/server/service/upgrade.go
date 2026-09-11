package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/license"
	"github.com/meshploy/packages/server/version"
	"gorm.io/gorm"
)

// Upgrading the server from the console.
//
// The API never upgrades anything itself. It has no Docker socket and no access
// to the host, by design: gaining either would make a compromise of the
// internet-facing API a compromise of the machine. Instead it queues a request
// as a file in inbox/, a systemd path unit on the gateway runs
// `meshploy updater run --queued` as root, and that runner reports its progress
// in state/. docker-compose mounts inbox/ read-write and state/ read-only, so
// the API can ask for an upgrade but never write anything the root runner reads
// back as its own.
//
// The file formats are shared with apps/cli/cmd/updater.go.

var (
	ErrNotInstanceOwner  = errors.New("only the owner of this server can upgrade it")
	ErrUpgradeNotEnabled = errors.New("upgrades from the console are not turned on for this server; on the gateway, run: sudo meshploy updater start")
	ErrUpgradeRunning    = errors.New("an upgrade is already queued or running")
	ErrUpgradeDevBuild   = errors.New("this is a development build, which has no release channel to upgrade on")
	ErrUnknownEdition    = errors.New("the edition must be enterprise")
	// ErrEditionSwitchRefused wraps the reason a switch to Enterprise is not
	// possible now.
	ErrEditionSwitchRefused = errors.New("cannot switch to Enterprise")
)

// UpgradeOptions is what the owner asked for. Both empty is an upgrade on the
// server's own channel and edition.
type UpgradeOptions struct {
	// Channel switches to stable or edge; empty stays on the server's own.
	Channel string
	// Edition "enterprise" switches a Community server to the Enterprise images
	// its licence grants; empty keeps the images it runs.
	Edition string
}

const (
	upgradeRequestFile = "request.json"
	upgradeStatusFile  = "status.json"
	upgradeLogFile     = "upgrade.log"
	upgradeEnabledFile = "enabled"

	// The runner refreshes its heartbeat every 10 seconds. A run that stopped
	// without finishing was interrupted, most likely by a reboot.
	upgradeStaleAfter   = 2 * time.Minute
	upgradeLogTailLines = 40
	maxUpgradeFileBytes = 64 << 10
)

// UpgradeStatus is what the console needs to offer an upgrade and follow one.
type UpgradeStatus struct {
	// Enabled reports whether the updater is on (`meshploy updater start`).
	// When it is off the console shows the commands to run instead of a button.
	Enabled bool `json:"enabled"`
	// CanUpgrade reports whether the current user may start one now: the
	// instance owner, on a server with the updater on, running a release or
	// edge build.
	CanUpgrade bool `json:"can_upgrade"`
	// Pending is true from the moment a request is queued until the runner
	// picks it up. PendingID is that request's id.
	Pending   bool   `json:"pending"`
	PendingID string `json:"pending_id,omitempty"`

	// The current or last run, as the runner reported it. ID is empty when
	// there has never been one. State is running, succeeded, failed or
	// interrupted.
	ID         string `json:"id"`
	State      string `json:"state"`
	Step       string `json:"step"`
	Channel    string `json:"channel"`
	CLIFrom    string `json:"cli_from"`
	CLITo      string `json:"cli_to"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Error      string `json:"error"`
	// Edition is "enterprise" when the run switches to the Enterprise images.
	Edition string `json:"edition,omitempty"`

	// LogTail is the end of the run's log. Only the instance owner gets it.
	LogTail []string `json:"log_tail"`
}

// runnerStatus is state/status.json as the runner writes it.
type runnerStatus struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	Step        string `json:"step"`
	Channel     string `json:"channel"`
	CLIFrom     string `json:"cli_from"`
	CLITo       string `json:"cli_to"`
	StartedAt   string `json:"started_at"`
	HeartbeatAt string `json:"heartbeat_at"`
	FinishedAt  string `json:"finished_at"`
	Error       string `json:"error"`
	Edition     string `json:"edition,omitempty"`
}

// upgradeRequest is inbox/request.json. The runner treats it as untrusted and
// acts only on the channel, which it checks is stable or edge, and on a switch
// to Enterprise, whose image it holds to the licence's registry scope rule.
type upgradeRequest struct {
	ID          string `json:"id"`
	Channel     string `json:"channel"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
	Edition     string `json:"edition,omitempty"`
	// Image is the Enterprise API image, taken from the verified licence and
	// never from the client.
	Image string `json:"image,omitempty"`
}

// upgradeChannel is the channel this build runs on, or "" for a local build.
// It comes from the running build, so an upgrade stays on it unless the owner
// asks for the other one, and that move is checked (checkChannelSwitch).
func upgradeChannel() string {
	switch version.Channel {
	case "stable", "edge":
		return version.Channel
	}
	return ""
}

// upgradePath joins parts onto the upgrade directory, or returns "" when none
// is configured.
func (s *SystemService) upgradePath(parts ...string) string {
	if s.cfg == nil || s.cfg.UpgradeDir == "" {
		return ""
	}
	return filepath.Join(append([]string{s.cfg.UpgradeDir}, parts...)...)
}

// GetUpgradeStatus reports whether this server can be upgraded from the
// console, and the state of the current or last upgrade.
//
// Readable by any member, so everyone signed in can see that an upgrade is
// under way. The log and the ability to start one are the instance owner's.
func (s *SystemService) GetUpgradeStatus(ctx context.Context, userID uuid.UUID) (UpgradeStatus, error) {
	return s.upgradeStatus(ctx, userID, time.Now())
}

func (s *SystemService) upgradeStatus(ctx context.Context, userID uuid.UUID, now time.Time) (UpgradeStatus, error) {
	out := UpgradeStatus{LogTail: []string{}}
	if s.upgradePath() == "" {
		return out, nil
	}

	owner, err := s.IsInstanceOwner(ctx, userID)
	if err != nil {
		return out, err
	}
	_, err = os.Stat(s.upgradePath("state", upgradeEnabledFile))
	out.Enabled = err == nil
	out.CanUpgrade = owner && out.Enabled && upgradeChannel() != ""

	if req, err := readUpgradeJSON[upgradeRequest](s.upgradePath("inbox", upgradeRequestFile)); err == nil && req != nil {
		out.Pending, out.PendingID = true, req.ID
	}

	st, err := readUpgradeJSON[runnerStatus](s.upgradePath("state", upgradeStatusFile))
	if err != nil {
		// A status the API cannot read must not stop the console from offering
		// the next upgrade, so it is logged and treated as no run.
		log.Printf("upgrade: read %s: %v", upgradeStatusFile, err)
	} else if st != nil {
		out.ID, out.Step, out.Channel = st.ID, st.Step, st.Channel
		out.CLIFrom, out.CLITo = st.CLIFrom, st.CLITo
		out.StartedAt, out.FinishedAt, out.Error = st.StartedAt, st.FinishedAt, st.Error
		out.Edition = st.Edition
		out.State = effectiveUpgradeState(*st, now)
	}

	if owner {
		out.LogTail = tailFileLines(s.upgradePath("state", upgradeLogFile), upgradeLogTailLines)
	}
	return out, nil
}

// RequestUpgrade queues an upgrade to the latest build on the channel asked
// for, or on this server's own channel. Moving to the other channel is only
// allowed forward (see channelSwitch), and to Enterprise only with an active
// licence. It returns the status with the new request's id as PendingID.
func (s *SystemService) RequestUpgrade(ctx context.Context, userID uuid.UUID, opts UpgradeOptions) (UpgradeStatus, error) {
	owner, err := s.IsInstanceOwner(ctx, userID)
	if err != nil {
		return UpgradeStatus{}, err
	}
	if !owner {
		return UpgradeStatus{}, ErrNotInstanceOwner
	}
	current := upgradeChannel()
	if current == "" {
		return UpgradeStatus{}, ErrUpgradeDevBuild
	}
	channel := opts.Channel
	if channel == "" {
		channel = current
	}
	// Before the lock: it may ask GitHub, which can take seconds.
	if err := s.checkChannelSwitch(ctx, channel); err != nil {
		return UpgradeStatus{}, err
	}
	var image string
	switch opts.Edition {
	case "":
	case version.EditionEnterprise:
		if image, err = s.enterpriseImage(ctx); err != nil {
			return UpgradeStatus{}, err
		}
	default:
		return UpgradeStatus{}, ErrUnknownEdition
	}

	s.upgradeMu.Lock()
	defer s.upgradeMu.Unlock()

	st, err := s.GetUpgradeStatus(ctx, userID)
	if err != nil {
		return st, err
	}
	if !st.Enabled {
		return st, ErrUpgradeNotEnabled
	}
	if st.Pending || st.State == "running" {
		return st, ErrUpgradeRunning
	}

	req := upgradeRequest{
		ID:          uuid.NewString(),
		Channel:     channel,
		RequestedBy: userID.String(),
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
		Edition:     opts.Edition,
		Image:       image,
	}
	if err := writeUpgradeRequest(s.upgradePath("inbox"), req); err != nil {
		return st, fmt.Errorf("queue the upgrade: %w", err)
	}
	switch {
	case image != "":
		log.Printf("upgrade: user %s queued a switch to the Enterprise image %s on the %s channel (request %s)", userID, image, channel, req.ID)
	case channel != current:
		log.Printf("upgrade: user %s queued a switch from %s to %s (request %s)", userID, current, channel, req.ID)
	default:
		log.Printf("upgrade: user %s queued an upgrade on the %s channel (request %s)", userID, channel, req.ID)
	}
	st.Pending, st.PendingID = true, req.ID
	return st, nil
}

// enterpriseImage returns the API image a switch to Enterprise installs: the one
// the active licence names, or the default Enterprise image when it names none.
// The client never supplies it, and it is checked again by the runner.
//
// The switch needs the licence in force (verified, unexpired, bound to this
// domain) so the Enterprise server comes up entitled, and it is refused on a
// server already running the Enterprise images.
func (s *SystemService) enterpriseImage(ctx context.Context) (string, error) {
	if version.Edition == version.EditionEnterprise {
		return "", fmt.Errorf("%w: this server already runs the Enterprise images", ErrEditionSwitchRefused)
	}
	if s.ent == nil {
		return "", fmt.Errorf("%w: this server cannot read its licence", ErrEditionSwitchRefused)
	}
	st, err := s.ent.Describe(ctx, uuid.Nil)
	if err != nil {
		return "", err
	}
	switch {
	case !st.Licensed && st.Problem != "":
		return "", fmt.Errorf("%w: the licence is not active (%s)", ErrEditionSwitchRefused, st.Problem)
	case !st.Licensed:
		return "", fmt.Errorf("%w: activate a licence first", ErrEditionSwitchRefused)
	case st.Problem != "":
		return "", fmt.Errorf("%w: %s", ErrEditionSwitchRefused, st.Problem)
	}
	image := st.RegistryScope
	if image == "" {
		image = license.DefaultRegistryScope
	}
	if err := license.ValidRegistryScope(image); err != nil {
		return "", fmt.Errorf("%w: %v", ErrEditionSwitchRefused, err)
	}
	return image, nil
}

// IsInstanceOwner reports whether userID owns this Meshploy server: the owner
// of its first organization, the one created with the setup token.
//
// There is no separate instance-admin role. The owner of an organization
// created later owns that organization, not the machine it runs on, so it does
// not qualify. Nor does an agent: upgrading the server is not something to
// delegate to a machine principal.
func (s *SystemService) IsInstanceOwner(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	db := s.db.WithContext(ctx)

	var user meshdb.User
	if err := db.Select("id", "kind").First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if user.Kind == meshdb.UserAgent {
		return false, nil
	}

	var first meshdb.Organization
	if err := db.Select("id").Order("created_at ASC, id ASC").First(&first).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	var n int64
	err := db.Model(&meshdb.OrganizationMember{}).
		Where("organization_id = ? AND user_id = ? AND role = ?", first.ID, userID, meshdb.RoleOwner).
		Count(&n).Error
	return n > 0, err
}

// effectiveUpgradeState reads a stored status the way the console should: a run
// that stopped heartbeating without finishing was interrupted, and must not be
// reported as running forever.
func effectiveUpgradeState(st runnerStatus, now time.Time) string {
	if st.State != "running" {
		return st.State
	}
	hb, err := time.Parse(time.RFC3339, st.HeartbeatAt)
	if err != nil || now.Sub(hb) > upgradeStaleAfter {
		return "interrupted"
	}
	return st.State
}

// writeUpgradeRequest writes the request under a temporary name and renames it
// into place: the path unit watches for request.json, and must never start the
// runner on half a file.
func writeUpgradeRequest(inbox string, req upgradeRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(inbox, ".request-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(inbox, upgradeRequestFile))
}

// readUpgradeJSON reads one of the shared files. A missing file is nil, nil.
func readUpgradeJSON[T any](path string) (*T, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var v T
	if err := json.NewDecoder(io.LimitReader(f, maxUpgradeFileBytes)).Decode(&v); err != nil {
		return nil, err
	}
	return &v, nil
}

// tailFileLines returns up to n trailing lines of a file, reading only its end.
func tailFileLines(path string, n int) []string {
	out := []string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxUpgradeFileBytes {
		_, _ = f.Seek(-maxUpgradeFileBytes, io.SeekEnd)
	}
	data, _ := io.ReadAll(f)
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return out
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return append(out, lines...)
}
