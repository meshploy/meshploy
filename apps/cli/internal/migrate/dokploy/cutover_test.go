package dokploy

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

func cutoverFixture(t *testing.T) (CutoverDeps, *fakeRunner, string) {
	t.Helper()
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })

	dir := t.TempDir()
	traefik := filepath.Join(dir, "traefik")
	if err := os.MkdirAll(filepath.Join(traefik, "dynamic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(traefik, "traefik.yml"), []byte("providers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(traefik, "dynamic", "web.yml"), []byte(appDynamicFile), 0o644); err != nil {
		t.Fatal(err)
	}

	acme := filepath.Join(dir, "acme.json")
	crt, key := issue(t, "web.example.com", time.Now().Add(60*24*time.Hour))
	var c AcmeCertificate
	c.Domain.Main = "web.example.com"
	c.Certificate = base64.StdEncoding.EncodeToString(crt)
	c.Key = base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(acme, acmeJSON(t, c), 0o600); err != nil {
		t.Fatal(err)
	}

	caddy := filepath.Join(dir, "caddy-data")
	runner := &fakeRunner{replies: map[string]string{
		"docker service inspect dokploy-traefik --format {{.Spec.Mode.Replicated.Replicas}}": "1\n",
	}}

	// The port, modelled the way a real handover uses it: the old edge holds it
	// until it is stopped, and ours takes it when it starts. A fixture that
	// simply left something listening the whole time could not tell a cutover
	// that worked from one where the old edge never let go - which is the
	// failure a real server produced.
	//
	// So take an address, free it, and let StartCaddy be the thing that binds.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	var edge net.Listener
	t.Cleanup(func() {
		if edge != nil {
			edge.Close()
		}
	})

	return CutoverDeps{
		EdgeAddr: addr,
		Plan: Plan{Items: []Item{
			{Kind: "domain", Name: "web.example.com", Verdict: Moves},
		}},
		Runner:     runner,
		Journal:    j,
		Probe:      &fakeProbe{},
		Edge:       EdgeHolder{Kind: "swarm", Name: "dokploy-traefik"},
		AcmePath:   acme,
		TraefikDir: traefik,
		CaddyData:  caddy,
		StartCaddy: func() error {
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			edge = l
			return nil
		},
	}, runner, caddy
}

// Cutover only changes who holds the ports, and everything expensive happens
// before that so the refused-connection window is as short as it can be.
func TestCutoverImportsCertificatesBeforeTakingThePorts(t *testing.T) {
	d, runner, caddy := cutoverFixture(t)

	var startedAfterStop bool
	// Wrapped, not replaced: the fixture's StartCaddy is what binds the port,
	// and a cutover is not finished until something of ours is on it.
	bind := d.StartCaddy
	d.StartCaddy = func() error {
		startedAfterStop = runner.didRun("docker service scale --detach dokploy-traefik=0")
		return bind()
	}

	out, err := Cutover(d)
	if err != nil {
		t.Fatalf("%v: %+v", err, out)
	}
	if len(out.CertificatesImported) != 1 {
		t.Errorf("imported = %v, skipped = %v", out.CertificatesImported, out.CertificatesSkipped)
	}
	// Written where Caddy looks, before it needed them.
	crt := filepath.Join(CaddyDataDir(caddy), "certificates", CaddyIssuer, "web.example.com", "web.example.com.crt")
	if _, err := os.Stat(crt); err != nil {
		t.Errorf("certificate not written: %v", err)
	}
	if !startedAfterStop {
		t.Error("Caddy started before the old edge was stopped")
	}
	if out.Downtime == "" {
		t.Error("the window should be reported")
	}
}

// The backup is taken first, because the value of a backup is entirely in
// having taken it early.
func TestCutoverBacksUpTheEdgeFirst(t *testing.T) {
	d, _, _ := cutoverFixture(t)
	if _, err := Cutover(d); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme.json", filepath.Join("edge", "traefik.yml"), filepath.Join("edge", "dynamic", "web.yml")} {
		if _, err := os.Stat(filepath.Join(d.Journal.Dir(), "backup", want)); err != nil {
			t.Errorf("%s was not backed up: %v", want, err)
		}
	}
}

// If ours does not come up, the old edge goes back at once rather than the
// server staying dark while somebody reads an error.
func TestAFailedStartPutsTheOldEdgeBack(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.StartCaddy = func() error { return fmt.Errorf("caddy refused to start") }

	out, err := Cutover(d)
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !out.RolledBack {
		t.Errorf("result = %+v", out)
	}
	if !runner.didRun("docker service scale --detach dokploy-traefik=1") {
		t.Errorf("the old edge was not restarted: %v", runner.ran)
	}
}

// A domain that does not answer once Caddy holds the ports is reported by name:
// that is what the operator decides a rollback on.
func TestCutoverReportsDomainsThatDoNotAnswer(t *testing.T) {
	d, _, _ := cutoverFixture(t)
	d.Probe = &fakeProbe{err: fmt.Errorf("connection refused")}

	out, err := Cutover(d)
	if err == nil {
		t.Fatal("expected a failure")
	}
	if len(out.Unreachable) != 1 || out.Unreachable[0] != "web.example.com" {
		t.Errorf("unreachable = %v", out.Unreachable)
	}
	// The ports have changed hands: this is a decision to make, not an
	// automatic undo, because going back also means taking the moved domains
	// off Meshploy.
	if out.RolledBack {
		t.Error("cutover should not undo itself over an unreachable domain")
	}
}

// A certificate store that cannot be read does not stop the cutover: Caddy
// issuing certificates is the normal path and always works.
func TestAnUnreadableAcmeStoreDoesNotStopCutover(t *testing.T) {
	d, _, _ := cutoverFixture(t)
	if err := os.WriteFile(d.AcmePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Cutover(d)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if out.CertificatesSkipped["*"] == "" {
		t.Error("the reason should be reported")
	}
}

// A custom edge is a systemd unit, stopped and restartable the same way.
func TestACustomEdgeIsStoppedAsAUnit(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.Edge = EdgeHolder{Kind: "systemd", Name: "caddy"}
	d.StartCaddy = func() error { return fmt.Errorf("no") }

	out, _ := Cutover(d)
	if !runner.didRun("systemctl stop caddy") {
		t.Errorf("commands = %v", runner.ran)
	}
	if !out.RolledBack || !runner.didRun("systemctl start caddy") {
		t.Errorf("the unit was not started again: %v", runner.ran)
	}
}

// An edge nothing is known about stops the cutover before the ports change
// hands.
func TestAnUnknownEdgeRefuses(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.Edge = EdgeHolder{}
	if _, err := Cutover(d); err == nil || !strings.Contains(err.Error(), "nothing known holds") {
		t.Fatalf("err = %v", err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("it touched the server anyway: %v", runner.ran)
	}
}

// A custom edge keeps its certificates elsewhere, and a server may never have
// had any. Nothing to copy is not a reason to refuse the cutover.
func TestCutoverWithoutACertificateStore(t *testing.T) {
	d, _, _ := cutoverFixture(t)
	if err := os.Remove(d.AcmePath); err != nil {
		t.Fatal(err)
	}
	out, err := Cutover(d)
	if err != nil {
		t.Fatalf("%v: %+v", err, out)
	}
	if len(out.CertificatesImported) != 0 {
		t.Errorf("imported = %v", out.CertificatesImported)
	}
}

// The old platform's own server is stopped before its edge: while it runs it
// can put that edge back, and a recreated edge races ours for 80 and 443 on the
// next restart. Its data is untouched, and the undo starts it again.
func TestCutoverStopsTheOldControlPlaneBeforeItsEdge(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.ControlPlane = ControlPlane{Kind: "swarm", Name: "dokploy"}
	runner.replies["docker service inspect dokploy --format {{.ID}}"] = "abc123\n"
	runner.replies["docker service inspect dokploy --format {{.Spec.Mode.Replicated.Replicas}}"] = "1\n"

	if _, err := Cutover(d); err != nil {
		t.Fatal(err)
	}
	stopPlane, stopEdge := -1, -1
	for i, cmd := range runner.ran {
		switch {
		case strings.Contains(cmd, "service scale --detach dokploy=0"):
			stopPlane = i
		case strings.Contains(cmd, "dokploy-traefik"):
			if stopEdge == -1 {
				stopEdge = i
			}
		}
	}
	if stopPlane == -1 {
		t.Fatalf("the control plane was not stopped: %v", runner.ran)
	}
	if stopEdge != -1 && stopPlane > stopEdge {
		t.Errorf("it must stop before the edge, not after: %v", runner.ran)
	}

	// And it can be put back: the journal knows what it was.
	entries, _ := journal.Read(d.Journal.Dir())
	var found bool
	for _, e := range entries {
		if e.Step == "cutover/stop-control-plane" && e.Undo != nil && e.Undo.Args["replicas"] == "1" {
			found = true
		}
	}
	if !found {
		t.Error("stopping the control plane should be recorded with how to start it again")
	}
}

// A server whose old console is already gone is exactly the state cutover is
// aiming for, so there is nothing to stop and nothing to complain about.
func TestCutoverDoesNotMindAMissingControlPlane(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.ControlPlane = ControlPlane{Kind: "swarm", Name: "dokploy"}
	runner.errs = map[string]error{
		"docker service inspect dokploy --format {{.ID}}": fmt.Errorf("no such service: dokploy"),
	}

	if _, err := Cutover(d); err != nil {
		t.Fatalf("a missing control plane is not a failure: %v", err)
	}
}

// planWith is a plan of two groups: one that can move, one that cannot.
func planWith(t *testing.T, movableMoved bool) (CutoverDeps, *fakeRunner) {
	t.Helper()
	d, runner, _ := cutoverFixture(t)
	d.Plan.Items = append(d.Plan.Items,
		Item{Kind: "domain", Name: "shop.example.com/", Verdict: Moves, Details: map[string]string{"application_id": "a1"}},
		Item{Kind: "domain", Name: "legacy.example.com/", Verdict: NeedsYou, Details: map[string]string{"application_id": "a2"}},
	)
	d.Plan.Groups = []Group{
		{ID: "g1", Name: "shop", CanMove: true, Members: []GroupMember{{Kind: "application", ID: "a1", Name: "shop"}}},
		{ID: "g2", Name: "legacy", CanMove: false, Members: []GroupMember{{Kind: "application", ID: "a2", Name: "legacy"}},
			Blockers: []string{"its certificate was uploaded by hand"}},
	}
	if movableMoved {
		if err := d.Journal.Append(journal.Entry{Step: "move/g1/start/a1", Group: "g1",
			Action: "start-service", Result: journal.OK}); err != nil {
			t.Fatal(err)
		}
	}
	return d, runner
}

// A group that can still move has no business being left behind: moving it
// costs only the downtime it already carries.
func TestCutoverRefusesWhileAnythingCanStillMove(t *testing.T) {
	d, runner := planWith(t, false)

	_, err := Cutover(d)
	if err == nil || !strings.Contains(err.Error(), "shop can still move") {
		t.Fatalf("err = %v", err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("nothing should have happened: %v", runner.ran)
	}
}

// A group that cannot move is left where it is, and the domains it loses are
// named rather than refused - one unanswerable question used to mean a
// migration that could never finish.
func TestCutoverNamesTheDomainsThatStopBeingServed(t *testing.T) {
	d, runner := planWith(t, true)

	out, err := Cutover(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Unserved) != 1 || out.Unserved[0] != "legacy.example.com" {
		t.Fatalf("unserved = %v", out.Unserved)
	}
	if !runner.didRun("docker service scale --detach dokploy-traefik=0") {
		t.Errorf("the ports should still have changed hands: %v", runner.ran)
	}
}

// The domains of a group are what it serves, whatever the verdict on them: a
// workload left behind keeps its hostnames until the ports change hands.
func TestGroupDomainsReadsThePlan(t *testing.T) {
	d, _ := planWith(t, true)
	got := GroupDomains(d.Plan, d.Plan.Groups[1])
	if len(got) != 1 || got[0] != "legacy.example.com" {
		t.Errorf("got %v", got)
	}
}

// Starting our edge is not the same as our edge serving. `docker compose up`
// succeeds when the container is created, and on a server whose plan has no
// domains that was the only thing cutover checked before saying the ports had
// changed hands - so an edge that never listened was reported as a success.
func TestCutoverPutsTheOldEdgeBackIfOursNeverListens(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	// Nothing is listening where our edge should be.
	d.EdgeAddr, d.EdgeUpWindow = "127.0.0.1:1", 200*time.Millisecond

	out, err := Cutover(d)
	if err == nil || !strings.Contains(err.Error(), "did not take the ports") {
		t.Fatalf("err = %v", err)
	}
	if !out.RolledBack {
		t.Error("the old edge should have been put back rather than left stopped")
	}
	if !runner.didRun("docker service scale --detach dokploy-traefik=1") {
		t.Errorf("the old edge should be serving again: %v", runner.ran)
	}
}

// A server whose edge is not Traefik has no acme.json, and its certificates are
// worth exactly as much: without them Caddy asks Let's Encrypt for every domain
// at once, in the window the operator is watching.
func TestCutoverImportsCertificatesFromADirectory(t *testing.T) {
	d, _, caddy := cutoverFixture(t)
	d.AcmePath = ""
	d.Edge = EdgeHolder{Kind: "systemd", Name: "nginx.service"}

	certs := t.TempDir()
	crt, key := issue(t, "web.example.com", time.Now().Add(60*24*time.Hour))
	if err := os.WriteFile(filepath.Join(certs, "web.example.com.crt"), crt, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certs, "web.example.com.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	d.CertDir = certs

	out, err := Cutover(d)
	if err != nil {
		t.Fatalf("%v: %+v", err, out)
	}
	if len(out.CertificatesImported) != 1 {
		t.Fatalf("imported = %v, skipped = %v", out.CertificatesImported, out.CertificatesSkipped)
	}
	written := filepath.Join(CaddyDataDir(caddy), "certificates", CaddyIssuer, "web.example.com", "web.example.com.crt")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("certificate not written: %v", err)
	}
}

// Everything after the edge check is downtime. Finding out there that our edge
// cannot start means a server with nothing on 80 and 443 until the restore
// runs - which is what a rehearsal found, when the image turned out not to be
// on the host at all.
func TestCutoverRefusesBeforeTheDowntimeWhenOurEdgeIsNotReady(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.ReadyCaddy = func() error { return fmt.Errorf("image not found") }
	started := false
	d.StartCaddy = func() error { started = true; return nil }

	out, err := Cutover(d)
	if err == nil {
		t.Fatal("a cutover that cannot start our edge must refuse")
	}
	if !strings.Contains(out.Error, "not ready to take the ports") {
		t.Errorf("error should say what is wrong: %q", out.Error)
	}
	if started {
		t.Error("our edge should not have been started")
	}
	// Nothing may have been stopped: the old platform is still serving.
	if runner.didRun("docker service scale --detach dokploy-traefik=0") {
		t.Error("the old edge was stopped before the check that would have avoided it")
	}
	if runner.didRun("docker service scale --detach dokploy=0") {
		t.Error("the control plane was stopped before the check")
	}
}

// A cutover that failed and put the old edge back must record that it did.
//
// From a real server: the restore ran without a journal, so the stop stayed
// recorded as done. The next attempt skipped stopping an edge that was back up
// and holding 443, started ours against a bound socket, and reported that the
// ports had changed hands.
func TestAFailedCutoverRecordsThatItPutTheEdgeBack(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.StartCaddy = func() error { return fmt.Errorf("image not found") }

	if _, err := Cutover(d); err == nil {
		t.Fatal("a cutover whose edge will not start must fail")
	}
	if d.Journal.Done("cutover/stop-edge") {
		t.Fatal("the stop is recorded as done although the edge was put back: a retry will skip it")
	}
	if !runner.didRun("docker service scale --detach dokploy-traefik=1") {
		t.Fatalf("the old edge should have been restored: %v", runner.ran)
	}

	// The retry stops it again rather than starting ours against a held port.
	from := len(runner.ran)
	d.StartCaddy = func() error { return fmt.Errorf("still broken") }
	if _, err := Cutover(d); err == nil {
		t.Fatal("the second attempt must fail too")
	}
	if !runner.didRunAfter("docker service scale --detach dokploy-traefik=0", from) {
		t.Errorf("the retry skipped stopping the old edge: %v", runner.ran[from:])
	}
}

// Something listening is not the same as our edge listening. An old edge that
// never let go answers a dial just as well, and the handover then reports
// success while ours crash-loops behind it.
func TestCutoverWillNotCallItDoneWhileOurEdgeIsRestarting(t *testing.T) {
	d, runner, _ := cutoverFixture(t)
	d.CaddyContainer = "meshploy-caddy-1"
	d.EdgeUpWindow = 300 * time.Millisecond
	// Our container is crash-looping; something else holds the port.
	runner.replies["docker inspect -f {{.State.Status}} {{.State.Restarting}} meshploy-caddy-1"] =
		"restarting true\n"

	out, err := Cutover(d)
	if err == nil {
		t.Fatal("a cutover whose edge is not running must not report success")
	}
	if !strings.Contains(out.Error, "not running") {
		t.Errorf("the error should name what is wrong: %q", out.Error)
	}
	if !out.RolledBack {
		t.Error("the old edge should have been put back")
	}
}

// The old edge is told to stop and Swarm returns before the task is gone, so
// ours must wait for the socket rather than race it into a crash-loop.
func TestCutoverWaitsForTheOldEdgeToLetGoOfThePort(t *testing.T) {
	d, runner, _ := cutoverFixture(t)

	d.EdgeGoneWindow = 300 * time.Millisecond

	// Something is still on the port when the stop returns, and stays there.
	squatter, err := net.Listen("tcp", d.EdgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer squatter.Close()

	started := false
	d.StartCaddy = func() error { started = true; return nil }

	out, err := Cutover(d)
	if err == nil {
		t.Fatal("a cutover must not start ours while the old edge holds the port")
	}
	if started {
		t.Error("ours was started against a port that was still held")
	}
	if !strings.Contains(out.Error, "still holding") {
		t.Errorf("the error should say the old edge has not let go: %q", out.Error)
	}
	if !runner.didRun("docker service scale --detach dokploy-traefik=1") {
		t.Errorf("the old edge should have been put back: %v", runner.ran)
	}
}
