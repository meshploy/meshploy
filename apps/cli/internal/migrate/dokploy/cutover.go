package dokploy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Stage 3: hand over ports 80 and 443.
//
// By now every group has moved and every domain already points at Meshploy
// through the old edge. Cutover only changes *who holds the ports*, which is
// why it takes seconds rather than minutes.
//
// The order matters and is the whole design: back up what is about to change,
// put the certificates where Caddy will look before it needs them, then stop
// the old edge and start ours. Between those two steps connections are refused;
// everything else is arranged so that window is as short as it can be.

// CutoverDeps is what stage 3 needs.
type CutoverDeps struct {
	Plan    Plan
	Runner  migrate.Runner
	Journal *journal.Journal
	Probe   Prober

	// Edge is what holds 80 and 443 now.
	Edge EdgeHolder
	// CertDir is a directory of PEM certificate and key pairs, read when there
	// is no acme.json: an edge that is not Traefik keeps them as files. See
	// ImportCertificateDir.
	CertDir string
	// AcmePath is Traefik's certificate store, copied and read.
	AcmePath string
	// TraefikDir is Traefik's configuration, copied whole before anything.
	TraefikDir string
	// CaddyData is where Meshploy's Caddy keeps its storage on the host - the
	// mount point of its volume.
	CaddyData string
	// ReadyCaddy makes sure Meshploy's edge can start, before anything is
	// stopped. Nil skips the check, which is only right in a test.
	ReadyCaddy func() error
	// StartCaddy brings Meshploy's edge up on 80 and 443.
	StartCaddy func() error
	// ControlPlane is the old platform's own server, stopped at cutover so it
	// cannot recreate the edge it is losing. Empty leaves it running.
	ControlPlane ControlPlane
	// EdgeAddr is where Meshploy's own edge should answer once it holds the
	// ports. Empty means 127.0.0.1:443.
	EdgeAddr string
	// EdgeUpWindow is how long it has to start listening. Zero means the
	// default; a test uses its own so the suite does not wait a minute to
	// learn what it already knows.
	EdgeUpWindow time.Duration
	// EdgeGoneWindow is how long the old edge has to let go of the port after
	// being told to stop. Zero means the default.
	EdgeGoneWindow time.Duration
	// CaddyContainer is what StartCaddy started, so a rollback can take it off
	// the ports again. Without it, rolling a cutover back started the old edge
	// against ports Meshploy's was still holding, and neither served.
	CaddyContainer string

	Now func() time.Time
}

// EdgeHolder is the thing to stop: a Swarm service, a container, or a systemd
// unit for a custom edge.
type EdgeHolder struct {
	// Kind is "swarm", "container" or "systemd".
	Kind string
	Name string
	// Note is what the host found, for the case where none of those three is
	// what holds the ports - a process nobody supervises. Cutover cannot take
	// the ports from it, and saying which process it is beats saying nothing.
	Note string
}

// CutoverResult is what stage 3 did.
type CutoverResult struct {
	CertificatesImported []string          `json:"certificates_imported"`
	CertificatesSkipped  map[string]string `json:"certificates_skipped,omitempty"`
	// Unreachable are domains that did not answer once Caddy held the ports.
	Unreachable []string `json:"unreachable,omitempty"`
	// Unserved are domains of workloads that never moved. They stop being
	// served here - the edge that served them is stopped - and their workloads
	// keep running on the old platform, which finish still refuses to remove.
	Unserved []string `json:"unserved,omitempty"`
	// RolledBack is true when the old edge was put back because domains did
	// not answer.
	RolledBack bool   `json:"rolled_back,omitempty"`
	Error      string `json:"error,omitempty"`
	Downtime   string `json:"downtime,omitempty"`
}

// Cutover runs stage 3.
func Cutover(d CutoverDeps) (CutoverResult, error) {
	out := CutoverResult{CertificatesSkipped: map[string]string{}}
	if d.Journal == nil || d.StartCaddy == nil {
		return out, fmt.Errorf("cutover needs a journal and a way to start Caddy")
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}

	// A group that can still move has no business being left behind: moving it
	// costs only the downtime it already carries, and cutting over without it
	// takes its domains down for nothing.
	pending := PendingAtCutover(d.Plan, d.Journal)
	if len(pending.Movable) > 0 {
		out.Error = fmt.Sprintf("%s can still move: move %s first, or answer what is holding %s back",
			strings.Join(pending.Movable, ", "),
			plural(len(pending.Movable), "it", "them"), plural(len(pending.Movable), "it", "them"))
		return out, fmt.Errorf("%s", out.Error)
	}
	// What cannot move is left where it is, named rather than refused.
	out.Unserved = pending.StuckDomains

	// 1. Back up what is about to change. Before anything else, because the
	// value of a backup is entirely in having taken it early.
	if err := d.backupEdge(); err != nil {
		out.Error = err.Error()
		return out, err
	}

	// 2. Certificates, while the old edge is still serving. A failure here is
	// not fatal: Caddy issues what it does not find, which is the normal path.
	if imported, err := d.importCertificates(now()); err != nil {
		out.CertificatesSkipped["*"] = err.Error()
	} else {
		out.CertificatesImported, out.CertificatesSkipped = imported.Imported, imported.Skipped
	}

	// 3. Check our edge can start, while the old one is still serving.
	//
	// Everything after this point is downtime, and finding out there that the
	// image is missing means a server with no edge at all until the restore
	// runs. Found on a rehearsal: the pull failed after the ports had already
	// changed hands, and the only thing between that and an outage was the
	// automatic restore. A cutover should fail before the downtime, not during
	// it.
	if d.ReadyCaddy != nil {
		if err := d.ReadyCaddy(); err != nil {
			out.Error = "Meshploy's edge is not ready to take the ports: " + err.Error()
			return out, fmt.Errorf("%s", out.Error)
		}
	}

	// 4. The control plane, before its edge and before any downtime: while the
	// old platform is running it can put its edge back. It is the thing whose
	// job is keeping that edge alive, and a recreated edge races ours for 80
	// and 443 on the next restart. Its data is untouched and rollback starts it
	// again; only the console is not running between here and finish.
	if err := d.stopControlPlane(); err != nil {
		out.Error = err.Error()
		return out, err
	}

	// 5 and 6. The ports change hands. Everything between these two lines is
	// refused connections, so there is nothing between them.
	started := now()
	if err := d.stopEdge(); err != nil {
		out.Error = err.Error()
		return out, err
	}
	// `docker service scale` is asked to detach, so it returns once Swarm has
	// accepted the request and not once the task is gone. Starting ours against
	// a port the old edge has not let go of yet is a crash-loop, so wait for
	// the socket rather than race it. This is inside the downtime because it is
	// the downtime: nothing can serve until the port is free.
	if err := d.waitEdgeGone(); err != nil {
		out.Error = err.Error()
		if undoErr := d.restoreEdge(); undoErr == nil {
			out.RolledBack = true
		}
		return out, fmt.Errorf("%s", out.Error)
	}
	if err := d.StartCaddy(); err != nil {
		// Ours did not come up: put the old edge back at once rather than
		// leaving the server dark while somebody reads an error message.
		out.Error = err.Error()
		if undoErr := d.restoreEdge(); undoErr == nil {
			out.RolledBack = true
		} else {
			out.Error += "; and the old edge could not be restarted: " + undoErr.Error()
		}
		return out, fmt.Errorf("%s", out.Error)
	}
	// Our edge has to be holding the ports, whatever the plan's domains say.
	// `docker compose up` succeeding means the container was created, not that
	// anything is listening - and on a server with no domains to probe, that
	// was the only thing cutover checked before declaring the ports handed
	// over.
	if err := d.edgeIsUp(); err != nil {
		out.Error = err.Error()
		if undoErr := d.restoreEdge(); undoErr == nil {
			out.RolledBack = true
		} else {
			out.Error += "; and the old edge could not be restarted: " + undoErr.Error()
		}
		return out, fmt.Errorf("%s", out.Error)
	}
	out.Downtime = now().Sub(started).Round(time.Second).String()
	if d.CaddyContainer != "" {
		_ = d.Journal.Append(journal.Entry{Step: "cutover/start-caddy", Action: "start-container",
			Target: d.CaddyContainer, Result: journal.OK, Undo: &journal.Undo{
				Kind: journal.UndoStopContainer,
				Args: map[string]string{"container": d.CaddyContainer},
			}})
	}

	// 5. Check. A domain that does not answer is why rollback exists.
	//
	// The first one waits for the new edge to be ready - opening its listeners,
	// and issuing a certificate for any name whose own did not come across. The
	// rest do not: once one domain answers the edge is up, and a server with
	// seventy domains cannot spend a full waiting window on each.
	if d.Probe != nil {
		first := true
		for host := range WantedDomains(d.Plan) {
			var err error
			if first {
				err = d.Probe.Probe(host)
				first = false
			} else {
				err = probeBriefly(d.Probe, host)
			}
			if err != nil {
				out.Unreachable = append(out.Unreachable, host)
			}
		}
	}
	if len(out.Unreachable) > 0 {
		out.Error = fmt.Sprintf("%d domain(s) did not answer: %s",
			len(out.Unreachable), strings.Join(out.Unreachable, ", "))
		return out, fmt.Errorf("%s", out.Error)
	}
	return out, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// edgeIsUp waits for Meshploy's edge to be the thing holding the ports.
//
// Two questions, and the second is the one that was missing. Something
// listening is not the same as our edge listening: an old edge that never
// actually stopped answers a dial just as well, and the handover then reports
// success while ours crash-loops behind it against a bound socket. That is not
// a hypothetical - it is what a real cutover did, and the operator was told the
// ports had changed hands.
//
// So our own container is asked whether it is running, and a container that is
// restarting is not. What it serves is still Caddy's business; whether it is
// the one on the port is this function's.
func (d CutoverDeps) edgeIsUp() error {
	addr := d.EdgeAddr
	if addr == "" {
		addr = "127.0.0.1:443"
	}
	window := d.EdgeUpWindow
	if window == 0 {
		window = edgeUpWindow
	}
	deadline := time.Now().Add(window)
	var last error
	for {
		if err := d.ourEdgeRunning(); err != nil {
			last = err
		} else {
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			last = fmt.Errorf("nothing is listening on %s", addr)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("Meshploy's edge did not take the ports: %w", last)
		}
		time.Sleep(min(time.Second, window/4))
	}
}

// ourEdgeRunning reports whether the container StartCaddy started is up.
//
// Empty CaddyContainer skips the check, which is what a test without a Docker
// does; a server always names it.
func (d CutoverDeps) ourEdgeRunning() error {
	if d.CaddyContainer == "" {
		return nil
	}
	out, err := d.Runner.Output("docker", "inspect", "-f",
		"{{.State.Status}} {{.State.Restarting}}", d.CaddyContainer)
	if err != nil {
		return fmt.Errorf("%s could not be inspected: %w", d.CaddyContainer, err)
	}
	status, restarting, _ := strings.Cut(strings.TrimSpace(out), " ")
	if status != "running" || restarting == "true" {
		return fmt.Errorf("%s is %s, not running - something else is holding the ports",
			d.CaddyContainer, strings.TrimSpace(out))
	}
	return nil
}

// waitEdgeGone waits for the old edge to let go of the port it was told to
// give up.
func (d CutoverDeps) waitEdgeGone() error {
	addr := d.EdgeAddr
	if addr == "" {
		addr = "127.0.0.1:443"
	}
	window := d.EdgeGoneWindow
	if window == 0 {
		window = edgeGoneWindow
	}
	deadline := time.Now().Add(window)
	for {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return nil
		}
		_ = conn.Close()
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the old edge is still holding %s: it was told to stop and has not", addr)
		}
		time.Sleep(min(500*time.Millisecond, window/4))
	}
}

// edgeGoneWindow is how long the old edge has to release the port. Short: it
// has already been told to stop, and every second here is downtime.
const edgeGoneWindow = 20 * time.Second

// edgeUpWindow is how long the new edge has to start listening. Long enough
// for a container to come up, short enough that a server with no edge is put
// back while the operator is still watching.
const edgeUpWindow = 60 * time.Second

// briefProbeWindow is what a domain gets once the edge has proved it is up.
const briefProbeWindow = 10 * time.Second

// probeBriefly asks with a short window where the prober supports one, and
// exactly once where it does not.
func probeBriefly(p Prober, host string) error {
	if hp, ok := p.(HTTPProbe); ok {
		hp.Window = briefProbeWindow
		return hp.Probe(host)
	}
	return p.Probe(host)
}

// backupEdge copies Traefik's configuration and its certificate store into the
// journal's directory.
func (d CutoverDeps) backupEdge() error {
	step := "cutover/backup"
	if d.Journal.Done(step) {
		return nil
	}
	var copied []string
	if d.AcmePath != "" {
		b, err := os.ReadFile(d.AcmePath)
		switch {
		case os.IsNotExist(err):
			// A custom edge keeps its certificates elsewhere, and a server may
			// never have had any. Nothing to copy is not a failure.
		case err != nil:
			return fmt.Errorf("read %s: %w", d.AcmePath, err)
		default:
			if _, err := d.Journal.Backup("acme.json", b); err != nil {
				return err
			}
			copied = append(copied, "acme.json")
		}
	}
	if d.TraefikDir != "" {
		n, err := d.backupDir(d.TraefikDir)
		if os.IsNotExist(err) {
			n, err = 0, nil
		}
		if err != nil {
			return err
		}
		copied = append(copied, fmt.Sprintf("%d configuration file(s)", n))
	}
	if len(copied) == 0 {
		copied = []string{"nothing to copy"}
	}
	return d.Journal.Append(journal.Entry{Step: step, Action: "back-up-edge",
		Target: strings.Join(copied, ", "), Result: journal.OK})
}

func (d CutoverDeps) backupDir(dir string) (int, error) {
	var n int
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(dir, path)
		if _, err := d.Journal.Backup(filepath.Join("edge", rel), b); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

// importCertificates writes the old edge's certificates where Caddy will look.
//
// Traefik's store first, then a directory of PEM files, because a server whose
// edge is not Traefik has no acme.json and its certificates are worth just as
// much: without them Caddy asks Let's Encrypt for every domain at once, in the
// one window where the operator is watching.
func (d CutoverDeps) importCertificates(now time.Time) (CertImport, error) {
	if d.CaddyData == "" || (d.AcmePath == "" && d.CertDir == "") {
		return CertImport{}, fmt.Errorf("no certificate store to import from")
	}
	imported, err := d.readCertificates(now)
	if err != nil {
		return CertImport{}, err
	}
	root := CaddyDataDir(d.CaddyData)
	for _, f := range imported.Files {
		path := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return imported, err
		}
		if err := os.WriteFile(path, f.Content, os.FileMode(f.Mode)); err != nil {
			return imported, err
		}
	}
	_ = d.Journal.Append(journal.Entry{Step: "cutover/certificates", Action: "import-certificates",
		Target: fmt.Sprintf("%d domain(s)", len(imported.Imported)), Result: journal.OK})
	return imported, nil
}

// readCertificates reads whichever store this host has.
func (d CutoverDeps) readCertificates(now time.Time) (CertImport, error) {
	wanted := WantedDomains(d.Plan)
	if d.AcmePath == "" {
		return ImportCertificateDir(d.CertDir, wanted, now)
	}
	acme, err := os.ReadFile(d.AcmePath)
	if err != nil {
		return CertImport{}, err
	}
	return ImportCertificates(acme, wanted, now)
}

// ControlPlane is the old platform's own server: what recreates its edge.
type ControlPlane struct {
	// Kind is "swarm" or "container"; empty means there is none to stop.
	Kind string
	Name string
}

// stopControlPlane stops the platform being replaced, so nothing puts its edge
// back. A platform that is not there is not a failure: a server whose console
// was already removed is exactly the state this is aiming for.
func (d CutoverDeps) stopControlPlane() error {
	step := "cutover/stop-control-plane"
	if d.Journal.Done(step) || d.ControlPlane.Name == "" {
		return nil
	}
	switch d.ControlPlane.Kind {
	case "swarm":
		if !d.swarmServiceExists(d.ControlPlane.Name) {
			return nil
		}
		return Control{Runner: d.Runner, Journal: d.Journal}.
			Stop(step, "", Workload{Name: d.ControlPlane.Name, Swarm: true})
	case "container":
		if !d.containerExists(d.ControlPlane.Name) {
			return nil
		}
		return Control{Runner: d.Runner, Journal: d.Journal}.
			Stop(step, "", Workload{Name: d.ControlPlane.Name})
	}
	return nil
}

func (d CutoverDeps) swarmServiceExists(name string) bool {
	out, err := d.Runner.Output("docker", "service", "inspect", name, "--format", "{{.ID}}")
	return err == nil && strings.TrimSpace(out) != ""
}

func (d CutoverDeps) containerExists(name string) bool {
	out, err := d.Runner.Output("docker", "inspect", name, "--format", "{{.Id}}")
	return err == nil && strings.TrimSpace(out) != ""
}

// stopEdge takes the old edge off the ports, recording how to put it back.
//
// Stopped, never removed: nothing of Dokploy's is deleted before finish, and an
// edge that can be started again is what makes rollback possible for days.
func (d CutoverDeps) stopEdge() error {
	step := "cutover/stop-edge"
	if d.Journal.Done(step) {
		return nil
	}
	switch d.Edge.Kind {
	case "swarm":
		return Control{Runner: d.Runner, Journal: d.Journal}.
			Stop(step, "", Workload{Name: d.Edge.Name, Swarm: true})
	case "container":
		return Control{Runner: d.Runner, Journal: d.Journal}.
			Stop(step, "", Workload{Name: d.Edge.Name})
	case "systemd":
		if _, err := d.Runner.Output("systemctl", "stop", d.Edge.Name); err != nil {
			return fmt.Errorf("stop %s: %w", d.Edge.Name, err)
		}
		return d.Journal.Append(journal.Entry{Step: step, Action: "stop-edge", Target: d.Edge.Name,
			Result: journal.OK, Undo: &journal.Undo{Kind: journal.UndoStartUnit,
				Args: map[string]string{"unit": d.Edge.Name}}})
	}
	if d.Edge.Note != "" {
		return fmt.Errorf("nothing here can hand over ports 80 and 443: %s", d.Edge.Note)
	}
	return fmt.Errorf("nothing known holds ports 80 and 443 here")
}

// restoreEdge puts the old edge back, for a cutover that could not bring ours
// up.
//
// The journal goes in, and that is the whole point of it. Without it the
// restore ran but was never recorded, so the stop stayed in the journal as
// done: the next attempt skipped stopping an edge that was back up and holding
// the ports, started ours against a bound socket, and called the handover
// finished. Found on a real server, where the second cutover reported success
// while Traefik still held 443 and Caddy crash-looped behind it.
func (d CutoverDeps) restoreEdge() error {
	entries, err := journal.Read(d.Journal.Dir())
	if err != nil {
		return err
	}
	var edge []journal.Entry
	for _, e := range journal.Undoable(entries, "") {
		if e.Step == "cutover/stop-edge" {
			edge = append(edge, e)
		}
	}
	res := Rollback{Runner: d.Runner, Journal: d.Journal}.Replay(edge)
	if len(res.Failures) > 0 {
		return fmt.Errorf("%s", strings.Join(res.Failures, "; "))
	}
	return nil
}
