package dokploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
	"gopkg.in/yaml.v3"
)

func newEdge(t *testing.T) (EdgeSwitcher, *journal.Journal, string) {
	t.Helper()
	dynamic := t.TempDir()
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return EdgeSwitcher{DynamicDir: dynamic, Target: "http://172.17.0.1:8081", Journal: j}, j, dynamic
}

// An application's domain moves by rewriting the file Traefik already watches.
// It takes effect in seconds, with no restart - which is what lets a domain
// move before cutover.
func TestSwitchAppRewritesTheFileAndKeepsTheOriginal(t *testing.T) {
	e, j, dynamic := newEdge(t)
	path := filepath.Join(dynamic, "api.yml")
	if err := os.WriteFile(path, []byte(appDynamicFile), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := e.SwitchApp("move/g1/domain/api", "g1", "api"); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "http://172.17.0.1:8081") {
		t.Errorf("file was not switched:\n%s", after)
	}
	// Nothing temporary is left where Traefik would read it.
	entries, _ := os.ReadDir(dynamic)
	for _, f := range entries {
		if strings.HasSuffix(f.Name(), ".meshploy-tmp") {
			t.Errorf("a temporary file was left behind: %s", f.Name())
		}
	}

	rec := mustRead(t, j)[0]
	if rec.Undo == nil || rec.Undo.Kind != journal.UndoRestoreFile {
		t.Fatalf("undo = %+v", rec.Undo)
	}
	backup, err := os.ReadFile(rec.Undo.Args["backup"])
	if err != nil || string(backup) != appDynamicFile {
		t.Errorf("the original was not kept verbatim: %v", err)
	}
}

// Rolling it back puts the operator's file back exactly as it was, comments
// and all - which is why the original is stored rather than reconstructed.
func TestRollingBackADomainRestoresTheFileVerbatim(t *testing.T) {
	e, j, dynamic := newEdge(t)
	path := filepath.Join(dynamic, "api.yml")
	original := "# hand written, do not lose me\n" + appDynamicFile
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.SwitchApp("s", "g1", "api"); err != nil {
		t.Fatal(err)
	}

	entries, _ := journal.Read(j.Dir())
	res := Rollback{Runner: &fakeRunner{}}.Replay(journal.Undoable(entries, "g1"))
	if res.Undone != 1 || len(res.Failures) != 0 {
		t.Fatalf("rollback = %+v", res)
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("not restored verbatim:\n%s", after)
	}
}

// A compose app's router comes from labels, so its hosts are taken over by a
// new file instead. Rollback deletes it.
func TestTakeOverHostsWritesAndRemovesItsOwnFile(t *testing.T) {
	e, j, dynamic := newEdge(t)

	if err := e.TakeOverHosts("move/g1/domain/n8n", "g1", "productivity-n8n", []string{"n8n.example.com"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dynamic, "meshploy-productivity-n8n.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("the file Traefik will read is not valid YAML: %v", err)
	}

	entries, _ := journal.Read(j.Dir())
	res := Rollback{Runner: &fakeRunner{}}.Replay(journal.Undoable(entries, "g1"))
	if res.Undone != 1 {
		t.Fatalf("rollback = %+v", res)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the override file should be gone")
	}
}

// Overwriting a file Dokploy wrote would take an app off the air with no record
// of what was there.
func TestTakeOverRefusesToOverwriteSomebodyElsesFile(t *testing.T) {
	e, _, dynamic := newEdge(t)
	path := filepath.Join(dynamic, OverrideFileName("api"))
	if err := os.WriteFile(path, []byte(appDynamicFile), 0o644); err != nil {
		t.Fatal(err)
	}

	err := e.TakeOverHosts("s", "g1", "api", []string{"api.example.com"})
	if err == nil || !strings.Contains(err.Error(), "not written by Meshploy") {
		t.Fatalf("err = %v", err)
	}
	// And it is left exactly as it was.
	after, _ := os.ReadFile(path)
	if string(after) != appDynamicFile {
		t.Error("the other file was modified")
	}
}

// Re-running a take-over that Meshploy itself wrote is fine: a resumed move
// must not trip over its own work.
func TestTakeOverOverwritesItsOwnFile(t *testing.T) {
	e, _, dynamic := newEdge(t)
	if err := e.TakeOverHosts("s1", "g1", "api", []string{"api.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := e.TakeOverHosts("s2", "g1", "api", []string{"api.example.com", "www.example.com"}); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(dynamic, OverrideFileName("api")))
	if !strings.Contains(string(content), "www.example.com") {
		t.Error("the second write did not take")
	}
}

// A file that cannot be switched fails loudly: a silent no-op would leave the
// domain on Dokploy after its group had moved.
func TestSwitchAppFailsLoudly(t *testing.T) {
	e, j, dynamic := newEdge(t)
	if err := os.WriteFile(filepath.Join(dynamic, "odd.yml"), []byte("http:\n  routers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := e.SwitchApp("s1", "g1", "missing"); err == nil {
		t.Error("a missing file should fail")
	}
	if err := e.SwitchApp("s2", "g1", "odd"); err == nil {
		t.Error("a file with no services should fail")
	}
	for _, rec := range mustRead(t, j) {
		if rec.Result != journal.Failed || rec.Undo != nil {
			t.Errorf("entry = %+v", rec)
		}
	}
}

// Every undo kind the journal declares has an implementation. Adding a kind
// without one would be a rollback that silently skips a step.
func TestEveryUndoKindIsImplemented(t *testing.T) {
	kinds := []string{
		journal.UndoRestoreFile, journal.UndoRemoveFile, journal.UndoScaleService,
		journal.UndoStartContainer, journal.UndoStopService, journal.UndoPauseRoute,
	}
	r := Rollback{Runner: &fakeRunner{}, Meshploy: stubMeshploy{}}
	dir := t.TempDir()
	backup := filepath.Join(dir, "b")
	_ = os.WriteFile(backup, []byte("x"), 0o600)

	for _, kind := range kinds {
		args := map[string]string{
			"path": filepath.Join(dir, "target"), "backup": backup,
			"service": "svc", "replicas": "2", "container": "c",
			"project_id": "p", "service_id": "s", "route_id": "r",
		}
		if err := r.one(journal.Entry{Undo: &journal.Undo{Kind: kind, Args: args}}); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if err := r.one(journal.Entry{Undo: &journal.Undo{Kind: "invented"}}); err == nil {
		t.Error("an unknown kind must stop the rollback, not be skipped")
	}
}

// A step that needs the API, replayed by a rollback that has none, is reported
// rather than counted as undone.
func TestRollbackWithoutTheAPIReportsWhatItCannotDo(t *testing.T) {
	r := Rollback{Runner: &fakeRunner{}}
	res := r.Replay([]journal.Entry{
		{Action: "start-service", Target: "api", Undo: &journal.Undo{Kind: journal.UndoStopService, Args: map[string]string{"service_id": "s"}}},
	})
	if res.Undone != 0 || len(res.Failures) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.Failures[0], "api") {
		t.Errorf("the failure should name what is left: %v", res.Failures)
	}
}

type stubMeshploy struct{}

func (stubMeshploy) StopService(projectID, serviceID string) error { return nil }
func (stubMeshploy) PauseRoute(projectID, routeID string) error    { return nil }
