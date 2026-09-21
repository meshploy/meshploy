package dokploy

import (
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
	"github.com/meshploy/packages/hostagent"
)

// Where a migration has got to.
//
// The journal is the record of everything that happened, and it lives on the
// host where only root can read it. The console cannot, and should not: the
// API has no business reading a file the root migrator writes and reads back.
// So the host summarises it after every stage into one small file the API
// reads like any other result.
//
// Written after every stage rather than on request, so a migration driven from
// a terminal - which is how all of them have been driven so far - shows the
// same state in the browser as it does in the shell.

// Progress reads the journal and says where each group has got to.
func Progress(plan Plan, j *journal.Journal, now time.Time) hostagent.MigrationStatus {
	out := hostagent.MigrationStatus{
		UpdatedAt: now.UTC(),
		CutOver:   j.Done("cutover/stop-edge"),
		Finished:  Finished(j),
		Groups:    make([]hostagent.GroupProgress, 0, len(plan.Groups)),
	}

	entries, err := journal.Read(j.Dir())
	if err != nil {
		entries = nil
	}
	// Prepared once anything of stage 1 stands. Not "every step succeeded":
	// prepare reports its own failures, and a half-built stage 1 is still a
	// stage 1 that has been run.
	lastError := map[string]string{}
	movedAt := map[string]time.Time{}
	for _, e := range entries {
		if strings.HasPrefix(e.Step, "prepare/") && e.Result == journal.OK {
			out.Prepared = true
		}
		if e.Group == "" {
			continue
		}
		switch e.Result {
		case journal.Failed:
			lastError[e.Group] = e.Error
		case journal.OK:
			if strings.Contains(e.Step, "/publish/") || strings.Contains(e.Step, "/start/") {
				movedAt[e.Group] = e.At
			}
			delete(lastError, e.Group)
		case journal.Undone:
			// Put back is not failed: the group is where it started, and the
			// reason it came back was recorded when it happened.
			delete(lastError, e.Group)
		}
	}

	for _, g := range plan.Groups {
		p := hostagent.GroupProgress{
			ID: g.ID, Name: g.Name, CanMove: g.CanMove, Blockers: g.Blockers,
			Downtime: g.Downtime, Moved: groupHasMoved(j, g), Error: lastError[g.ID],
		}
		for _, m := range g.Members {
			p.Members = append(p.Members, m.Kind+" "+m.Name)
		}
		for _, d := range g.Data {
			p.Data = append(p.Data, d.Name+" ("+d.Move+")")
		}
		if at, ok := movedAt[g.ID]; ok && p.Moved {
			at := at.UTC()
			p.MovedAt = &at
		}
		out.Groups = append(out.Groups, p)
	}
	return out
}
