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

type fakeFallback struct {
	upstream string
	hosts    []string
	cleared  bool
}

func (f *fakeFallback) SetEdgeFallback(upstream string, hosts []string) error {
	f.upstream, f.hosts = upstream, hosts
	return nil
}

func (f *fakeFallback) ClearEdgeFallback() error { f.cleared = true; return nil }

// fallbackRollback is a rollback's Meshploy side that can clear the fallback.
type fallbackRollback struct {
	stubMeshploy
	f *fakeFallback
}

func (r fallbackRollback) ClearEdgeFallback() error { return r.f.ClearEdgeFallback() }

// Taking the edge first does not wait for anything to move, and stops nothing:
// the old edge moves to a side port, learns to trust what Meshploy's forwards,
// and every domain it served - one whose group cannot move yet included - is
// sent to it through Meshploy's edge. Putting it back undoes every part.
func TestTakingTheEdgeFirst(t *testing.T) {
	d, runner := planWith(t, false)
	conf := filepath.Join(d.TraefikDir, "traefik.yml")
	original := "entryPoints:\n  web:\n    address: :80\n  websecure:\n    address: :443\n"
	if err := os.WriteFile(conf, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	fb := &fakeFallback{}
	d.EdgeFirst, d.SidePort, d.Fallback, d.TraefikConfig = true, 18123, fb, conf
	settledAside(runner, 18123)

	out, err := Cutover(d)
	if err != nil {
		t.Fatalf("%v (%s)", err, out.Error)
	}
	want := "docker service update --detach --publish-rm mode=host,target=80 --publish-rm mode=host,target=443 --publish-add mode=host,published=18123,target=80 dokploy-traefik"
	if !runner.didRun(want) || runner.didRun("docker service scale --detach dokploy-traefik=0") {
		t.Fatalf("the old edge should move aside, not stop: %v", runner.ran)
	}
	if fb.upstream != "127.0.0.1:18123" || strings.Join(fb.hosts, ",") != "legacy.example.com,shop.example.com,web.example.com" {
		t.Fatalf("fallback = %q %v", fb.upstream, fb.hosts)
	}
	if strings.Join(out.ThroughOldEdge, ",") != "legacy.example.com,shop.example.com,web.example.com" || out.SidePort != 18123 || len(out.Unserved) != 0 {
		t.Errorf("result = %+v", out)
	}
	b, _ := os.ReadFile(conf)
	if !strings.Contains(string(b), "forwardedHeaders:\n      trustedIPs:\n        - 127.0.0.1/32") {
		t.Errorf("the old edge was not told to trust Meshploy's: %s", b)
	}

	// A move after this only publishes: the old edge's files are not touched.
	if !d.Journal.Done("cutover/fallback") {
		t.Fatal("the fallback should be recorded, for moves to know")
	}

	entries, _ := journal.Read(d.Journal.Dir())
	res := Rollback{Runner: runner, Meshploy: fallbackRollback{f: fb}, Journal: d.Journal}.Replay(journal.Undoable(entries, ""))
	if len(res.Failures) != 0 {
		t.Fatalf("rollback: %v", res.Failures)
	}
	if !fb.cleared {
		t.Error("the fallback should be cleared")
	}
	if !runner.didRun("docker service update --detach --publish-rm mode=host,target=80 --publish-add mode=host,published=80,target=80 --publish-add mode=host,published=443,target=443 dokploy-traefik") {
		t.Errorf("the old edge should get 80 and 443 back: %v", runner.ran)
	}
	if b, _ := os.ReadFile(conf); string(b) != original {
		t.Errorf("the old edge's configuration should be as it was: %s", b)
	}
}

// Only a Swarm Traefik can be moved aside today; anything else is refused
// before anything changes.
func TestTakingTheEdgeFirstNeedsASwarmEdge(t *testing.T) {
	d, runner := planWith(t, false)
	d.EdgeFirst, d.Fallback, d.TraefikConfig = true, &fakeFallback{}, "x"
	d.Edge = EdgeHolder{Kind: "container", Name: "dokploy-traefik"}
	if _, err := Cutover(d); err == nil || !strings.Contains(err.Error(), "Swarm service") {
		t.Fatalf("err = %v", err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("nothing should have run: %v", runner.ran)
	}
}

// settledAside has the fake Docker report the edge on the side port alone,
// running, as a real one does once the update has rolled out.
func settledAside(r *fakeRunner, side int) {
	r.replies["docker service inspect dokploy-traefik --format {{json .Spec.EndpointSpec.Ports}}"] =
		`[{"TargetPort":80,"PublishedPort":80,"PublishMode":"host"},{"TargetPort":443,"PublishedPort":443,"PublishMode":"host"}]`
	r.replies["docker service inspect dokploy-traefik --format {{range .Endpoint.Ports}}{{.PublishedPort}}->{{.TargetPort}} {{end}}"] =
		fmt.Sprintf("%d->80 ", side)
	r.replies["docker service ps dokploy-traefik --filter desired-state=running --format {{.CurrentState}}"] = "Running 2 seconds ago"
}

// An edge still holding 80 after the update is a cutover that stops there, not
// one that starts Caddy against it and leaves the old edge crash-looping.
func TestAnEdgeThatKeeps80IsNotTakenAside(t *testing.T) {
	d, runner := planWith(t, false)
	conf := filepath.Join(d.TraefikDir, "traefik.yml")
	if err := os.WriteFile(conf, []byte("entryPoints:\n  web:\n    address: :80\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.EdgeFirst, d.SidePort, d.Fallback, d.TraefikConfig = true, 18123, &fakeFallback{}, conf
	d.EdgeGoneWindow = 50 * time.Millisecond
	settledAside(runner, 18123)
	runner.replies["docker service inspect dokploy-traefik --format {{range .Endpoint.Ports}}{{.PublishedPort}}->{{.TargetPort}} {{end}}"] = "18123->80 443->443 80->80 "

	started := false
	d.StartCaddy = func() error { started = true; return nil }

	out, err := Cutover(d)
	if err == nil {
		t.Fatal("the cutover should stop at the edge")
	}
	if !strings.Contains(err.Error()+out.Error, "did not settle") {
		t.Errorf("the reason should say the edge did not settle: %v / %s", err, out.Error)
	}
	if started {
		t.Fatal("Caddy was started against an edge still on 80")
	}
}
