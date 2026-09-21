package dokploy

import (
	"encoding/base64"
	"fmt"
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

	return CutoverDeps{
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
		StartCaddy: func() error { return nil },
	}, runner, caddy
}

// Cutover only changes who holds the ports, and everything expensive happens
// before that so the refused-connection window is as short as it can be.
func TestCutoverImportsCertificatesBeforeTakingThePorts(t *testing.T) {
	d, runner, caddy := cutoverFixture(t)

	var startedAfterStop bool
	d.StartCaddy = func() error {
		startedAfterStop = runner.didRun("docker service scale --detach dokploy-traefik=0")
		return nil
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
