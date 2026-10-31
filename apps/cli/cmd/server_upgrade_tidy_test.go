package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tidyEnv(t *testing.T) (ran *[]string, restarts *int, file string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "systemd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(dir, "journald.conf.d", "50-meshploy.conf")
	origCap, origRestart, origExec := journalCap, systemctlRestart, runtimeExec
	t.Cleanup(func() { journalCap, systemctlRestart, runtimeExec = origCap, origRestart, origExec })
	journalCap = file
	ran, restarts = &[]string{}, new(int)
	systemctlRestart = func(string) error { *restarts++; return nil }
	runtimeExec = func(runtime string, args ...string) error {
		*ran = append(*ran, runtime+" "+strings.Join(args, " "))
		return nil
	}
	return ran, restarts, file
}

// Only untagged images go: a plain prune, never --all, which would take every
// image a stopped container or an operator keeps.
func TestTidyingPrunesOnlyWhatLostItsTag(t *testing.T) {
	ran, _, _ := tidyEnv(t)
	tidyAfterUpgrade("podman")
	if len(*ran) != 1 || (*ran)[0] != "podman image prune -f" {
		t.Errorf("%v", *ran)
	}
}

func TestTheJournalIsCappedOnce(t *testing.T) {
	_, restarts, file := tidyEnv(t)
	if err := capJournal(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(body), "SystemMaxUse=500M") || *restarts != 1 {
		t.Fatalf("%q, %v, %d restarts", body, err, *restarts)
	}
	if err := capJournal(); err != nil || *restarts != 1 {
		t.Errorf("the second upgrade: %v, %d restarts", err, *restarts)
	}
}

func TestAnOperatorsOwnCapStays(t *testing.T) {
	_, restarts, file := tidyEnv(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := capJournal(); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(file); string(body) != "mine" || *restarts != 0 {
		t.Errorf("%q, %d restarts", body, *restarts)
	}
}

func TestNoSystemdNothingToCap(t *testing.T) {
	_, restarts, _ := tidyEnv(t)
	journalCap = filepath.Join(t.TempDir(), "nothing", "journald.conf.d", "50-meshploy.conf")
	if err := capJournal(); err != nil || *restarts != 0 {
		t.Errorf("%v, %d restarts", err, *restarts)
	}
	if _, err := os.Stat(filepath.Dir(journalCap)); err == nil {
		t.Error("a folder was made on a machine without systemd")
	}
}

// Tidying follows a good upgrade: if it cannot, the upgrade still stands.
func TestATidyThatFailsOnlyWarns(t *testing.T) {
	tidyEnv(t)
	runtimeExec = func(string, ...string) error { return errors.New("busy") }
	systemctlRestart = func(string) error { return errors.New("no") }
	tidyAfterUpgrade("docker")
}
