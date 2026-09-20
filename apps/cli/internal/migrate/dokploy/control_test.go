package dokploy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// fakeRunner answers the commands Control runs and records what it was asked
// to do, so a test can assert on the sequence rather than on a real Docker.
type fakeRunner struct {
	replies map[string]string
	errs    map[string]error
	ran     []string
}

func (f *fakeRunner) Output(name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.ran = append(f.ran, cmd)
	if err, ok := f.errs[cmd]; ok {
		return "", err
	}
	if out, ok := f.replies[cmd]; ok {
		return out, nil
	}
	return "", nil
}

func (f *fakeRunner) didRunAfter(cmd string, from int) bool {
	for _, c := range f.ran[from:] {
		if c == cmd {
			return true
		}
	}
	return false
}

func (f *fakeRunner) didRun(cmd string) bool {
	for _, c := range f.ran {
		if c == cmd {
			return true
		}
	}
	return false
}

func newControl(t *testing.T, r *fakeRunner) (Control, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return Control{Runner: r, Journal: j}, j
}

// Stopping a Swarm service records the replica count it had, because "scale
// back to 1" is a guess and "scale back to what it was" is a rollback.
func TestStoppingASwarmServiceRecordsItsReplicas(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker service inspect api --format {{.Spec.Mode.Replicated.Replicas}}": "3\n",
	}}
	c, j := newControl(t, r)

	if err := c.Stop("move/g1/stop/api", "g1", Workload{Name: "api", Swarm: true}); err != nil {
		t.Fatal(err)
	}
	if !r.didRun("docker service scale --detach api=0") {
		t.Fatalf("commands run: %v", r.ran)
	}

	entries, _ := journal.Read(j.Dir())
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	e := entries[0]
	if e.Result != journal.OK || e.Group != "g1" || e.Undo == nil {
		t.Fatalf("entry = %+v", e)
	}
	if e.Undo.Kind != journal.UndoScaleService || e.Undo.Args["replicas"] != "3" || e.Undo.Args["service"] != "api" {
		t.Errorf("undo = %+v", e.Undo)
	}
}

// A workload the operator had already stopped is left alone, and recorded as
// skipped - so a rollback does not start something they chose to stop.
func TestStoppingSomethingAlreadyStopped(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker service inspect api --format {{.Spec.Mode.Replicated.Replicas}}": "0\n",
		"docker inspect worker --format {{.State.Running}}":                      "false\n",
	}}
	c, j := newControl(t, r)

	if err := c.Stop("s1", "g1", Workload{Name: "api", Swarm: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop("s2", "g1", Workload{Name: "worker"}); err != nil {
		t.Fatal(err)
	}
	if r.didRun("docker service scale --detach api=0") || r.didRun("docker stop worker") {
		t.Errorf("nothing should have been stopped: %v", r.ran)
	}
	for _, e := range mustRead(t, j) {
		if e.Result != journal.Skipped || e.Undo != nil {
			t.Errorf("entry = %+v, want skipped with no undo", e)
		}
	}
}

func TestStoppingAContainerRecordsHowToStartIt(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker inspect wakapi --format {{.State.Running}}": "true\n",
	}}
	c, j := newControl(t, r)

	if err := c.Stop("move/g1/stop/wakapi", "g1", Workload{Name: "wakapi"}); err != nil {
		t.Fatal(err)
	}
	if !r.didRun("docker stop wakapi") {
		t.Fatalf("commands run: %v", r.ran)
	}
	e := mustRead(t, j)[0]
	if e.Undo == nil || e.Undo.Kind != journal.UndoStartContainer || e.Undo.Args["container"] != "wakapi" {
		t.Errorf("undo = %+v", e.Undo)
	}
}

// A step that already succeeded is not repeated, which is what makes a failed
// move resumable rather than restartable.
func TestStopIsNotRepeatedOnResume(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker service inspect api --format {{.Spec.Mode.Replicated.Replicas}}": "2\n",
	}}
	c, _ := newControl(t, r)

	step := "move/g1/stop/api"
	if err := c.Stop(step, "g1", Workload{Name: "api", Swarm: true}); err != nil {
		t.Fatal(err)
	}
	before := len(r.ran)
	if err := c.Stop(step, "g1", Workload{Name: "api", Swarm: true}); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != before {
		t.Errorf("the second run touched Docker again: %v", r.ran[before:])
	}
}

// A failure is recorded as failed, with the reason, and returned - a move that
// cannot stop writes must not go on to copy data.
func TestAFailedStopIsRecordedAndReturned(t *testing.T) {
	r := &fakeRunner{
		replies: map[string]string{"docker service inspect api --format {{.Spec.Mode.Replicated.Replicas}}": "1\n"},
		errs:    map[string]error{"docker service scale --detach api=0": fmt.Errorf("daemon refused")},
	}
	c, j := newControl(t, r)

	err := c.Stop("move/g1/stop/api", "g1", Workload{Name: "api", Swarm: true})
	if err == nil || !strings.Contains(err.Error(), "daemon refused") {
		t.Fatalf("err = %v", err)
	}
	e := mustRead(t, j)[0]
	if e.Result != journal.Failed || e.Error == "" || e.Undo != nil {
		t.Errorf("entry = %+v", e)
	}
	// And it is not marked done, so a resume tries again.
	if j.Done("move/g1/stop/api") {
		t.Error("a failed step must be retried")
	}
}

// A service Docker does not know about is an error, not a scale that silently
// does nothing.
func TestStoppingAnUnknownServiceFails(t *testing.T) {
	r := &fakeRunner{errs: map[string]error{
		"docker service inspect gone --format {{.Spec.Mode.Replicated.Replicas}}": fmt.Errorf("no such service"),
	}}
	c, _ := newControl(t, r)
	if err := c.Stop("s", "g1", Workload{Name: "gone", Swarm: true}); err == nil {
		t.Error("expected an error")
	}
}

func TestStartPutsAServiceBackToItsCount(t *testing.T) {
	r := &fakeRunner{}
	c, _ := newControl(t, r)
	if err := c.Start(Workload{Name: "api", Swarm: true}, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(Workload{Name: "wakapi"}, 0); err != nil {
		t.Fatal(err)
	}
	if !r.didRun("docker service scale --detach api=3") || !r.didRun("docker start wakapi") {
		t.Errorf("commands run: %v", r.ran)
	}
}

func mustRead(t *testing.T, j *journal.Journal) []journal.Entry {
	t.Helper()
	entries, err := journal.Read(j.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no journal entries")
	}
	return entries
}
