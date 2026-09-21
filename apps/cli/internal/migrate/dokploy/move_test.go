package dokploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// fakeMoveAPI is Meshploy during a move, recording the order things happened
// in: the order is the safety property, not the individual calls.
type fakeMoveAPI struct {
	started      []string
	stopped      []string
	published    []string
	paused       []string
	tcpPublished []string
	tcpPaused    []string
	status       map[string]string
	fail         map[string]error
}

func newMoveAPI() *fakeMoveAPI {
	return &fakeMoveAPI{status: map[string]string{}, fail: map[string]error{}}
}

func (f *fakeMoveAPI) StartService(projectID, serviceID string) error {
	if err := f.fail["start:"+serviceID]; err != nil {
		return err
	}
	f.started = append(f.started, serviceID)
	if _, set := f.status[serviceID]; !set {
		f.status[serviceID] = "running"
	}
	return nil
}

func (f *fakeMoveAPI) StopService(projectID, serviceID string) error {
	if err := f.fail["stop:"+serviceID]; err != nil {
		return err
	}
	f.stopped = append(f.stopped, serviceID)
	return nil
}

func (f *fakeMoveAPI) ServiceStatus(projectID, serviceID string) (string, error) {
	if s, ok := f.status[serviceID]; ok {
		return s, nil
	}
	return "stopped", nil
}

func (f *fakeMoveAPI) PublishRoute(projectID, routeID string) error {
	if err := f.fail["publish:"+routeID]; err != nil {
		return err
	}
	f.published = append(f.published, routeID)
	return nil
}

func (f *fakeMoveAPI) PauseRoute(projectID, routeID string) error {
	f.paused = append(f.paused, routeID)
	return nil
}

func (f *fakeMoveAPI) PublishTCPRoute(projectID, routeID string) error {
	if err := f.fail["publish-tcp:"+routeID]; err != nil {
		return err
	}
	f.tcpPublished = append(f.tcpPublished, routeID)
	return nil
}

func (f *fakeMoveAPI) PauseTCPRoute(projectID, routeID string) error {
	f.tcpPaused = append(f.tcpPaused, routeID)
	return nil
}

type fakeProbe struct {
	err  error
	seen []string
}

func (p *fakeProbe) Probe(host string) error {
	p.seen = append(p.seen, host)
	return p.err
}

// movable builds a prepared, stateless group: one app, one domain, stage 1
// already run so the journal knows what it created.
func movable(t *testing.T) (MoveDeps, *fakeMoveAPI, *fakeRunner, string) {
	t.Helper()
	plan, src := smallPlan(t)

	dir := t.TempDir()
	j, err := journal.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })

	api := newFakeAPI()
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: j}); err != nil {
		t.Fatal(err)
	}

	// The app's group, without the database, so it is stateless.
	group := Group{ID: "g-web", Name: "web", CanMove: true,
		Members: []GroupMember{{Kind: "application", ID: "a1", Name: "web", Project: "Acme · production"}}}

	dynamic := t.TempDir()
	if err := os.WriteFile(filepath.Join(dynamic, "web-abc.yml"), []byte(appDynamicFile), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{replies: map[string]string{
		"docker service inspect web-abc --format {{.Spec.Mode.Replicated.Replicas}}": "1\n",
	}}
	moveAPI := newMoveAPI()

	return MoveDeps{
		Plan:    plan,
		Group:   group,
		API:     moveAPI,
		Edge:    EdgeSwitcher{DynamicDir: dynamic, Target: "http://172.17.0.1:8081", Journal: j},
		Control: Control{Runner: runner, Journal: j},
		Probe:   &fakeProbe{},
		Journal: j,
		Sleep:   func(time.Duration) {},
	}, moveAPI, runner, dynamic
}

// The order is the safety property: Dokploy's copy stops before Meshploy's
// starts, so the same application never runs twice against the same data, and
// the domain switches only once the new copy is healthy.
func TestMoveStopsDokployBeforeStartingMeshploy(t *testing.T) {
	d, api, runner, dynamic := movable(t)

	out, err := Move(d)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !out.Moved || out.Downtime == "" {
		t.Fatalf("result = %+v", out)
	}

	if !runner.didRun("docker service scale --detach web-abc=0") {
		t.Errorf("Dokploy's copy was not stopped: %v", runner.ran)
	}
	if len(api.started) != 1 {
		t.Fatalf("started = %v", api.started)
	}
	// The domain moved, and its route was published only after that.
	switched, _ := os.ReadFile(filepath.Join(dynamic, "web-abc.yml"))
	if !strings.Contains(string(switched), "http://172.17.0.1:8081") {
		t.Errorf("the domain was not switched:\n%s", switched)
	}
	if len(api.published) != 1 {
		t.Errorf("published = %v", api.published)
	}
	if probe := d.Probe.(*fakeProbe); len(probe.seen) != 1 || probe.seen[0] != "web.example.com" {
		t.Errorf("probed %v", probe.seen)
	}
}

// A half-moved group never serves: a failure puts everything back.
func TestAFailedMoveUndoesItself(t *testing.T) {
	d, api, runner, dynamic := movable(t)
	// The copy starts but never becomes healthy.
	api.status["svc-2"] = "stopped"
	api.status["svc-3"] = "stopped"
	d.HealthTimeout = 10 * time.Millisecond

	out, err := Move(d)
	if err == nil {
		t.Fatal("expected the move to fail")
	}
	if out.Moved {
		t.Error("a failed move must not report as moved")
	}
	if !out.UndoneAfterFailure {
		t.Errorf("the group should have put itself back: %+v", out)
	}

	// Dokploy is serving again, Meshploy's copy is stopped, and the domain was
	// never switched.
	if !runner.didRun("docker service scale --detach web-abc=1") {
		t.Errorf("Dokploy's copy was not started again: %v", runner.ran)
	}
	if len(api.stopped) == 0 {
		t.Error("Meshploy's copy was left running")
	}
	if len(api.published) != 0 {
		t.Errorf("a route was published during a failed move: %v", api.published)
	}
	after, _ := os.ReadFile(filepath.Join(dynamic, "web-abc.yml"))
	if string(after) != appDynamicFile {
		t.Errorf("the edge was left switched:\n%s", after)
	}
}

// A domain that does not answer after the move is a failure, not a success with
// a warning - and the group goes back.
func TestADomainThatDoesNotAnswerFailsTheMove(t *testing.T) {
	d, api, runner, dynamic := movable(t)
	d.Probe = &fakeProbe{err: fmt.Errorf("502 from the edge")}

	out, err := Move(d)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
	if !out.UndoneAfterFailure {
		t.Errorf("result = %+v", out)
	}
	if len(api.paused) != 1 {
		t.Errorf("the route should have been paused again: %v", api.paused)
	}
	if !runner.didRun("docker service scale --detach web-abc=1") {
		t.Error("Dokploy's copy should be serving again")
	}
	after, _ := os.ReadFile(filepath.Join(dynamic, "web-abc.yml"))
	if string(after) != appDynamicFile {
		t.Error("the domain should have been given back")
	}
}

// A group carrying data is refused when the move has no way to copy it, rather
// than moving the application away from its database.
func TestAGroupWithDataIsRefusedWithoutAWayToCopyIt(t *testing.T) {
	d, _, _, _ := movable(t)
	d.Group.Data = []GroupData{{Name: "db", MB: 120, Move: "dump"}}

	if _, err := Move(d); err == nil || !strings.Contains(err.Error(), "no way to copy it") {
		t.Fatalf("err = %v", err)
	}
}

// A group whose decisions are not made cannot move, and says which.
func TestAGroupThatCannotMoveIsRefusedWithItsBlockers(t *testing.T) {
	d, _, _, _ := movable(t)
	d.Group.CanMove = false
	d.Group.Blockers = []string{"the certificate question is unanswered"}

	_, err := Move(d)
	if err == nil || !strings.Contains(err.Error(), "certificate question") {
		t.Fatalf("err = %v", err)
	}
}

// Without stage 1 there is nothing to start, and the move says so rather than
// stopping Dokploy and leaving the server down.
func TestMovingWithoutPrepareFails(t *testing.T) {
	plan, _ := smallPlan(t)
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	api := newMoveAPI()
	d := MoveDeps{
		Plan:    plan,
		Group:   Group{ID: "g", Name: "web", CanMove: true, Members: []GroupMember{{ID: "a1", Name: "web"}}},
		API:     api,
		Control: Control{Runner: &fakeRunner{replies: map[string]string{"docker service inspect web-abc --format {{.Spec.Mode.Replicated.Replicas}}": "1\n"}}, Journal: j},
		Journal: j,
		Sleep:   func(time.Duration) {},
	}
	_, err = Move(d)
	if err == nil || !strings.Contains(err.Error(), "run prepare first") {
		t.Fatalf("err = %v", err)
	}
}

// Re-running a move that already succeeded does not stop Dokploy a second time
// or start a second copy.
func TestMoveIsResumable(t *testing.T) {
	d, api, runner, _ := movable(t)
	if _, err := Move(d); err != nil {
		t.Fatal(err)
	}
	ranBefore, startedBefore := len(runner.ran), len(api.started)

	if _, err := Move(d); err != nil {
		t.Fatal(err)
	}
	if len(api.started) != startedBefore {
		t.Errorf("started again: %v", api.started)
	}
	if len(runner.ran) != ranBefore {
		t.Errorf("touched Docker again: %v", runner.ran[ranBefore:])
	}
}

// A group carrying data moves in one order and only that order: the
// application stops before anything is read, the database is dumped while it
// is still running, it stops only then, and Meshploy's database is running
// before the application that reads it comes up.
func TestAStatefulGroupMovesInTheOnlySafeOrder(t *testing.T) {
	d, _, runner, _ := movable(t)
	mover, _, stream, kube, _ := statefulGroup(t)

	// One group holding both: the application of the move fixture, and the
	// database of the data fixture.
	d.Group = Group{ID: "g-shop", Name: "shop", CanMove: true,
		Members: []GroupMember{
			{Kind: "application", ID: "a1", Name: "web", Project: "Acme · production"},
			{Kind: "database", ID: "d1", Name: "db", Project: "Acme · production"},
		},
		Data: []GroupData{{Name: "db", MB: 120, Move: "dump and restore"}}}

	// The mover works against the move's own journal and ids, as the command
	// wires it.
	mover.Journal, mover.Dir = d.Journal, d.Journal.Dir()
	mover.Plan = d.Plan
	for i := range mover.Plan.Items {
		if mover.Plan.Items[i].ID == "d1" {
			mover.Plan.Items[i].Details["data_mb"] = "120"
			mover.Plan.Items[i].Details["data_move"] = "dump and restore"
		}
	}
	d.Data = &mover
	runner.replies["docker service inspect db-xyz --format {{.Spec.Mode.Replicated.Replicas}}"] = "1\n"

	if _, err := Move(d); err != nil {
		t.Fatalf("%v", err)
	}

	// The journal is one ordered record of everything that happened, across
	// Docker, the data step and Meshploy - so it is what the order is asserted
	// on, rather than three fakes that cannot be compared with each other.
	var steps []string
	for _, e := range mustRead(t, d.Journal) {
		if e.Group == "g-shop" && e.Result == journal.OK {
			steps = append(steps, strings.TrimPrefix(e.Step, "move/g-shop/"))
		}
	}
	want := []string{"stop/a1", "dump/d1", "stop/d1", "start/d1", "data/d1", "start/a1"}
	if got := strings.Join(steps, " "); !strings.HasPrefix(got, strings.Join(want, " ")) {
		t.Fatalf("the group moved in the wrong order:\n got %s\nwant %s first", got, strings.Join(want, " "))
	}
	if !stream.didRun("pg_dump") || !kube.didRun("pg_restore") {
		t.Errorf("the data did not move: %v / %v", stream.ran, kube.ran)
	}
	if !runner.didRun("docker service scale --detach db-xyz=0") {
		t.Errorf("Dokploy's database was not stopped: %v", runner.ran)
	}
}

// The port a database was published on opens when its group moves, and closes
// again if the group goes back: until then Dokploy still holds that port.
func TestMovingAGroupOpensItsPublishedDatabasePort(t *testing.T) {
	d, api, runner, _ := movable(t)
	// Stage 1 created a TCP route for the database, as it does for one Dokploy
	// published.
	if err := d.Journal.Append(journal.Entry{Step: "prepare/tcp/d1", Action: "create-tcp-route",
		Target: "db on :5433", Result: journal.OK, Created: "tcp-1"}); err != nil {
		t.Fatal(err)
	}
	d.Group = Group{ID: "g-db", Name: "db", CanMove: true,
		Members: []GroupMember{{Kind: "database", ID: "d1", Name: "db", Project: "Acme · production"}}}
	runner.replies["docker service inspect db-xyz --format {{.Spec.Mode.Replicated.Replicas}}"] = "1\n"

	if _, err := Move(d); err != nil {
		t.Fatal(err)
	}
	if len(api.tcpPublished) != 1 || api.tcpPublished[0] != "tcp-1" {
		t.Fatalf("published = %v", api.tcpPublished)
	}

	// And a rollback closes it.
	entries, _ := journal.Read(d.Journal.Dir())
	res := Rollback{Runner: runner, Meshploy: moveRollback{api}, Journal: d.Journal}.
		Replay(journal.Undoable(entries, "g-db"))
	if len(res.Failures) != 0 {
		t.Fatalf("failures: %v", res.Failures)
	}
	if len(api.tcpPaused) != 1 || api.tcpPaused[0] != "tcp-1" {
		t.Errorf("paused = %v", api.tcpPaused)
	}
}
