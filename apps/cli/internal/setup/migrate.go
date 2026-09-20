package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
)

// A server being installed may already run another platform. Setup detects it,
// shows what moving it would take, and records the operator's choices. Nothing
// here changes the other platform: the stages that move workloads run later,
// from the console (internal-docs/plans/migrate-from-dokploy.md).

// migrationDir is where the confirmed plan waits for those stages. A var for
// tests.
var migrationDir = "/var/lib/meshploy/migrate/dokploy"

const migrationPlanFile = "plan.json"

// MigrationDir is where the confirmed plan, the journal and the migration's
// credential live. Root-only: the plan names every app, domain and path on the
// server, and the credential can act on the whole organisation.
func MigrationDir() string { return migrationDir }

// Planner detects another platform on this host and plans moving it.
type Planner interface {
	// Detect is the quick look: is it there, which version, can it be planned.
	Detect(ctx context.Context) (dokploy.Plan, error)
	// Plan reads everything, which on a large server takes a while.
	Plan(ctx context.Context) (dokploy.Plan, error)
}

// Migration is what the wizard shows about the platform found here.
type Migration struct {
	// Platform is empty when nothing was found.
	Platform  string        `json:"platform,omitempty"`
	Version   string        `json:"version,omitempty"`
	Supported bool          `json:"supported"`
	Note      string        `json:"note,omitempty"`
	Plan      *dokploy.Plan `json:"plan,omitempty"`
	// Choices are what the operator has confirmed, once they have.
	Choices *MigrationChoices `json:"choices,omitempty"`
}

// MigrationChoices is the operator's answers, saved with the plan.
type MigrationChoices struct {
	// Decisions maps an item's id to the option chosen for each of its
	// decisions.
	Decisions map[string]map[string]string `json:"decisions,omitempty"`
	// Exclude lists items the operator chose to leave behind.
	Exclude []string `json:"exclude,omitempty"`
	// Skipped is true when the operator installs Meshploy without migrating.
	Skipped bool      `json:"skipped,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}

// confirmedPlan is what is written to disk for the stages that follow.
type confirmedPlan struct {
	Plan    dokploy.Plan     `json:"plan"`
	Choices MigrationChoices `json:"choices"`
}

// validateChoices checks every answer against the plan: an item that is not
// there, a decision that is not asked, or an option that is not offered is a
// mistake worth catching before anything moves. Every decision without a
// default must be answered, since that is what "no default" means.
func validateChoices(plan dokploy.Plan, ch MigrationChoices) error {
	if ch.Skipped {
		return nil
	}
	items := map[string]dokploy.Item{}
	for _, it := range plan.Items {
		items[it.ID] = it
	}
	for _, id := range ch.Exclude {
		if _, ok := items[id]; !ok {
			return fmt.Errorf("cannot exclude %q: it is not in the plan", id)
		}
	}
	excluded := map[string]bool{}
	for _, id := range ch.Exclude {
		excluded[id] = true
	}
	for id, answers := range ch.Decisions {
		it, ok := items[id]
		if !ok {
			return fmt.Errorf("no item %q in the plan", id)
		}
		for decisionID, option := range answers {
			d, ok := findDecision(it, decisionID)
			if !ok {
				return fmt.Errorf("%s was not asked %q", it.Name, decisionID)
			}
			if !hasOption(d, option) {
				return fmt.Errorf("%s: %q is not an option for %q", it.Name, option, decisionID)
			}
		}
	}
	var missing []string
	for _, it := range plan.Items {
		if excluded[it.ID] || it.Verdict == dokploy.NotMoved {
			continue
		}
		for _, d := range it.Decisions {
			if d.Default != "" || ch.Decisions[it.ID][d.ID] != "" {
				continue
			}
			missing = append(missing, it.Name+": "+d.Question)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return errors.New("these need a choice before the plan can be saved: " + missing[0] +
			plural(len(missing)-1))
	}
	return nil
}

func plural(more int) string {
	switch more {
	case 0:
		return ""
	case 1:
		return " (and 1 more)"
	}
	return fmt.Sprintf(" (and %d more)", more)
}

func findDecision(it dokploy.Item, id string) (dokploy.Decision, bool) {
	for _, d := range it.Decisions {
		if d.ID == id {
			return d, true
		}
	}
	return dokploy.Decision{}, false
}

func hasOption(d dokploy.Decision, option string) bool {
	for _, o := range d.Options {
		if o.ID == option {
			return true
		}
	}
	return false
}

// withDefaults fills in the choices the operator left alone, so what is saved
// is the whole answer rather than the difference from a default that could
// change in a later release.
func withDefaults(plan dokploy.Plan, ch MigrationChoices) MigrationChoices {
	if ch.Decisions == nil {
		ch.Decisions = map[string]map[string]string{}
	}
	excluded := map[string]bool{}
	for _, id := range ch.Exclude {
		excluded[id] = true
	}
	for _, it := range plan.Items {
		if excluded[it.ID] || it.Verdict == dokploy.NotMoved {
			continue
		}
		for _, d := range it.Decisions {
			if ch.Decisions[it.ID][d.ID] != "" || d.Default == "" {
				continue
			}
			if ch.Decisions[it.ID] == nil {
				ch.Decisions[it.ID] = map[string]string{}
			}
			ch.Decisions[it.ID][d.ID] = d.Default
		}
	}
	return ch
}

// saveConfirmedPlan writes the plan and its answers where the migration stages
// read them. Root only: the plan names every app, domain and path on the
// server.
func saveConfirmedPlan(plan dokploy.Plan, ch MigrationChoices) error {
	if err := os.MkdirAll(migrationDir, 0700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(confirmedPlan{Plan: plan, Choices: ch}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(migrationDir, migrationPlanFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadConfirmedPlan loads what setup saved, or nil when nothing was saved.
func ReadConfirmedPlan() (*dokploy.Plan, *MigrationChoices, error) {
	b, err := os.ReadFile(filepath.Join(migrationDir, migrationPlanFile))
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var saved confirmedPlan
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, nil, err
	}
	return &saved.Plan, &saved.Choices, nil
}
