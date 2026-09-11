package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// updaterFixture points every path and seam the updater touches at a temp dir
// and records what would have run.
type updaterFixture struct {
	root  string
	steps [][]string // args of each upgradeStep call
	ctl   [][]string // args of each systemctl call
	fail  map[string]error
}

func newUpdaterFixture(t *testing.T) *updaterFixture {
	t.Helper()
	fx := &updaterFixture{root: t.TempDir(), fail: map[string]error{}}

	origDir, origUnits, origCLI, origInst := upgradeDir, systemdUnitDir, updaterCLIPath, meshployInstDir
	origStep, origVer, origURL := upgradeStep, upgradeCLIVersion, upgradeHealthURL
	origTimeout, origInterval, origBeat := upgradeHealthTimeout, upgradeHealthInterval, upgradeHeartbeat
	origCtl, origState := systemctl, systemctlState
	t.Cleanup(func() {
		upgradeDir, systemdUnitDir, updaterCLIPath, meshployInstDir = origDir, origUnits, origCLI, origInst
		upgradeStep, upgradeCLIVersion, upgradeHealthURL = origStep, origVer, origURL
		upgradeHealthTimeout, upgradeHealthInterval, upgradeHeartbeat = origTimeout, origInterval, origBeat
		systemctl, systemctlState = origCtl, origState
	})

	upgradeDir = filepath.Join(fx.root, "upgrade")
	systemdUnitDir = filepath.Join(fx.root, "systemd")
	updaterCLIPath = filepath.Join(fx.root, "meshploy")
	meshployInstDir = filepath.Join(fx.root, "opt")
	for _, d := range []string{systemdUnitDir, meshployInstDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, updaterCLIPath, "#!/bin/sh\n")
	writeTestFile(t, filepath.Join(meshployInstDir, ".env"), "MESHPLOY_CHANNEL=latest\n")

	upgradeStep = func(_ context.Context, w io.Writer, _ string, args ...string) error {
		fx.steps = append(fx.steps, args)
		fmt.Fprintf(w, "ran %s\n", strings.Join(args, " "))
		return fx.fail[args[0]]
	}
	upgradeCLIVersion = func(string) (string, error) { return "9.9.9", nil }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok","db":"ok"}`)
	}))
	t.Cleanup(srv.Close)
	upgradeHealthURL = srv.URL
	upgradeHealthTimeout, upgradeHealthInterval, upgradeHeartbeat = 2*time.Second, 10*time.Millisecond, 10*time.Millisecond

	systemctl = func(args ...string) error {
		fx.ctl = append(fx.ctl, args)
		return nil
	}
	systemctlState = func(string, string) string { return "inactive" }
	return fx
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func queueUpgradeRequest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(upgradeInboxDir(), upgradeRequestFile)
	writeTestFile(t, path, body)
	return path
}

func mustStatus(t *testing.T) upgradeStatus {
	t.Helper()
	st, err := readUpgradeStatus(upgradeStateDir())
	if err != nil || st == nil {
		t.Fatalf("status.json: %v, %v", st, err)
	}
	return *st
}

// ── consumeUpgradeRequest ────────────────────────────────────────────────────

func TestConsumeUpgradeRequestReadsAndRemovesIt(t *testing.T) {
	newUpdaterFixture(t)
	path := queueUpgradeRequest(t, `{"id":"r1","channel":"edge","requested_by":"u1","requested_at":"2026-09-10T12:00:00Z"}`)

	req, err := consumeUpgradeRequest(upgradeInboxDir())
	if err != nil || req == nil {
		t.Fatalf("got %v, %v", req, err)
	}
	if req.ID != "r1" || req.Channel != channelEdge || req.RequestedBy != "u1" {
		t.Errorf("parsed %+v", req)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("request not removed: %v", err)
	}
}

func TestConsumeUpgradeRequestNoneQueued(t *testing.T) {
	newUpdaterFixture(t)
	req, err := consumeUpgradeRequest(upgradeInboxDir())
	if req != nil || err != nil {
		t.Fatalf("got %v, %v", req, err)
	}
}

// A symlink is refused even when it points at a well-formed request: the API
// can write the inbox, and the runner is root on the host.
func TestConsumeUpgradeRequestRefusesSymlink(t *testing.T) {
	fx := newUpdaterFixture(t)
	target := filepath.Join(fx.root, "elsewhere.json")
	writeTestFile(t, target, `{"channel":"stable"}`)
	link := filepath.Join(upgradeInboxDir(), upgradeRequestFile)
	if err := os.MkdirAll(upgradeInboxDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := consumeUpgradeRequest(upgradeInboxDir()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("want a not-regular-file error, got %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("link not removed: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != `{"channel":"stable"}` {
		t.Errorf("symlink target was touched: %q", got)
	}
}

func TestConsumeUpgradeRequestRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"unknown channel": `{"channel":"nightly"}`,
		"no channel":      `{"id":"r1"}`,
		"not json":        `channel=edge`,
		// The image reaches server-upgrade, so the runner holds it to the
		// issuer's rule itself, whatever the API sent.
		"unknown edition":        `{"channel":"stable","edition":"platinum","image":"ghcr.io/meshploy/api-ee"}`,
		"image without edition":  `{"channel":"stable","image":"ghcr.io/meshploy/api-ee"}`,
		"enterprise, no image":   `{"channel":"stable","edition":"enterprise"}`,
		"another registry":       `{"channel":"stable","edition":"enterprise","image":"docker.io/evil/api-ee"}`,
		"another meshploy image": `{"channel":"stable","edition":"enterprise","image":"ghcr.io/meshploy/admin"}`,
		"a tag":                  `{"channel":"stable","edition":"enterprise","image":"ghcr.io/meshploy/api-ee:latest"}`,
		"an argument":            `{"channel":"stable","edition":"enterprise","image":"--no-rollback"}`,
		"too large":              `{"channel":"edge","id":"` + strings.Repeat("x", maxUpgradeRequestBytes) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			newUpdaterFixture(t)
			path := queueUpgradeRequest(t, body)
			if req, err := consumeUpgradeRequest(upgradeInboxDir()); err == nil {
				t.Fatalf("accepted %+v", req)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("rejected request left in place, would re-trigger the service: %v", err)
			}
		})
	}
}

func TestConsumeUpgradeRequestRemovesADirectory(t *testing.T) {
	newUpdaterFixture(t)
	dir := filepath.Join(upgradeInboxDir(), upgradeRequestFile)
	writeTestFile(t, filepath.Join(dir, "inner"), "x")
	if _, err := consumeUpgradeRequest(upgradeInboxDir()); err == nil {
		t.Fatal("accepted a directory")
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("directory left in place: %v", err)
	}
}

func TestClipStripsTerminalControl(t *testing.T) {
	if got := clip("u1\x1b[2J\nevil"); got != "u12Jevil" {
		t.Errorf("clip = %q", got)
	}
	if got := clip(strings.Repeat("a", 100)); len(got) != 64 {
		t.Errorf("clip kept %d chars", len(got))
	}
}

// ── runUpgrade ───────────────────────────────────────────────────────────────

func TestRunUpgradeQueuedRequest(t *testing.T) {
	fx := newUpdaterFixture(t)
	queueUpgradeRequest(t, `{"id":"r1","channel":"edge","requested_by":"u1"}`)

	var out bytes.Buffer
	if err := runUpgrade(context.Background(), &out, upgradeOptions{queued: true}); err != nil {
		t.Fatalf("runUpgrade: %v\n%s", err, out.String())
	}

	want := [][]string{{"update", "--edge"}, {"server-upgrade", "--edge"}}
	if !reflect.DeepEqual(fx.steps, want) {
		t.Errorf("steps = %v, want %v", fx.steps, want)
	}
	st := mustStatus(t)
	if st.State != "succeeded" || st.ID != "r1" || st.Channel != channelEdge || st.RequestedBy != "u1" ||
		st.CLITo != "9.9.9" || st.FinishedAt == "" || st.Error != "" {
		t.Errorf("status = %+v", st)
	}
	fi, err := os.Stat(filepath.Join(upgradeStateDir(), upgradeStatusFile))
	if err != nil || fi.Mode().Perm() != 0644 {
		t.Errorf("status.json mode: %v, %v", fi, err)
	}
	log, _ := os.ReadFile(filepath.Join(upgradeStateDir(), upgradeLogFile))
	for _, s := range []string{"▸ Updating the CLI", "ran update --edge", "▸ Upgrading the server", "▸ Waiting for the API", "Upgrade finished"} {
		if !strings.Contains(string(log), s) {
			t.Errorf("log is missing %q:\n%s", s, log)
		}
	}
}

// A switch to Enterprise is the upgrade with the Enterprise image named, so
// server-upgrade does the switch, its pull check and its rollback.
func TestRunUpgradeSwitchesToEnterprise(t *testing.T) {
	fx := newUpdaterFixture(t)
	queueUpgradeRequest(t, `{"id":"r2","channel":"stable","requested_by":"u1","edition":"enterprise","image":"ghcr.io/meshploy/api-ee-acme"}`)

	var out bytes.Buffer
	if err := runUpgrade(context.Background(), &out, upgradeOptions{queued: true}); err != nil {
		t.Fatalf("runUpgrade: %v\n%s", err, out.String())
	}

	want := [][]string{{"update"}, {"server-upgrade", "--ee", "--ee-image", "ghcr.io/meshploy/api-ee-acme"}}
	if !reflect.DeepEqual(fx.steps, want) {
		t.Errorf("steps = %v, want %v", fx.steps, want)
	}
	st := mustStatus(t)
	if st.State != "succeeded" || st.Edition != editionEnterprise || st.Image != "ghcr.io/meshploy/api-ee-acme" {
		t.Errorf("status = %+v", st)
	}
	log, _ := os.ReadFile(filepath.Join(upgradeStateDir(), upgradeLogFile))
	if !strings.Contains(string(log), "Switching to the Enterprise images (ghcr.io/meshploy/api-ee-acme)") {
		t.Errorf("log does not say it is switching:\n%s", log)
	}
}

func TestRunUpgradeQueuedWithNothingQueuedDoesNothing(t *testing.T) {
	fx := newUpdaterFixture(t)
	var out bytes.Buffer
	if err := runUpgrade(context.Background(), &out, upgradeOptions{queued: true}); err != nil {
		t.Fatal(err)
	}
	if len(fx.steps) != 0 {
		t.Errorf("ran %v without a request", fx.steps)
	}
	if st, _ := readUpgradeStatus(upgradeStateDir()); st != nil {
		t.Errorf("wrote a status without a run: %+v", st)
	}
}

func TestRunUpgradeRejectedRequestIsRecorded(t *testing.T) {
	fx := newUpdaterFixture(t)
	queueUpgradeRequest(t, `{"channel":"nightly"}`)

	err := runUpgrade(context.Background(), io.Discard, upgradeOptions{queued: true})
	if err == nil {
		t.Fatal("want an error")
	}
	if len(fx.steps) != 0 {
		t.Errorf("ran %v for a rejected request", fx.steps)
	}
	st := mustStatus(t)
	if st.State != "failed" || !strings.Contains(st.Error, "unknown channel") {
		t.Errorf("status = %+v", st)
	}
}

func TestRunUpgradeStepFailureIsRecorded(t *testing.T) {
	fx := newUpdaterFixture(t)
	fx.fail["server-upgrade"] = errors.New("exit status 1")
	queueUpgradeRequest(t, `{"id":"r1","channel":"stable"}`)

	if err := runUpgrade(context.Background(), io.Discard, upgradeOptions{queued: true}); err == nil {
		t.Fatal("want an error")
	}
	st := mustStatus(t)
	if st.State != "failed" || st.Step != "Upgrading the server" || !strings.Contains(st.Error, "server-upgrade") {
		t.Errorf("status = %+v", st)
	}
	want := [][]string{{"update"}, {"server-upgrade"}}
	if !reflect.DeepEqual(fx.steps, want) {
		t.Errorf("steps = %v, want %v (stable passes no flag)", fx.steps, want)
	}
}

func TestRunUpgradeUnhealthyAPIFails(t *testing.T) {
	newUpdaterFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"degraded","db":"connection refused"}`)
	}))
	defer srv.Close()
	upgradeHealthURL, upgradeHealthTimeout = srv.URL, 50*time.Millisecond

	if err := runUpgrade(context.Background(), io.Discard, upgradeOptions{channel: channelStable}); err == nil {
		t.Fatal("want an error")
	}
	st := mustStatus(t)
	if st.State != "failed" || !strings.Contains(st.Error, "degraded") {
		t.Errorf("status = %+v", st)
	}
}

// By hand, the run stays on the server's channel unless told otherwise, so it
// cannot move an edge server to stable the way server-upgrade without --edge
// does.
func TestRunUpgradeByHandKeepsTheCurrentChannel(t *testing.T) {
	cases := []struct {
		env     string
		channel string
		want    []string
	}{
		{"MESHPLOY_CHANNEL=main\n", "", []string{"update", "--edge"}},
		{"MESHPLOY_CHANNEL=latest\n", "", []string{"update"}},
		{"\n", "", []string{"update"}},
		{"MESHPLOY_CHANNEL=main\n", channelStable, []string{"update"}},
		{"MESHPLOY_CHANNEL=latest\n", channelEdge, []string{"update", "--edge"}},
	}
	for _, c := range cases {
		t.Run(strings.TrimSpace(c.env)+"/"+c.channel, func(t *testing.T) {
			fx := newUpdaterFixture(t)
			writeTestFile(t, filepath.Join(meshployInstDir, ".env"), c.env)
			if err := runUpgrade(context.Background(), io.Discard, upgradeOptions{channel: c.channel}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fx.steps[0], c.want) {
				t.Errorf("first step = %v, want %v", fx.steps[0], c.want)
			}
		})
	}
}

func TestRunUpgradeByHandConsumesAQueuedRequest(t *testing.T) {
	newUpdaterFixture(t)
	path := queueUpgradeRequest(t, `{"id":"r1","channel":"edge"}`)
	var out bytes.Buffer
	if err := runUpgrade(context.Background(), &out, upgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("queued request left behind; it would run a second upgrade")
	}
	if st := mustStatus(t); !strings.HasPrefix(st.ID, "cli-") || !strings.HasPrefix(st.RequestedBy, "cli") {
		t.Errorf("status = %+v", st)
	}
	if !strings.Contains(out.String(), "this run covers it") {
		t.Errorf("output: %s", out.String())
	}
}

func TestRunUpgradeLeavesAQueuedRequestWhileAnotherRunHoldsTheLock(t *testing.T) {
	fx := newUpdaterFixture(t)
	if err := os.MkdirAll(upgradeDir, 0755); err != nil {
		t.Fatal(err)
	}
	unlock, ok, err := lockUpgrade(filepath.Join(upgradeDir, "lock"))
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	defer unlock()
	path := queueUpgradeRequest(t, `{"channel":"edge"}`)

	if err := runUpgrade(context.Background(), io.Discard, upgradeOptions{queued: true}); err != nil {
		t.Fatal(err)
	}
	if len(fx.steps) != 0 {
		t.Errorf("ran %v while locked", fx.steps)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Error("request consumed by a run that did not hold the lock")
	}
}

// ── start / stop ─────────────────────────────────────────────────────────────

func TestUpdaterStartInstallsUnitsAndEnablesTheWatcher(t *testing.T) {
	fx := newUpdaterFixture(t)
	if err := updaterStart(io.Discard); err != nil {
		t.Fatal(err)
	}

	path, _ := os.ReadFile(filepath.Join(systemdUnitDir, upgradePathUnit))
	if want := "PathExists=" + filepath.Join(upgradeDir, "inbox", upgradeRequestFile); !strings.Contains(string(path), want) {
		t.Errorf("path unit lacks %q:\n%s", want, path)
	}
	svc, _ := os.ReadFile(filepath.Join(systemdUnitDir, upgradeServiceUnit))
	if want := "ExecStart=" + updaterCLIPath + " updater run --queued"; !strings.Contains(string(svc), want) {
		t.Errorf("service unit lacks %q:\n%s", want, svc)
	}
	wantCtl := [][]string{{"daemon-reload"}, {"enable", "--now", upgradePathUnit}}
	if !reflect.DeepEqual(fx.ctl, wantCtl) {
		t.Errorf("systemctl calls = %v, want %v", fx.ctl, wantCtl)
	}
	for _, p := range []string{upgradeInboxDir(), filepath.Join(upgradeStateDir(), upgradeEnabledFile)} {
		if !fileExists(p) {
			t.Errorf("%s missing", p)
		}
	}

	// Idempotent: a second start just rewrites the units.
	if err := updaterStart(io.Discard); err != nil {
		t.Fatalf("second start: %v", err)
	}
}

func TestUpdaterStartRefusesAMachineWithoutAServer(t *testing.T) {
	fx := newUpdaterFixture(t)
	if err := os.Remove(filepath.Join(meshployInstDir, ".env")); err != nil {
		t.Fatal(err)
	}
	err := updaterStart(io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no Meshploy server") {
		t.Fatalf("got %v", err)
	}
	if len(fx.ctl) != 0 || fileExists(filepath.Join(systemdUnitDir, upgradePathUnit)) {
		t.Error("installed units on a machine with no server")
	}
}

// The enabled marker must not exist if enabling the watcher failed: the
// console reads it as "the button will work".
func TestUpdaterStartWritesNoMarkerWhenEnableFails(t *testing.T) {
	newUpdaterFixture(t)
	systemctl = func(args ...string) error {
		if args[0] == "enable" {
			return errors.New("exit status 1")
		}
		return nil
	}
	if err := updaterStart(io.Discard); err == nil {
		t.Fatal("want an error")
	}
	if fileExists(filepath.Join(upgradeStateDir(), upgradeEnabledFile)) {
		t.Error("enabled marker written although the watcher is not running")
	}
}

func TestUpdaterStopDisablesAndDiscardsAPendingRequest(t *testing.T) {
	fx := newUpdaterFixture(t)
	if err := updaterStart(io.Discard); err != nil {
		t.Fatal(err)
	}
	path := queueUpgradeRequest(t, `{"channel":"edge"}`)
	fx.ctl = nil

	var out bytes.Buffer
	if err := updaterStop(&out); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"disable", "--now", upgradePathUnit}}; !reflect.DeepEqual(fx.ctl, want) {
		t.Errorf("systemctl calls = %v, want %v", fx.ctl, want)
	}
	if fileExists(filepath.Join(upgradeStateDir(), upgradeEnabledFile)) {
		t.Error("enabled marker left behind")
	}
	if fileExists(path) {
		t.Error("pending request left behind; it would run when the watcher comes back")
	}
	if !strings.Contains(out.String(), "Discarded") {
		t.Errorf("output: %s", out.String())
	}
}

func TestUpdaterStopWhenNotInstalled(t *testing.T) {
	fx := newUpdaterFixture(t)
	var out bytes.Buffer
	if err := updaterStop(&out); err != nil {
		t.Fatal(err)
	}
	if len(fx.ctl) != 0 || !strings.Contains(out.String(), "not installed") {
		t.Errorf("ctl = %v, output: %s", fx.ctl, out.String())
	}
}

// ── status ───────────────────────────────────────────────────────────────────

func TestEffectiveUpgradeState(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-30 * time.Second).Format(time.RFC3339)
	stale := now.Add(-5 * time.Minute).Format(time.RFC3339)
	cases := []struct {
		st            upgradeStatus
		serviceActive bool
		want          string
	}{
		{upgradeStatus{State: "succeeded", HeartbeatAt: stale}, false, "succeeded"},
		{upgradeStatus{State: "running", HeartbeatAt: fresh}, false, "running"},
		{upgradeStatus{State: "running", HeartbeatAt: stale}, false, "interrupted"},
		{upgradeStatus{State: "running", HeartbeatAt: stale}, true, "running"},
		{upgradeStatus{State: "running", HeartbeatAt: "garbage"}, false, "interrupted"},
	}
	for _, c := range cases {
		if got := effectiveUpgradeState(c.st, c.serviceActive, now); got != c.want {
			t.Errorf("%+v active=%v: got %s, want %s", c.st, c.serviceActive, got, c.want)
		}
	}
}

func TestPrintUpdaterStatus(t *testing.T) {
	newUpdaterFixture(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	var out bytes.Buffer
	printUpdaterStatus(&out, updaterInfo{}, now)
	if s := out.String(); !strings.Contains(s, "not installed") || !strings.Contains(s, "none yet") {
		t.Errorf("fresh machine:\n%s", s)
	}

	out.Reset()
	printUpdaterStatus(&out, updaterInfo{
		installed: true, watcher: "active", pending: true,
		status: &upgradeStatus{
			State: "failed", Step: "Upgrading the server", Channel: "edge", RequestedBy: "u1",
			CLIFrom: "0.10.0", CLITo: "0.11.0", StartedAt: "a", FinishedAt: "b", Error: "compose pull: boom",
		},
		logTail: []string{"line one", "line two"},
	}, now)
	for _, want := range []string{"on, watching", "waiting to run", "failed, edge channel, requested by u1",
		"0.10.0 → 0.11.0", "Step:      Upgrading the server", "compose pull: boom", "  line two"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestTailLines(t *testing.T) {
	fx := newUpdaterFixture(t)
	path := filepath.Join(fx.root, "log")
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	writeTestFile(t, path, b.String())
	got := tailLines(path, 3)
	if !reflect.DeepEqual(got, []string{"line 28", "line 29", "line 30"}) {
		t.Errorf("tail = %v", got)
	}
	if got := tailLines(filepath.Join(fx.root, "missing"), 3); got != nil {
		t.Errorf("missing file = %v", got)
	}
}

// ── writers and health ───────────────────────────────────────────────────────

func TestCappedWriterNeverFailsTheWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &cappedWriter{w: &buf, left: 5}
	for _, s := range []string{"abc", "defg", "hij"} {
		if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", s, n, err)
		}
	}
	if !strings.HasPrefix(buf.String(), "abcde\n[log truncated") || strings.Count(buf.String(), "truncated") != 1 {
		t.Errorf("got %q", buf.String())
	}

	broken := &cappedWriter{w: errWriter{}, left: 100}
	if n, err := broken.Write([]byte("x")); n != 1 || err != nil {
		t.Errorf("a failing log must not fail the child: %d, %v", n, err)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWaitForHealthyAPIWaitsThroughTheRestart(t *testing.T) {
	newUpdaterFixture(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusBadGateway)
		case 2:
			fmt.Fprint(w, `{"status":"degraded","db":"starting"}`)
		default:
			fmt.Fprint(w, `{"status":"ok","db":"ok"}`)
		}
	}))
	defer srv.Close()

	if err := waitForHealthyAPI(context.Background(), srv.URL, time.Second); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("polled %d times, want 3", calls.Load())
	}
}
