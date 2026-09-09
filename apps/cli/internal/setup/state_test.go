package setup

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Closing the browser mid-install must resume, not restart. That is the whole
// reason state is on disk rather than in memory.
func TestStateSurvivesARestart(t *testing.T) {
	dir := t.TempDir()

	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Phase; got != PhaseAnswers {
		t.Fatalf("a fresh install starts at %q, got %q", PhaseAnswers, got)
	}

	if err := s.Update(func(st *State) {
		st.Phase = PhaseInstall
		st.Answers = Answers{Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10"}
	}); err != nil {
		t.Fatal(err)
	}
	s.Append("pulling images")
	s.Append("starting postgres")

	// A second process — the operator reloaded, or the server was restarted.
	again, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Get()
	if got.Phase != PhaseInstall {
		t.Errorf("phase lost across restart: %q", got.Phase)
	}
	if got.Answers.Domain != "example.com" || got.Answers.DNSMode != "ondemand" {
		t.Errorf("answers lost across restart: %+v", got.Answers)
	}
	if len(got.Log) != 2 {
		t.Errorf("the transcript must replay to a reconnecting browser, got %d lines", len(got.Log))
	}
}

// A corrupt state file must not brick the installer — starting over is
// recoverable, refusing to start is not.
func TestCorruptStateStartsOverRatherThanFailing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".setup-state.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("a corrupt state file must not be fatal: %v", err)
	}
	if s.Get().Phase != PhaseAnswers {
		t.Error("should have fallen back to the start")
	}
}

// Get hands out a copy; a caller mutating it must not corrupt shared state.
func TestGetReturnsACopy(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Append("one")

	got := s.Get()
	got.Log[0] = "tampered"
	got.Phase = PhaseComplete

	if s.Get().Log[0] != "one" {
		t.Error("the transcript was mutated through a returned copy")
	}
	if s.Get().Phase == PhaseComplete {
		t.Error("the phase was mutated through a returned copy")
	}
}

// The HTTP handlers and the install goroutine both write. Run under -race.
func TestConcurrentAppendAndUpdate(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.Append("line") }()
		go func() { defer wg.Done(); _ = s.Update(func(st *State) { st.Phase = PhaseInstall }) }()
	}
	wg.Wait()

	if n := len(s.Get().Log); n != 20 {
		t.Errorf("want 20 transcript lines, got %d", n)
	}
}

// Once the console is up the installer must not resume a finished install.
func TestClearRemovesTheStateFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	_ = s.Update(func(st *State) { st.Phase = PhaseComplete })

	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".setup-state.json")); !os.IsNotExist(err) {
		t.Error("the state file should be gone")
	}
	// And clearing twice is not an error — the command may be re-run.
	if err := s.Clear(); err != nil {
		t.Errorf("clearing an already-clear install must be a no-op: %v", err)
	}
}

// A truncated write must leave the previous state readable, which is why
// persist renames rather than writing in place.
func TestNoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	_ = s.Update(func(st *State) { st.Answers.Domain = "example.com" })

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
}
