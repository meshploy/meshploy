package dokploy

import (
	"fmt"
	"os"
	"strconv"

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
}

// MeshploySide is the part of a rollback that talks to Meshploy's API.
type MeshploySide interface {
	StopService(projectID, serviceID string) error
	PauseRoute(projectID, routeID string) error
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

	case journal.UndoStartContainer:
		_, err := r.Runner.Output("docker", "start", u.Args["container"])
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
	}
	return fmt.Errorf("unknown undo kind %q", u.Kind)
}
