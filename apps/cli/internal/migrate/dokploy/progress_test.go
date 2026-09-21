package dokploy

import (
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// The console follows a migration through this summary, so it has to say the
// same thing the journal does - including for a migration driven entirely from
// a terminal, which is how all of them have been driven so far.
func TestProgressReadsTheJournal(t *testing.T) {
	plan, _ := smallPlan(t)
	plan.Groups = []Group{
		{ID: "g1", Name: "web", CanMove: true, Downtime: "a restart",
			Members: []GroupMember{{Kind: "application", ID: "a1", Name: "web"}}},
		{ID: "g2", Name: "shop", CanMove: false, Blockers: []string{"its certificate was uploaded by hand"},
			Members: []GroupMember{{Kind: "database", ID: "d1", Name: "db"}},
			Data:    []GroupData{{Name: "db", MB: 120, Move: "dump and restore"}}},
	}
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	// Nothing has happened yet.
	at := time.Now()
	got := Progress(plan, j, at)
	if got.Prepared || got.CutOver || got.Finished {
		t.Errorf("a migration that has not run should say so: %+v", got)
	}
	if len(got.Groups) != 2 || got.Groups[0].Moved {
		t.Fatalf("groups = %+v", got.Groups)
	}
	if got.Groups[1].CanMove || len(got.Groups[1].Blockers) != 1 {
		t.Errorf("a group with an unanswered question says so: %+v", got.Groups[1])
	}
	if len(got.Groups[1].Data) != 1 {
		t.Errorf("a group says what it carries: %+v", got.Groups[1].Data)
	}

	// Stage 1, then one group moves.
	_ = j.Append(journal.Entry{Step: "prepare/service/a1", Action: "create-service", Result: journal.OK})
	_ = j.Append(journal.Entry{Step: "move/g1/start/a1", Group: "g1", Action: "start-service", Result: journal.OK})

	got = Progress(plan, j, at)
	if !got.Prepared {
		t.Error("stage 1 has run")
	}
	if !got.Groups[0].Moved || got.Groups[0].MovedAt == nil {
		t.Errorf("the group moved: %+v", got.Groups[0])
	}
	if got.Groups[1].Moved {
		t.Error("the other one did not")
	}

	// It is put back again.
	_ = j.Append(journal.Entry{Step: "move/g1/start/a1", Group: "g1", Action: "undo stop-service", Result: journal.Undone})
	if Progress(plan, j, at).Groups[0].Moved {
		t.Error("a group that was rolled back has not moved")
	}
}

// A failure is a reason to read, not damage to repair: the group put itself
// back. It is shown until something about that group succeeds.
func TestProgressCarriesTheReasonAGroupStopped(t *testing.T) {
	plan, _ := smallPlan(t)
	plan.Groups = []Group{{ID: "g1", Name: "web", CanMove: true,
		Members: []GroupMember{{Kind: "application", ID: "a1", Name: "web"}}}}
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	_ = j.Append(journal.Entry{Step: "move/g1/start/a1", Group: "g1", Action: "start-service",
		Result: journal.Failed, Error: "web did not become healthy"})
	if got := Progress(plan, j, time.Now()); got.Groups[0].Error == "" {
		t.Fatalf("the reason should be carried: %+v", got.Groups[0])
	}

	_ = j.Append(journal.Entry{Step: "move/g1/start/a1", Group: "g1", Action: "start-service", Result: journal.OK})
	got := Progress(plan, j, time.Now())
	if got.Groups[0].Error != "" {
		t.Errorf("a retry that worked clears it: %q", got.Groups[0].Error)
	}
	if !got.Groups[0].Moved {
		t.Error("and the group has moved")
	}
}

// After finishing there is nothing left to undo, and the page says so instead
// of offering buttons that would fail.
func TestProgressSaysWhenTheMigrationIsOver(t *testing.T) {
	plan, _ := smallPlan(t)
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	_ = j.Append(journal.Entry{Step: "cutover/stop-edge", Action: "stop-container", Result: journal.OK})
	_ = j.Append(journal.Entry{Step: FinishedStep, Action: "finish", Result: journal.OK})

	got := Progress(plan, j, time.Now())
	if !got.CutOver || !got.Finished {
		t.Errorf("got %+v", got)
	}
}
