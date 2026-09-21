package dokploy

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Stage 4: finish.
//
// Every other stage is reversible. This one is not, and that is its whole
// character: it exists to be run days later, deliberately, once the operator
// has watched the migrated server serve real traffic and decided they are not
// going back.
//
// So it is built the other way round from the rest. It says what it will
// remove before it removes anything, it refuses while a rollback would still
// mean something, and it removes only what it can name: the platform's own
// parts and the copies of workloads that moved. Anything else running on this
// host was never the migration's to take - an application nobody migrated, a
// compose project that has nothing to do with Dokploy - and stays.
//
// What it keeps: volumes, unless asked; the journal and the edge backups,
// because "it was fine for a week" is a thing people discover on day eight.

// FinishScope is what finish would remove, in the order it would go.
type FinishScope struct {
	// Services, Containers and Networks are Docker's, named.
	Services   []string `json:"services,omitempty"`
	Containers []string `json:"containers,omitempty"`
	Networks   []string `json:"networks,omitempty"`
	// Paths are directories on this host.
	Paths []string `json:"paths,omitempty"`
	// Volumes are listed always and removed only when asked, because a volume
	// is the one thing here that cannot be rebuilt from anywhere else.
	Volumes []ScopedVolume `json:"volumes,omitempty"`
	// Kept says what stays behind and why, so the list reads as a decision
	// rather than an oversight.
	Kept []string `json:"kept,omitempty"`
	// Blockers are why finish must not run yet. Any blocker and it will not.
	Blockers []string `json:"blockers,omitempty"`
}

// ScopedVolume is one volume finish would remove, with what it holds.
type ScopedVolume struct {
	Name string `json:"name"`
	MB   int    `json:"mb"`
}

// FinishDeps is what stage 4 needs.
type FinishDeps struct {
	Plan    Plan
	Source  Source
	Runner  migrate.Runner
	Journal *journal.Journal
	// API revokes the principal the migration acted as. Nil skips that, which
	// is only right in a test.
	API FinishAPI
	// RemoveVolumes takes the volumes too. Off by default: an operator who
	// wants the data gone says so.
	RemoveVolumes bool
	// EtcDir is the platform's configuration directory.
	EtcDir string
	Now    func() time.Time
}

// FinishAPI is the Meshploy side of finishing.
type FinishAPI interface {
	// RevokeMigrationAgent takes away the principal, and with it the token on
	// this host.
	RevokeMigrationAgent() error
}

// FinishResult is what stage 4 did.
type FinishResult struct {
	Removed   FinishScope `json:"removed"`
	Failures  []string    `json:"failures,omitempty"`
	AgentGone bool        `json:"agent_revoked"`
	// JournalKept is where the record of the whole migration stays.
	JournalKept string `json:"journal_kept,omitempty"`
}

// FinishedStep is the journal entry that says a migration is over. Rollback
// refuses once it is there: there is nothing left to go back to.
const FinishedStep = "finish/complete"

// Finished reports whether this migration has been finished.
func Finished(j *journal.Journal) bool { return j != nil && j.Done(FinishedStep) }

// PlanFinish works out what finish would do, without doing any of it.
func PlanFinish(d FinishDeps) FinishScope {
	scope := FinishScope{Blockers: finishBlockers(d)}

	moved := map[string]bool{}
	// owned is everything the platform ran, moved or not. It is what tells a
	// workload of somebody else's from one of the platform's, and without it
	// every migrated workload was counted as a stranger in the summary.
	owned := map[string]bool{}
	for _, it := range d.Plan.Items {
		switch it.Kind {
		case "application", "database", "compose":
			name := itemAppName(it)
			if name == "" {
				continue
			}
			owned[name] = true
			if it.Verdict == Moves {
				moved[name] = true
			}
		}
	}

	// Docker's view, not the plan's: what is actually on this host is what can
	// be removed, and a workload deleted by hand should not appear in a list
	// that promises to remove it.
	for _, s := range d.Source.Docker.Services {
		if dokployOwn(s.Name) || moved[s.Name] {
			scope.Services = append(scope.Services, s.Name)
		}
	}
	for _, c := range d.Source.Docker.Containers {
		if c.Service != "" {
			continue // a Swarm task goes with its service
		}
		if dokployOwn(c.Name) || moved[c.Name] || moved[c.Project] {
			scope.Containers = append(scope.Containers, c.Name)
		}
	}
	scope.Networks = []string{dokployNetwork}
	if d.EtcDir != "" {
		scope.Paths = []string{d.EtcDir}
	}

	for _, v := range d.Source.Docker.Volumes {
		if finishOwnsVolume(v.Name, moved) {
			scope.Volumes = append(scope.Volumes, ScopedVolume{Name: v.Name, MB: v.MB})
		}
	}

	sort.Strings(scope.Services)
	sort.Strings(scope.Containers)
	sort.Slice(scope.Volumes, func(i, j int) bool { return scope.Volumes[i].Name < scope.Volumes[j].Name })

	if !d.RemoveVolumes && len(scope.Volumes) > 0 {
		scope.Kept = append(scope.Kept, fmt.Sprintf("%d volume(s), holding the data as it was at the move", len(scope.Volumes)))
	}
	if unmanaged := unmanaged(d.Source, owned); len(unmanaged) > 0 {
		scope.Kept = append(scope.Kept, fmt.Sprintf("%d workload(s) this migration never touched", len(unmanaged)))
	}
	scope.Kept = append(scope.Kept, "the migration's journal and the edge's configuration backup")
	return scope
}

// dokployNetwork is the overlay Dokploy puts its workloads on.
const dokployNetwork = "dokploy-network"

// finishOwnsVolume reports whether a volume belongs to this migration's subject:
// the platform's own, or one a migrated workload used. Anything else is
// somebody's data that has nothing to do with the migration.
func finishOwnsVolume(name string, moved map[string]bool) bool {
	if strings.HasPrefix(name, "dokploy-") {
		return true
	}
	for app := range moved {
		if name == app || strings.HasPrefix(name, app+"-") {
			return true
		}
	}
	return false
}

// finishBlockers are the reasons finish must not run.
//
// Both are the same reason really: something on this server is still Dokploy's
// to serve, and removing Dokploy would take it down.
func finishBlockers(d FinishDeps) []string {
	var out []string
	if d.Journal == nil {
		return []string{"there is no journal, so what this server has done cannot be known"}
	}
	for _, g := range d.Plan.Groups {
		if !groupHasMoved(d.Journal, g) {
			out = append(out, fmt.Sprintf("%s has not moved", g.Name))
		}
	}
	if !d.Journal.Done("cutover/stop-edge") {
		out = append(out, "the ports have not been handed over: run cutover first")
	}
	return out
}

// groupHasMoved reports whether every member of a group started in Meshploy.
func groupHasMoved(j *journal.Journal, g Group) bool {
	for _, m := range g.Members {
		if !j.Done(fmt.Sprintf("move/%s/start/%s", g.ID, m.ID)) {
			return false
		}
	}
	return len(g.Members) > 0
}

// Finish runs stage 4.
//
// Failures are collected rather than fatal. Half of finishing is better than
// none: a network that will not go because something is still attached should
// not leave the platform's containers running, and the operator gets one list
// of what is left to do by hand.
func Finish(d FinishDeps) (FinishResult, error) {
	scope := PlanFinish(d)
	out := FinishResult{Removed: scope}
	if len(scope.Blockers) > 0 {
		return out, fmt.Errorf("this server is not ready to finish:\n  %s", strings.Join(scope.Blockers, "\n  "))
	}
	if d.Journal == nil {
		return out, fmt.Errorf("finishing needs a journal to record what it removed")
	}
	if Finished(d.Journal) {
		return out, fmt.Errorf("this migration was already finished")
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}

	// Workloads first, then the platform, then what they were attached to: a
	// network with endpoints on it cannot be removed.
	for _, name := range scope.Services {
		d.remove(&out, "finish/service/"+name, "remove-service", name,
			func() error { return d.docker("service", "rm", name) })
	}
	for _, name := range scope.Containers {
		d.remove(&out, "finish/container/"+name, "remove-container", name,
			func() error { return d.docker("rm", "-f", name) })
	}
	for _, name := range scope.Networks {
		d.remove(&out, "finish/network/"+name, "remove-network", name,
			func() error { return d.docker("network", "rm", name) })
	}
	if d.RemoveVolumes {
		for _, v := range scope.Volumes {
			d.remove(&out, "finish/volume/"+v.Name, "remove-volume", v.Name,
				func() error { return d.docker("volume", "rm", v.Name) })
		}
	}
	for _, path := range scope.Paths {
		d.remove(&out, "finish/path/"+path, "remove-path", path,
			func() error { return os.RemoveAll(path) })
	}

	// The credential last: everything above went through it.
	if d.API != nil {
		if err := d.API.RevokeMigrationAgent(); err != nil {
			out.Failures = append(out.Failures, "revoke the migration's principal: "+err.Error())
		} else {
			out.AgentGone = true
			_ = d.Journal.Append(journal.Entry{Step: "finish/agent", Action: "revoke-agent",
				Target: MigrationAgentName, Result: journal.OK})
		}
	}

	out.JournalKept = d.Journal.Dir()
	_ = d.Journal.Append(journal.Entry{Step: FinishedStep, Action: "finish",
		Target: fmt.Sprintf("%d service(s), %d container(s)", len(scope.Services), len(scope.Containers)),
		Result: journal.OK, At: now().UTC()})
	return out, nil
}

// remove runs one removal, records it, and carries on.
func (d FinishDeps) remove(out *FinishResult, step, action, target string, fn func() error) {
	if d.Journal.Done(step) {
		return
	}
	if err := fn(); err != nil {
		out.Failures = append(out.Failures, fmt.Sprintf("%s %s: %v", action, target, err))
		_ = d.Journal.Append(journal.Entry{Step: step, Action: action, Target: target,
			Result: journal.Failed, Error: err.Error()})
		return
	}
	_ = d.Journal.Append(journal.Entry{Step: step, Action: action, Target: target, Result: journal.OK})
}

func (d FinishDeps) docker(args ...string) error {
	_, err := d.Runner.Output("docker", args...)
	return err
}

// itemAppName is what Docker calls this workload.
func itemAppName(it Item) string {
	if n := it.Details["app_name"]; n != "" {
		return n
	}
	return it.Name
}
