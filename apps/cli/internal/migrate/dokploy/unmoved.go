package dokploy

import (
	"sort"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// What happens to a workload that cannot move.
//
// Some of them cannot: an application whose certificate was uploaded by hand, a
// route with basic auth, a compose app this migration does not carry yet. The
// operator can often answer the question and move it; sometimes there is no
// answer they want to give.
//
// Cutover used to refuse while any group was unmoved, which turned one
// unanswerable question into a migration that could never finish. So the rule
// is narrower: a group that **can** move must move first - there is no reason
// not to - and a group that cannot is left where it is, with its domains named
// before the ports change hands. They stop being served at that moment: the old
// edge that served them is stopped, and Meshploy has no route for a workload it
// was never given. The workload keeps running, and finish still refuses to
// remove it.

// GroupDomains are the hostnames a group serves.
func GroupDomains(plan Plan, g Group) []string {
	members := map[string]bool{}
	for _, m := range g.Members {
		members[m.ID] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, it := range plan.Items {
		if it.Kind != "domain" {
			continue
		}
		owner := it.Details["application_id"]
		if owner == "" {
			owner = it.Details["compose_id"]
		}
		if !members[owner] {
			continue
		}
		if host := splitHostPath(it.Name); host != "" && !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	sort.Strings(out)
	return out
}

// Pending is what still stands between a server and its cutover.
type Pending struct {
	// Movable are groups that can move and have not. Cutover refuses while any
	// of these exist: moving them costs nothing but the downtime they already
	// carry, and cutting over without them takes their domains down for no
	// reason.
	Movable []string
	// Stuck are groups that cannot move, with the domains that stop being
	// served when the ports change hands.
	Stuck        []string
	StuckDomains []string
}

// PendingAtCutover reads the plan and the journal for what has not moved.
func PendingAtCutover(plan Plan, j *journal.Journal) Pending {
	var out Pending
	for _, g := range plan.Groups {
		if groupHasMoved(j, g) {
			continue
		}
		if g.CanMove {
			out.Movable = append(out.Movable, g.Name)
			continue
		}
		out.Stuck = append(out.Stuck, g.Name)
		out.StuckDomains = append(out.StuckDomains, GroupDomains(plan, g)...)
	}
	sort.Strings(out.StuckDomains)
	return out
}
