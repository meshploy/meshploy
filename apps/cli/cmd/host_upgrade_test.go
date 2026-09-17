package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type launchFixture struct {
	runs   [][]string
	active string
	fail   error
}

func newLaunchFixture(t *testing.T) *launchFixture {
	t.Helper()
	newUpdaterFixture(t)
	lf := &launchFixture{active: "inactive"}
	origRun := hostSystemdRun
	t.Cleanup(func() { hostSystemdRun = origRun })
	hostSystemdRun = func(args ...string) (string, error) {
		lf.runs = append(lf.runs, args)
		if lf.fail != nil {
			return "Failed to start transient service unit: Access denied", lf.fail
		}
		return "", nil
	}
	systemctlState = func(verb, unit string) string {
		if unit == upgradeServiceUnit {
			return lf.active
		}
		return "inactive"
	}
	if err := ensureUpgradeDirs(); err != nil {
		t.Fatal(err)
	}
	return lf
}

func enableUpdater(t *testing.T) {
	t.Helper()
	writeTestFile(t, filepath.Join(upgradeStateDir(), upgradeEnabledFile), "on\n")
}

// A queued request starts one transient run of the updater, on the fixed unit
// name, detached from the agent.
func TestAgentLaunchesAQueuedUpgrade(t *testing.T) {
	lf := newLaunchFixture(t)
	enableUpdater(t)
	queueUpgradeRequest(t, `{"id":"u1","channel":"edge"}`)

	var l upgradeLauncher
	task, changed := l.check(time.Now())
	if !changed || !task.OK || len(lf.runs) != 1 {
		t.Fatalf("task=%+v changed=%v runs=%v", task, changed, lf.runs)
	}
	args := strings.Join(lf.runs[0], " ")
	for _, want := range []string{"--unit=meshploy-upgrade", "--collect", "--no-block", "--service-type=oneshot", updaterCLIPath + " updater run --queued"} {
		if !strings.Contains(args, want) {
			t.Errorf("systemd-run args lack %q: %s", want, args)
		}
	}
}

func TestAgentLeavesRequestsAloneWhenItShould(t *testing.T) {
	t.Run("updater off", func(t *testing.T) {
		lf := newLaunchFixture(t)
		queueUpgradeRequest(t, `{"channel":"edge"}`)
		var l upgradeLauncher
		if _, changed := l.check(time.Now()); changed || len(lf.runs) != 0 {
			t.Errorf("launched with the updater off: %v", lf.runs)
		}
	})
	t.Run("nothing queued", func(t *testing.T) {
		lf := newLaunchFixture(t)
		enableUpdater(t)
		var l upgradeLauncher
		if _, changed := l.check(time.Now()); changed || len(lf.runs) != 0 {
			t.Errorf("launched with nothing queued: %v", lf.runs)
		}
	})
	t.Run("a run in progress", func(t *testing.T) {
		lf := newLaunchFixture(t)
		enableUpdater(t)
		queueUpgradeRequest(t, `{"channel":"edge"}`)
		lf.active = "activating"
		var l upgradeLauncher
		if _, changed := l.check(time.Now()); changed || len(lf.runs) != 0 {
			t.Errorf("launched over a running upgrade: %v", lf.runs)
		}
	})
	t.Run("a symlink in the inbox", func(t *testing.T) {
		lf := newLaunchFixture(t)
		enableUpdater(t)
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		writeTestFile(t, target, `{"channel":"edge"}`)
		if err := os.Symlink(target, filepath.Join(upgradeInboxDir(), upgradeRequestFile)); err != nil {
			t.Fatal(err)
		}
		var l upgradeLauncher
		if _, changed := l.check(time.Now()); changed || len(lf.runs) != 0 {
			t.Errorf("acted on a symlink: %v", lf.runs)
		}
	})
}

// A request still waiting after a launch means the run never started. The
// agent waits before trying again, rather than launching every two seconds.
func TestAgentDoesNotRelaunchAStuckRequestAtOnce(t *testing.T) {
	lf := newLaunchFixture(t)
	enableUpdater(t)
	queueUpgradeRequest(t, `{"channel":"edge"}`)
	now := time.Now()

	var l upgradeLauncher
	l.check(now)
	l.check(now.Add(upgradeWatchInterval))
	if len(lf.runs) != 1 {
		t.Fatalf("launched %d times within the wait, want 1", len(lf.runs))
	}
	l.check(now.Add(upgradeRelaunchAfter + time.Second))
	if len(lf.runs) != 2 {
		t.Errorf("did not retry after the wait: %d launches", len(lf.runs))
	}
}

func TestAgentRecordsAFailedLaunch(t *testing.T) {
	lf := newLaunchFixture(t)
	lf.fail = errors.New("exit status 1")
	enableUpdater(t)
	queueUpgradeRequest(t, `{"channel":"edge"}`)

	var l upgradeLauncher
	task, changed := l.check(time.Now())
	if !changed || task.OK || !strings.Contains(task.Error, "Access denied") {
		t.Errorf("task = %+v", task)
	}
}
