package dokploy

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Undoing what the migration did, newest first.
//
// Rollback is not a second implementation of the migration in reverse: it
// replays the journal, and every step that changed something wrote down how to
// take it back. A kind this does not recognise stops the rollback rather than
// being skipped - the operator hearing "rolled back" must be able to believe it.

// Rollback replays undo records.
type Rollback struct {
	Runner migrate.Runner
	// Meshploy reverses the steps that live in Meshploy rather than on the
	// host - stopping a service copy, pausing a route. Nil where a rollback
	// only has to touch the host, and any such step is then reported as one
	// the caller must do.
	Meshploy MeshploySide
	// Journal records each reversal, so the step is no longer done: a group
	// that was put back and is moved again has to do the work over, not skip it
	// as already finished. Nil records nothing, which is only right in a test.
	Journal *journal.Journal
	// PortWait is how long a container put back waits for a host port the
	// gateway is still letting go of: pausing a TCP route closes its listener
	// on the proxy's next refresh, not at once. Zero means 90 seconds.
	PortWait time.Duration
	// Sleep is a seam for tests.
	Sleep func(time.Duration)
}

// startContainer starts one of the old platform's containers again, waiting
// out a host port Meshploy's gateway has been told to give back but has not
// yet: the moved copy's TCP route was paused a moment ago.
func (r Rollback) startContainer(name string) error {
	wait, sleep := r.PortWait, r.Sleep
	if wait == 0 {
		wait = 90 * time.Second
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	deadline := time.Now().Add(wait)
	for {
		_, err := r.Runner.Output("docker", "start", name)
		if err == nil {
			return nil
		}
		msg := err.Error()
		if !strings.Contains(msg, "address already in use") && !strings.Contains(msg, "port is already allocated") ||
			!time.Now().Before(deadline) {
			return err
		}
		sleep(2 * time.Second)
	}
}

// MeshploySide is the part of a rollback that talks to Meshploy's API.
type MeshploySide interface {
	StopService(projectID, serviceID string) error
	PauseRoute(projectID, routeID string) error
	PauseTCPRoute(projectID, routeID string) error
}

// Result of replaying one journal.
type Result struct {
	Undone  int
	Skipped int
	// Failures are the steps that could not be undone, in the order they were
	// tried. A rollback reports them all rather than stopping at the first:
	// the operator needs the whole list to finish by hand.
	Failures []string
}

// Replay undoes the entries, which are expected newest-first from
// journal.Undoable.
func (r Rollback) Replay(entries []journal.Entry) Result {
	var out Result
	for _, e := range entries {
		if err := r.one(e); err != nil {
			out.Failures = append(out.Failures, fmt.Sprintf("%s (%s): %v", e.Action, e.Target, err))
			continue
		}
		if r.Journal != nil {
			_ = r.Journal.Append(journal.Entry{
				Step: e.Step, Group: e.Group, Action: "undo " + e.Undo.Kind,
				Target: e.Target, Result: journal.Undone,
			})
		}
		out.Undone++
	}
	return out
}

func (r Rollback) one(e journal.Entry) error {
	u := e.Undo
	switch u.Kind {
	case journal.UndoRestoreFile:
		content, err := os.ReadFile(u.Args["backup"])
		if err != nil {
			return fmt.Errorf("read backup: %w", err)
		}
		return writeAtomic(u.Args["path"], content)

	case journal.UndoRemoveFile:
		if err := os.Remove(u.Args["path"]); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil

	case journal.UndoScaleService:
		n, err := strconv.Atoi(u.Args["replicas"])
		if err != nil || n < 1 {
			// The count was recorded when it was stopped; a missing or absurd
			// one means bringing it back at all is better than leaving it down.
			n = 1
		}
		_, err = r.Runner.Output("docker", "service", "scale", "--detach", fmt.Sprintf("%s=%d", u.Args["service"], n))
		return err

	case journal.UndoRepublishService:
		// The side port is the only one left, removed by its mode; 80 and
		// 443 come back in the mode they had. See moveEdgeAside for why a
		// bare port number removes nothing here.
		mode80, mode443 := u.Args["mode80"], u.Args["mode443"]
		if mode80 == "" {
			mode80 = "host"
		}
		if mode443 == "" {
			mode443 = "host"
		}
		_, err := r.Runner.Output("docker", "service", "update", "--detach",
			"--publish-rm", "mode=host,target=80",
			"--publish-add", "mode="+mode80+",published=80,target=80",
			"--publish-add", "mode="+mode443+",published=443,target=443",
			u.Args["service"])
		return err

	case journal.UndoForget:
		return nil

	case journal.UndoClearEdgeFallback:
		c, ok := r.Meshploy.(interface{ ClearEdgeFallback() error })
		if !ok {
			return fmt.Errorf("needs the Meshploy API, which this rollback was not given")
		}
		return c.ClearEdgeFallback()

	case journal.UndoStartUnit:
		_, err := r.Runner.Output("systemctl", "start", u.Args["unit"])
		return err

	case journal.UndoStartContainer:
		// The policy first: a container put back without the one it had would
		// stay down after the next reboot, which is not what was there before.
		if p := u.Args["restart"]; p != "" {
			if _, err := r.Runner.Output("docker", "update", "--restart="+p, u.Args["container"]); err != nil {
				return err
			}
		}
		_, err := r.Runner.Output("docker", "start", u.Args["container"])
		return err

	case journal.UndoStartContainers:
		for _, name := range strings.Split(u.Args["containers"], ",") {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			if err := r.startContainer(name); err != nil {
				return err
			}
		}
		return nil

	case journal.UndoStopContainer:
		_, err := r.Runner.Output("docker", "stop", u.Args["container"])
		return err

	case journal.UndoStopService:
		if r.Meshploy == nil {
			return fmt.Errorf("needs the Meshploy API, which this rollback was not given")
		}
		return r.Meshploy.StopService(u.Args["project_id"], u.Args["service_id"])

	case journal.UndoPauseRoute:
		if r.Meshploy == nil {
			return fmt.Errorf("needs the Meshploy API, which this rollback was not given")
		}
		return r.Meshploy.PauseRoute(u.Args["project_id"], u.Args["route_id"])

	case journal.UndoPauseTCPRoute:
		if r.Meshploy == nil {
			return fmt.Errorf("needs the Meshploy API, which this rollback was not given")
		}
		return r.Meshploy.PauseTCPRoute(u.Args["project_id"], u.Args["route_id"])
	}
	return fmt.Errorf("unknown undo kind %q", u.Kind)
}
