package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// The updater lets the console upgrade this server without the API gaining any
// privilege on the host. The API only drops a request into inbox/; a systemd
// path unit notices it and runs `meshploy updater run --queued` as root, which
// does what an operator would: `update`, then `server-upgrade` with the new
// binary, then waits for the API to come back healthy.
//
// inbox/ is mounted into the API container read-write and state/ read-only.
// The API runs as root in its container, so it could plant a symlink in any
// directory it can write. Keeping everything the root runner writes out of
// inbox/ is what stops that from becoming a way to overwrite host files.
var (
	upgradeDir     = "/var/lib/meshploy/upgrade"
	systemdUnitDir = "/etc/systemd/system"
	updaterCLIPath = "/usr/local/bin/meshploy"
)

const (
	upgradePathUnit    = "meshploy-upgrade.path"
	upgradeServiceUnit = "meshploy-upgrade.service"

	upgradeRequestFile = "request.json"
	upgradeStatusFile  = "status.json"
	upgradeLogFile     = "upgrade.log"
	upgradeEnabledFile = "enabled"

	maxUpgradeRequestBytes = 4 << 10
	maxUpgradeLogBytes     = 5 << 20
	upgradeLogTailLines    = 20

	// A run whose heartbeat is older than this, with no service active, was
	// interrupted: most likely the host rebooted mid-upgrade.
	upgradeStaleAfter = 2 * time.Minute

	channelStable = "stable"
	channelEdge   = "edge"
)

// Seams replaced by tests.
var (
	// upgradeStep runs one step of an upgrade as a child process. A child
	// rather than a function call, because after `update` the code that must
	// do the rest is the new binary on disk, not this process.
	upgradeStep = func(ctx context.Context, w io.Writer, exe string, args ...string) error {
		c := exec.CommandContext(ctx, exe, args...)
		c.Stdout, c.Stderr = w, w
		return c.Run()
	}
	// upgradeCLIVersion reports the version of the binary at exe.
	upgradeCLIVersion = func(exe string) (string, error) {
		out, err := exec.Command(exe, "version").Output()
		if err != nil {
			return "", err
		}
		f := strings.Fields(string(out))
		if len(f) < 2 {
			return "", fmt.Errorf("unexpected version output %q", out)
		}
		return f[1], nil
	}
	upgradeHealthURL      = localAPI + "/health"
	upgradeHealthTimeout  = 3 * time.Minute
	upgradeHealthInterval = 3 * time.Second
	upgradeHeartbeat      = 10 * time.Second

	systemctl = func(args ...string) error {
		return sysCmd("systemctl", args...)
	}
	// systemctlState returns what `systemctl is-active` or `is-enabled`
	// prints. Both exit non-zero for anything but the positive answer, so
	// the output is the result and the exit code is ignored.
	systemctlState = func(verb, unit string) string {
		out, _ := exec.Command("systemctl", verb, unit).Output()
		return strings.TrimSpace(string(out))
	}
)

// upgradeRequest is what the API writes into inbox/. Everything in it is
// untrusted: the channel is checked against the two known values, and the
// other fields are only ever recorded.
type upgradeRequest struct {
	ID          string `json:"id"`
	Channel     string `json:"channel"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
}

// upgradeStatus is state/status.json, which the API and `updater status` read.
type upgradeStatus struct {
	ID          string `json:"id"`
	State       string `json:"state"` // running, succeeded, failed
	Step        string `json:"step"`
	Channel     string `json:"channel"`
	RequestedBy string `json:"requested_by"`
	CLIFrom     string `json:"cli_from"`
	CLITo       string `json:"cli_to"`
	StartedAt   string `json:"started_at"`
	HeartbeatAt string `json:"heartbeat_at"`
	FinishedAt  string `json:"finished_at"`
	Error       string `json:"error"`
}

func upgradeInboxDir() string { return filepath.Join(upgradeDir, "inbox") }
func upgradeStateDir() string { return filepath.Join(upgradeDir, "state") }

var updaterCmd = &cobra.Command{
	Use:   "updater",
	Short: "Let the console upgrade this server",
	Long: `The updater upgrades a Meshploy gateway on request from the console.

The API cannot touch the host, so it only queues a request. A systemd path unit
on this machine notices it and runs 'meshploy updater run' as root, which
updates the CLI, runs server-upgrade with the new binary on the server's current
channel, and waits for the API to come back healthy.

  start    install the systemd units and start watching for requests
  stop     stop watching; an upgrade already running finishes
  status   whether the watcher is on, and the last run with its log
  run      upgrade now, the same way the console would; usable by hand`,
}

var updaterStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Install the upgrade service and start watching for console requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("updater start requires root; re-run with sudo")
		}
		return updaterStart(cmd.OutOrStdout())
	},
}

var updaterStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop watching for console requests; a running upgrade finishes",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("updater stop requires root; re-run with sudo")
		}
		return updaterStop(cmd.OutOrStdout())
	},
}

var updaterStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the updater is on, and the last upgrade with its log",
	RunE: func(cmd *cobra.Command, args []string) error {
		printUpdaterStatus(cmd.OutOrStdout(), collectUpdaterInfo(), time.Now())
		return nil
	},
}

var updaterRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Upgrade this server now: update the CLI, run server-upgrade, check health",
	Long: `Upgrade this server now, the way the console does: update the CLI, run
server-upgrade with the new binary, then wait for the API to answer healthy.
Progress goes to /var/lib/meshploy/upgrade/state, where 'meshploy updater status'
and the console read it.

It stays on the channel the server is on now (MESHPLOY_CHANNEL in
/opt/meshploy/.env). Pass --edge or --stable to switch.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("updater run requires root; re-run with sudo")
		}
		edge, _ := cmd.Flags().GetBool("edge")
		stable, _ := cmd.Flags().GetBool("stable")
		queued, _ := cmd.Flags().GetBool("queued")
		opts := upgradeOptions{queued: queued}
		switch {
		case edge:
			opts.channel = channelEdge
		case stable:
			opts.channel = channelStable
		}
		return runUpgrade(cmd.Context(), cmd.OutOrStdout(), opts)
	},
}

// ── start / stop ─────────────────────────────────────────────────────────────

// upgradeUnitFiles renders the two systemd units, keyed by file name.
func upgradeUnitFiles(cliPath string) map[string]string {
	return map[string]string{
		upgradePathUnit: fmt.Sprintf(`[Unit]
Description=Watch for Meshploy upgrade requests from the console

[Path]
PathExists=%s
Unit=%s

[Install]
WantedBy=multi-user.target
`, filepath.Join(upgradeInboxDir(), upgradeRequestFile), upgradeServiceUnit),

		upgradeServiceUnit: fmt.Sprintf(`[Unit]
Description=Upgrade Meshploy (requested from the console)
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s updater run --queued
# docker compose reads registry credentials from root's home directory, and
# systemd does not always set HOME for a system service.
Environment=HOME=/root
# Optional; only a private fork needs it, for GITHUB_PAT.
EnvironmentFile=-/etc/meshploy/updater.env
# oneshot units have no start timeout by default, and a hung image pull
# would otherwise hold the upgrade lock forever.
TimeoutStartSec=30min
`, cliPath),
	}
}

// updaterStart installs the units and turns the watcher on. Running it again
// rewrites the units, which is how they follow the CLI version.
func updaterStart(out io.Writer) error {
	if _, err := os.Stat(filepath.Join(meshployInstDir, ".env")); err != nil {
		return fmt.Errorf("no Meshploy server found at %s: the updater runs on the gateway", meshployInstDir)
	}
	if _, err := os.Stat(updaterCLIPath); err != nil {
		return fmt.Errorf("the upgrade service runs %s, which is missing: %w", updaterCLIPath, err)
	}
	if err := ensureUpgradeDirs(); err != nil {
		return err
	}
	if err := writeUpgradeUnits(); err != nil {
		return err
	}
	if err := systemctl("enable", "--now", upgradePathUnit); err != nil {
		return fmt.Errorf("enable %s: %w", upgradePathUnit, err)
	}
	// Written last: it tells the console the button will work, so it must
	// not exist unless the watcher really is running.
	marker := filepath.Join(upgradeStateDir(), upgradeEnabledFile)
	if err := writeFileAtomic(marker, []byte(nowRFC3339()+"\n"), 0644); err != nil {
		return err
	}
	fmt.Fprintln(out, "✔  Updater on: upgrade requests from the console will run on this server.")
	fmt.Fprintln(out, "   Check on it with:  meshploy updater status")
	fmt.Fprintln(out, "   Turn it off with:  sudo meshploy updater stop")
	return nil
}

// ensureUpgradeDirs creates the folders docker-compose mounts into the API.
// Podman refuses to start a container whose bind-mount source is missing, so
// they must exist before the stack starts, whether or not the updater is on.
func ensureUpgradeDirs() error {
	for _, dir := range []string{upgradeInboxDir(), upgradeStateDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// writeUpgradeUnits writes both units and has systemd reload them.
func writeUpgradeUnits() error {
	for name, body := range upgradeUnitFiles(updaterCLIPath) {
		if err := writeFileAtomic(filepath.Join(systemdUnitDir, name), []byte(body), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}

// refreshUpgradeUnits rewrites the units when the updater is on, so they
// follow the CLI installed now. server-upgrade calls it; with the updater off
// it does nothing.
func refreshUpgradeUnits() error {
	if !fileExists(filepath.Join(upgradeStateDir(), upgradeEnabledFile)) {
		return nil
	}
	return writeUpgradeUnits()
}

// updaterStop turns the watcher off. It never stops the service itself:
// interrupting server-upgrade halfway is worse than letting it finish.
func updaterStop(out io.Writer) error {
	installed := fileExists(filepath.Join(systemdUnitDir, upgradePathUnit))
	if installed {
		if err := systemctl("disable", "--now", upgradePathUnit); err != nil {
			return fmt.Errorf("disable %s: %w", upgradePathUnit, err)
		}
	}
	_ = os.Remove(filepath.Join(upgradeStateDir(), upgradeEnabledFile))

	// A request left in the inbox would run the moment the watcher came back.
	pending := filepath.Join(upgradeInboxDir(), upgradeRequestFile)
	if _, err := os.Lstat(pending); err == nil {
		if err := os.RemoveAll(pending); err == nil {
			fmt.Fprintln(out, "Discarded an upgrade request that was waiting to run.")
		}
	}

	if !installed {
		fmt.Fprintln(out, "The updater is not installed on this server.")
		return nil
	}
	fmt.Fprintln(out, "✔  Updater off: the console can no longer start an upgrade here.")
	if systemctlState("is-active", upgradeServiceUnit) == "activating" {
		fmt.Fprintln(out, "   An upgrade is running now and will finish. Follow it with:  meshploy updater status")
	}
	return nil
}

// ── run ──────────────────────────────────────────────────────────────────────

type upgradeOptions struct {
	// queued means act only on a request the console left in the inbox. The
	// service passes it, so a start with nothing queued does nothing rather
	// than upgrading without being asked.
	queued bool
	// channel is for runs by hand: stable, edge, or "" for the server's
	// current channel.
	channel string
}

func runUpgrade(ctx context.Context, out io.Writer, opts upgradeOptions) error {
	if err := os.MkdirAll(upgradeStateDir(), 0755); err != nil {
		return err
	}
	unlock, locked, err := lockUpgrade(filepath.Join(upgradeDir, "lock"))
	if err != nil {
		return err
	}
	if !locked {
		fmt.Fprintln(out, "Another upgrade is already running; leaving it to finish.")
		return nil
	}
	defer unlock()

	req, reqErr := consumeUpgradeRequest(upgradeInboxDir())
	if opts.queued {
		if req == nil && reqErr == nil {
			fmt.Fprintln(out, "No upgrade request is queued.")
			return nil
		}
	} else if req != nil || reqErr != nil {
		fmt.Fprintln(out, "Discarding the upgrade request the console queued; this run covers it.")
		req, reqErr = nil, nil
	}

	logFile, err := openUpgradeLog(filepath.Join(upgradeStateDir(), upgradeLogFile))
	if err != nil {
		return err
	}
	defer logFile.Close()

	now := nowRFC3339()
	run := &upgradeRun{
		stateDir: upgradeStateDir(),
		log:      io.MultiWriter(bestEffortWriter{out}, &cappedWriter{w: logFile, left: maxUpgradeLogBytes}),
		st:       upgradeStatus{State: "running", CLIFrom: Version, StartedAt: now},
	}

	switch {
	case reqErr != nil:
		run.update(nil)
		return run.end(fmt.Errorf("rejected the queued request: %w", reqErr))
	case req != nil:
		run.st.ID, run.st.Channel, run.st.RequestedBy = req.ID, req.Channel, req.RequestedBy
	default:
		run.st.ID = newUpgradeID()
		run.st.Channel = opts.channel
		if run.st.Channel == "" {
			run.st.Channel = currentUpgradeChannel()
		}
		run.st.RequestedBy = "cli"
		if u := os.Getenv("SUDO_USER"); u != "" {
			run.st.RequestedBy = "cli:" + u
		}
	}
	run.update(nil)
	run.startHeartbeat(upgradeHeartbeat)
	// A switch changes the version as well as the channel; saying so up front
	// is what makes the rest of the log read right.
	if from := currentUpgradeChannel(); from != run.st.Channel {
		fmt.Fprintf(run.log, "Switching Meshploy from the %s channel to %s (requested by %s)\n", from, run.st.Channel, run.st.RequestedBy)
	} else {
		fmt.Fprintf(run.log, "Upgrading Meshploy on the %s channel (requested by %s)\n", run.st.Channel, run.st.RequestedBy)
	}

	exe, err := os.Executable()
	if err != nil {
		return run.end(fmt.Errorf("find this binary: %w", err))
	}
	withChannel := func(args ...string) []string {
		if run.st.Channel == channelEdge {
			return append(args, "--edge")
		}
		return args
	}

	run.step("Updating the CLI")
	if err := upgradeStep(ctx, run.log, exe, withChannel("update")...); err != nil {
		return run.end(fmt.Errorf("meshploy update: %w", err))
	}
	if v, err := upgradeCLIVersion(exe); err == nil {
		run.update(func(s *upgradeStatus) { s.CLITo = v })
	} else {
		fmt.Fprintf(run.log, "Could not read the new CLI version: %v\n", err)
	}

	run.step("Upgrading the server")
	if err := upgradeStep(ctx, run.log, exe, withChannel("server-upgrade")...); err != nil {
		return run.end(fmt.Errorf("meshploy server-upgrade: %w", err))
	}

	run.step("Waiting for the API")
	if err := waitForHealthyAPI(ctx, upgradeHealthURL, upgradeHealthTimeout); err != nil {
		return run.end(err)
	}
	return run.end(nil)
}

// upgradeRun owns status.json for the length of one run.
type upgradeRun struct {
	stateDir string
	log      io.Writer

	mu       sync.Mutex
	st       upgradeStatus
	stopBeat func()
}

func (r *upgradeRun) update(fn func(*upgradeStatus)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if fn != nil {
		fn(&r.st)
	}
	r.st.HeartbeatAt = nowRFC3339()
	if err := writeUpgradeStatus(r.stateDir, r.st); err != nil {
		fmt.Fprintf(r.log, "warning: could not write %s: %v\n", upgradeStatusFile, err)
	}
}

func (r *upgradeRun) step(name string) {
	r.update(func(s *upgradeStatus) { s.Step = name })
	fmt.Fprintf(r.log, "\n▸ %s\n", name)
}

// startHeartbeat refreshes heartbeat_at until the run ends, so a reader can
// tell a long step from a run that died with the host.
func (r *upgradeRun) startHeartbeat(every time.Duration) {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				r.update(nil)
			}
		}
	}()
	var once sync.Once
	r.stopBeat = func() {
		once.Do(func() { close(done); <-exited })
	}
}

// end records the outcome and returns err, so callers can `return run.end(err)`.
func (r *upgradeRun) end(err error) error {
	if r.stopBeat != nil {
		r.stopBeat()
	}
	r.update(func(s *upgradeStatus) {
		s.FinishedAt = nowRFC3339()
		if err != nil {
			s.State, s.Error = "failed", err.Error()
		} else {
			s.State = "succeeded"
		}
	})
	if err != nil {
		fmt.Fprintf(r.log, "\n✘  Upgrade failed: %v\n", err)
	} else {
		fmt.Fprintln(r.log, "\n✔  Upgrade finished")
	}
	return err
}

// consumeUpgradeRequest reads and removes the request the API queued. It
// returns nil, nil when there is none.
func consumeUpgradeRequest(inboxDir string) (*upgradeRequest, error) {
	path := filepath.Join(inboxDir, upgradeRequestFile)
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Consumed whatever happens next: the path unit keeps starting the service
	// while the file exists, so even a request that fails validation has to
	// go. RemoveAll never follows a symlink; a planted link is removed, not
	// its target.
	defer os.RemoveAll(path)

	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", upgradeRequestFile)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", upgradeRequestFile, err)
	}
	defer f.Close()
	// Checked again on the open descriptor, in case the file was swapped
	// between the Lstat and the open.
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", upgradeRequestFile)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxUpgradeRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", upgradeRequestFile, err)
	}
	if len(data) > maxUpgradeRequestBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", upgradeRequestFile, maxUpgradeRequestBytes)
	}
	var req upgradeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse %s: %w", upgradeRequestFile, err)
	}
	if req.Channel != channelStable && req.Channel != channelEdge {
		return nil, fmt.Errorf("unknown channel %q", clip(req.Channel))
	}
	req.ID, req.RequestedBy = clip(req.ID), clip(req.RequestedBy)
	return &req, nil
}

// clip keeps an untrusted field short and free of anything a terminal would
// interpret, since it ends up in the log and in `updater status`.
func clip(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 64 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("-_.:@+", r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lockUpgrade takes the lock that keeps two runs from overlapping. ok is false
// when another run holds it. Closing the descriptor releases it, so a crashed
// run never leaves it held.
func lockUpgrade(path string) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, false, fmt.Errorf("open upgrade lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("take upgrade lock: %w", err)
	}
	return func() { f.Close() }, true, nil
}

// openUpgradeLog starts the log afresh for a run. It is world-readable so
// `updater status` works without root.
func openUpgradeLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", upgradeLogFile, err)
	}
	if err := f.Chmod(0644); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func writeUpgradeStatus(stateDir string, st upgradeStatus) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(stateDir, upgradeStatusFile), append(data, '\n'), 0644)
}

func readUpgradeStatus(stateDir string) (*upgradeStatus, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, upgradeStatusFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st upgradeStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", upgradeStatusFile, err)
	}
	return &st, nil
}

// writeFileAtomic replaces path in one rename, so a reader never sees half a
// file. The temporary file is created exclusively and the rename replaces a
// symlink rather than following it.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// waitForHealthyAPI polls /health until it reports ok or timeout passes. The
// API is recreated by server-upgrade, so connection errors are expected at
// first and only the last one is reported.
func waitForHealthyAPI(ctx context.Context, url string, timeout time.Duration) error {
	c := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	for {
		why := apiHealth(ctx, c, url)
		if why == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the API was not healthy %s after the upgrade (last check: %s)", timeout, why)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(upgradeHealthInterval):
		}
	}
}

// apiHealth returns "" when the API reports ok, and otherwise why not.
func apiHealth(ctx context.Context, c *http.Client, url string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err.Error()
	}
	resp, err := c.Do(req)
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	var h struct {
		Status string `json:"status"`
		DB     string `json:"db"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h); err != nil {
		return "unreadable health response"
	}
	if h.Status != "ok" {
		return fmt.Sprintf("status %s, database: %s", h.Status, h.DB)
	}
	return ""
}

// currentUpgradeChannel is the channel the server is on now. main is what
// server-upgrade --edge writes; anything else is stable.
func currentUpgradeChannel() string {
	if readEnvVar("MESHPLOY_CHANNEL") == "main" {
		return channelEdge
	}
	return channelStable
}

func newUpgradeID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "cli-" + hex.EncodeToString(b)
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// cappedWriter passes writes through until left runs out, then drops the rest
// with one note. It never reports an error or a short write: the child
// process whose output it carries must not fail because its log did.
type cappedWriter struct {
	w     io.Writer
	left  int64
	noted bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.left <= 0 {
		if !c.noted {
			c.noted = true
			fmt.Fprintf(c.w, "\n[log truncated at %d bytes; the full output is in journalctl -u %s]\n", maxUpgradeLogBytes, upgradeServiceUnit)
		}
		return len(p), nil
	}
	chunk := p
	if int64(len(chunk)) > c.left {
		chunk = chunk[:c.left]
	}
	if _, err := c.w.Write(chunk); err != nil {
		c.left, c.noted = 0, true
		return len(p), nil
	}
	c.left -= int64(len(chunk))
	return len(p), nil
}

// bestEffortWriter swallows errors, for the same reason as cappedWriter.
type bestEffortWriter struct{ w io.Writer }

func (b bestEffortWriter) Write(p []byte) (int, error) {
	_, _ = b.w.Write(p)
	return len(p), nil
}

// ── status ───────────────────────────────────────────────────────────────────

type updaterInfo struct {
	installed     bool
	watcher       string // `systemctl is-active` of the path unit
	serviceActive bool
	pending       bool
	status        *upgradeStatus
	statusErr     error
	logTail       []string
}

func collectUpdaterInfo() updaterInfo {
	in := updaterInfo{installed: fileExists(filepath.Join(systemdUnitDir, upgradePathUnit))}
	if in.installed {
		in.watcher = systemctlState("is-active", upgradePathUnit)
		in.serviceActive = systemctlState("is-active", upgradeServiceUnit) == "activating"
	}
	_, err := os.Lstat(filepath.Join(upgradeInboxDir(), upgradeRequestFile))
	in.pending = err == nil
	in.status, in.statusErr = readUpgradeStatus(upgradeStateDir())
	in.logTail = tailLines(filepath.Join(upgradeStateDir(), upgradeLogFile), upgradeLogTailLines)
	return in
}

// effectiveUpgradeState reads a stored status the way a caller should: a run
// that stopped heartbeating without finishing was interrupted, and must not be
// reported as running forever.
func effectiveUpgradeState(st upgradeStatus, serviceActive bool, now time.Time) string {
	if st.State != "running" || serviceActive {
		return st.State
	}
	hb, err := time.Parse(time.RFC3339, st.HeartbeatAt)
	if err != nil || now.Sub(hb) > upgradeStaleAfter {
		return "interrupted"
	}
	return st.State
}

func printUpdaterStatus(w io.Writer, in updaterInfo, now time.Time) {
	switch {
	case !in.installed:
		fmt.Fprintln(w, "Updater:   not installed (turn it on with: sudo meshploy updater start)")
	case in.watcher == "active":
		fmt.Fprintln(w, "Updater:   on, watching for upgrade requests from the console")
	default:
		fmt.Fprintf(w, "Updater:   off (watcher %s; turn it on with: sudo meshploy updater start)\n", orDash(in.watcher))
	}
	if in.pending {
		fmt.Fprintln(w, "Queued:    an upgrade request is waiting to run")
	}

	switch {
	case in.statusErr != nil:
		fmt.Fprintf(w, "Last run:  unreadable (%v)\n", in.statusErr)
	case in.status == nil:
		fmt.Fprintln(w, "Last run:  none yet")
	default:
		st := *in.status
		fmt.Fprintf(w, "Last run:  %s, %s channel, requested by %s\n",
			effectiveUpgradeState(st, in.serviceActive, now), orDash(st.Channel), orDash(st.RequestedBy))
		fmt.Fprintf(w, "Started:   %s\n", orDash(st.StartedAt))
		if st.FinishedAt != "" {
			fmt.Fprintf(w, "Finished:  %s\n", st.FinishedAt)
		}
		if st.CLITo != "" && st.CLITo != st.CLIFrom {
			fmt.Fprintf(w, "CLI:       %s → %s\n", orDash(st.CLIFrom), st.CLITo)
		}
		if st.State != "succeeded" && st.Step != "" {
			fmt.Fprintf(w, "Step:      %s\n", st.Step)
		}
		if st.Error != "" {
			fmt.Fprintf(w, "Error:     %s\n", st.Error)
		}
	}

	if len(in.logTail) > 0 {
		fmt.Fprintf(w, "\nLast %d lines of %s:\n", len(in.logTail), filepath.Join(upgradeStateDir(), upgradeLogFile))
		for _, l := range in.logTail {
			fmt.Fprintln(w, "  "+l)
		}
	}
}

// tailLines returns up to n trailing lines of a file, reading only its end.
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const window = 64 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > window {
		_, _ = f.Seek(-window, io.SeekEnd)
	}
	data, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	updaterRunCmd.Flags().Bool("edge", false, "Upgrade to the edge channel (builds from main)")
	updaterRunCmd.Flags().Bool("stable", false, "Upgrade to the latest stable release")
	updaterRunCmd.MarkFlagsMutuallyExclusive("edge", "stable")
	updaterRunCmd.Flags().Bool("queued", false, "Act only on a request queued by the console (what the systemd service runs)")
	_ = updaterRunCmd.Flags().MarkHidden("queued")

	updaterCmd.AddCommand(updaterStartCmd, updaterStopCmd, updaterStatusCmd, updaterRunCmd)
	rootCmd.AddCommand(updaterCmd)
}
