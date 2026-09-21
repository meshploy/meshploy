package dokploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

type fakeFinishAPI struct {
	revoked bool
	err     error
}

func (f *fakeFinishAPI) RevokeMigrationAgent() error {
	if f.err != nil {
		return f.err
	}
	f.revoked = true
	return nil
}

// finishable is a server where everything moved and the ports changed hands,
// with one workload of somebody else's running beside it.
func finishable(t *testing.T) (FinishDeps, *fakeRunner, *journal.Journal) {
	t.Helper()
	plan, src := smallPlan(t)
	src.Docker.Containers = []migrate.Container{
		{Name: "dokploy-traefik", Image: "traefik:v3", State: "running"},
		{Name: "wakapi", Image: "wakapi:latest", State: "running"},
	}
	src.Docker.Volumes = []migrate.Volume{
		{Name: "db-xyz-data", MB: 400},
		{Name: "dokploy-postgres-database", MB: 90},
		{Name: "wakapi-data", MB: 12},
	}

	dir := t.TempDir()
	j, err := journal.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })

	group := Group{ID: "g1", Name: "web", Members: []GroupMember{
		{Kind: "application", ID: "a1", Name: "web"},
		{Kind: "database", ID: "d1", Name: "db"},
	}}
	plan.Groups = []Group{group}
	for _, m := range group.Members {
		if err := j.Append(journal.Entry{Step: fmt.Sprintf("move/g1/start/%s", m.ID), Group: "g1",
			Action: "start-service", Result: journal.OK}); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Append(journal.Entry{Step: "cutover/stop-edge", Action: "stop-container", Result: journal.OK}); err != nil {
		t.Fatal(err)
	}

	etc := filepath.Join(t.TempDir(), "dokploy")
	if err := os.MkdirAll(etc, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	return FinishDeps{Plan: plan, Source: src, Runner: runner, Journal: j,
		API: &fakeFinishAPI{}, EtcDir: etc}, runner, j
}

// Finish removes what it can name - the platform's own parts and the copies of
// what moved - and nothing else. A workload nobody migrated was never the
// migration's to take.
func TestFinishRemovesThePlatformAndLeavesEverythingElse(t *testing.T) {
	d, runner, _ := finishable(t)

	out, err := Finish(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failures) != 0 {
		t.Fatalf("failures: %v", out.Failures)
	}
	if !runner.didRun("docker service rm web-abc") || !runner.didRun("docker service rm db-xyz") {
		t.Errorf("the migrated copies should be gone: %v", runner.ran)
	}
	if !runner.didRun("docker rm -f dokploy-traefik") {
		t.Errorf("the old edge should be gone: %v", runner.ran)
	}
	if !runner.didRun("docker network rm dokploy-network") {
		t.Errorf("the network should be gone: %v", runner.ran)
	}
	for _, cmd := range runner.ran {
		if strings.Contains(cmd, "wakapi") {
			t.Errorf("something that was never migrated was removed: %s", cmd)
		}
	}
	if _, err := os.Stat(d.EtcDir); !os.IsNotExist(err) {
		t.Errorf("the configuration directory should be gone: %v", err)
	}
	if !out.AgentGone {
		t.Error("the migration's principal should have been revoked")
	}
}

// A volume is the one thing here that cannot be rebuilt from anywhere else, so
// it stays unless the operator says otherwise - and even then, only the ones
// that belong to what was migrated.
func TestFinishKeepsVolumesUnlessAsked(t *testing.T) {
	d, runner, _ := finishable(t)

	out, err := Finish(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range runner.ran {
		if strings.Contains(cmd, "volume rm") {
			t.Fatalf("a volume was removed without being asked for: %s", cmd)
		}
	}
	if len(out.Removed.Volumes) != 2 {
		t.Errorf("volumes in scope = %+v, want the database's and the platform's", out.Removed.Volumes)
	}
	for _, v := range out.Removed.Volumes {
		if v.Name == "wakapi-data" {
			t.Error("a volume of something that was never migrated is not in scope")
		}
	}

	// Asked for, they go.
	d2, runner2, _ := finishable(t)
	d2.RemoveVolumes = true
	if _, err := Finish(d2); err != nil {
		t.Fatal(err)
	}
	if !runner2.didRun("docker volume rm db-xyz-data") || !runner2.didRun("docker volume rm dokploy-postgres-database") {
		t.Errorf("the volumes should have gone: %v", runner2.ran)
	}
	if runner2.didRun("docker volume rm wakapi-data") {
		t.Error("somebody else's data is never removed")
	}
}

// Finish is refused while anything on the server is still the old platform's to
// serve: removing it would take that down.
func TestFinishRefusesWhileTheOldPlatformStillServes(t *testing.T) {
	// A group that has not moved.
	d, runner, _ := finishable(t)
	d.Plan.Groups = append(d.Plan.Groups, Group{ID: "g2", Name: "shop",
		Members: []GroupMember{{Kind: "application", ID: "a2", Name: "shop"}}})

	_, err := Finish(d)
	if err == nil || !strings.Contains(err.Error(), "shop has not moved") {
		t.Fatalf("err = %v", err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("nothing should have been removed: %v", runner.ran)
	}

	// The ports still held by the old edge.
	plan, src := smallPlan(t)
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	plan.Groups = []Group{{ID: "g1", Name: "web", Members: []GroupMember{{Kind: "application", ID: "a1", Name: "web"}}}}
	_ = j.Append(journal.Entry{Step: "move/g1/start/a1", Group: "g1", Action: "start-service", Result: journal.OK})

	runner2 := &fakeRunner{}
	if _, err := Finish(FinishDeps{Plan: plan, Source: src, Runner: runner2, Journal: j}); err == nil ||
		!strings.Contains(err.Error(), "ports have not been handed over") {
		t.Fatalf("err = %v", err)
	}
	if len(runner2.ran) != 0 {
		t.Errorf("nothing should have been removed: %v", runner2.ran)
	}
}

// After finish there is nothing to go back to, and the journal says so - which
// is what rollback reads before it replays anything.
func TestFinishIsRecordedAndCannotHappenTwice(t *testing.T) {
	d, _, j := finishable(t)

	if Finished(j) {
		t.Fatal("a migration that has not finished should not say it has")
	}
	if _, err := Finish(d); err != nil {
		t.Fatal(err)
	}
	if !Finished(j) {
		t.Error("finishing should be recorded")
	}
	if _, err := Finish(d); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Errorf("err = %v", err)
	}
}

// One thing that will not go does not leave the rest running: the operator gets
// a list of what is left to do by hand.
func TestFinishCarriesOnPastAFailureAndReportsIt(t *testing.T) {
	d, runner, _ := finishable(t)
	runner.errs = map[string]error{
		"docker network rm dokploy-network": fmt.Errorf("network has active endpoints"),
	}

	out, err := Finish(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "active endpoints") {
		t.Fatalf("failures = %v", out.Failures)
	}
	if !runner.didRun("docker rm -f dokploy-traefik") {
		t.Error("the rest should still have been removed")
	}
	if _, statErr := os.Stat(d.EtcDir); !os.IsNotExist(statErr) {
		t.Error("the configuration directory should still have been removed")
	}
}
