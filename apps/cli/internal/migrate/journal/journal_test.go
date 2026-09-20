package journal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndResume(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	steps := []Entry{
		{Step: "prepare/project/p1", Action: "create-project", Target: "productivity", Result: OK},
		{Step: "prepare/service/s1", Action: "create-service", Target: "api", Result: OK,
			Undo: &Undo{Kind: UndoStopService, Args: map[string]string{"service_id": "abc"}}},
		{Step: "prepare/service/s2", Action: "create-service", Target: "web", Result: Failed, Error: "no image"},
	}
	for _, e := range steps {
		if err := j.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	// A resumed run skips what succeeded and retries what did not.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if !again.Done("prepare/project/p1") || !again.Done("prepare/service/s1") {
		t.Error("a successful step should be skipped on resume")
	}
	if again.Done("prepare/service/s2") {
		t.Error("a failed step must be retried, not skipped")
	}
	if again.Done("prepare/service/never-run") {
		t.Error("an unknown step is not done")
	}

	// Retrying it marks it done without a second entry deciding otherwise.
	if err := again.Append(Entry{Step: "prepare/service/s2", Action: "create-service", Result: OK}); err != nil {
		t.Fatal(err)
	}
	if !again.Done("prepare/service/s2") {
		t.Error("the retry should have marked it done")
	}
}

// Rollback replays backwards, and rolling back one group leaves the others.
func TestUndoableIsNewestFirstAndPerGroup(t *testing.T) {
	entries := []Entry{
		{Step: "1", Action: "a", Result: OK, Undo: &Undo{Kind: UndoRemoveFile}},
		{Step: "2", Group: "g1", Action: "b", Result: OK, Undo: &Undo{Kind: UndoRestoreFile}},
		{Step: "3", Group: "g1", Action: "c", Result: Failed, Undo: &Undo{Kind: UndoRemoveFile}},
		{Step: "4", Group: "g2", Action: "d", Result: OK, Undo: &Undo{Kind: UndoScaleService}},
		{Step: "5", Group: "g1", Action: "e", Result: OK},
		{Step: "6", Group: "g1", Action: "f", Result: OK, Undo: &Undo{Kind: UndoStartContainer}},
	}

	all := Undoable(entries, "")
	var order []string
	for _, e := range all {
		order = append(order, e.Step)
	}
	want := []string{"6", "4", "2", "1"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got %v, want %v", order, want)
		}
	}

	g1 := Undoable(entries, "g1")
	if len(g1) != 2 || g1[0].Step != "6" || g1[1].Step != "2" {
		t.Errorf("one group's rollback: got %d entries %+v", len(g1), g1)
	}
}

// A power cut leaves a half-written line. The rest of the record is still true.
func TestATruncatedLastLineDoesNotLoseTheRest(t *testing.T) {
	dir := t.TempDir()
	j, _ := Open(dir)
	_ = j.Append(Entry{Step: "1", Action: "a", Result: OK})
	_ = j.Append(Entry{Step: "2", Action: "b", Result: OK})
	j.Close()

	path := filepath.Join(dir, FileName)
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(b, []byte(`{"step":"3","act`)...), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("a truncated line should not fail the read: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want the two complete ones", len(entries))
	}
	resumed, _ := Open(dir)
	defer resumed.Close()
	if !resumed.Done("1") || !resumed.Done("2") {
		t.Error("the complete steps are still done")
	}
}

// Nothing has happened yet is an empty journal, not an error.
func TestReadingABareDirectory(t *testing.T) {
	entries, err := Read(t.TempDir())
	if err != nil || len(entries) != 0 {
		t.Errorf("got %v, %v", entries, err)
	}
}

func TestBackupStoresContentBesideTheJournal(t *testing.T) {
	dir := t.TempDir()
	j, _ := Open(dir)
	defer j.Close()

	path, err := j.Backup("app.yml", []byte("http:\n  routers: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "http:\n  routers: {}\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if filepath.Dir(filepath.Dir(path)) != dir {
		t.Errorf("backup landed at %s, want it under %s", path, dir)
	}
}

// An edge's configuration is copied with its own layout, so it can be put back
// the way it was found.
func TestBackupKeepsNestedPaths(t *testing.T) {
	dir := t.TempDir()
	j, _ := Open(dir)
	defer j.Close()

	path, err := j.Backup(filepath.Join("edge", "dynamic", "web.yml"), []byte("http: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "http: {}\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// A rollback records what it reversed, and what was reversed is no longer done:
// a group that was put back and moved again must start its copies over rather
// than wait forever for services nothing restarted.
func TestAnUndoneStepIsNotDone(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := Entry{Step: "move/g1/start/s1", Group: "g1", Action: "start-service", Result: OK,
		Created: "svc-1", Undo: &Undo{Kind: UndoStopService}}
	if err := j.Append(start); err != nil {
		t.Fatal(err)
	}
	if !j.Done(start.Step) || j.CreatedBy(start.Step) != "svc-1" {
		t.Fatal("the step should be done after it succeeded")
	}
	if err := j.Append(Entry{Step: start.Step, Group: "g1", Action: "undo stop-service", Result: Undone}); err != nil {
		t.Fatal(err)
	}
	if j.Done(start.Step) {
		t.Error("an undone step must run again, not be skipped")
	}
	if j.CreatedBy(start.Step) != "" {
		t.Error("an undone step created nothing that still stands")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	// And the same after a restart, which reads the record rather than holding it.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Done(start.Step) {
		t.Error("a reopened journal must also see the step as undone")
	}
}

// Undoing twice is not undoing again: a second rollback, or a rollback of
// everything after one group was already put back, has nothing left to do.
func TestUndoableSkipsWhatWasAlreadyUndone(t *testing.T) {
	entries := []Entry{
		{Step: "1", Group: "g1", Action: "stop", Result: OK, Undo: &Undo{Kind: UndoScaleService}},
		{Step: "2", Group: "g1", Action: "start", Result: OK, Undo: &Undo{Kind: UndoStopService}},
		{Step: "1", Group: "g1", Action: "undo scale-service", Result: Undone},
	}
	got := Undoable(entries, "g1")
	if len(got) != 1 {
		t.Fatalf("expected the one step still standing, got %d", len(got))
	}
	if got[0].Step != "2" {
		t.Errorf("expected step 2, got %s", got[0].Step)
	}
	if len(Undoable(append(entries, Entry{Step: "2", Group: "g1", Result: Undone}), "g1")) != 0 {
		t.Error("with both reversed there is nothing left to undo")
	}
}
