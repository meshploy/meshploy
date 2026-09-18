package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
)

// fakePlanner stands in for reading a real Dokploy server.
type fakePlanner struct {
	plan dokploy.Plan
	err  error
}

func (f fakePlanner) Detect(context.Context) (dokploy.Plan, error) {
	return dokploy.Plan{Detection: f.plan.Detection}, f.err
}

func (f fakePlanner) Plan(context.Context) (dokploy.Plan, error) { return f.plan, f.err }

// testPlan is one app that needs a choice with a default, one that needs one
// without, and a database that just moves.
func testPlan() dokploy.Plan {
	return dokploy.Plan{
		Detection: dokploy.Detection{Dokploy: true, Version: "v0.30.5", Supported: true},
		Items: []dokploy.Item{
			{Kind: "application", ID: "a1", Name: "web", Verdict: dokploy.NeedsYou,
				Decisions: []dokploy.Decision{{
					ID: "github", Question: "Built from a GitHub App connection",
					Options: []dokploy.Option{{ID: "image_only"}, {ID: "reconnect"}},
					Default: "image_only",
				}}},
			{Kind: "application", ID: "a2", Name: "api", Verdict: dokploy.NeedsYou,
				Decisions: []dokploy.Decision{{
					ID: "mount:/srv/data", Question: "Bind mount of /srv/data, shared",
					Options: []dokploy.Option{{ID: "copy"}, {ID: "skip"}},
				}}},
			{Kind: "database", ID: "d1", Name: "db", Verdict: dokploy.Moves},
		},
	}
}

func migrationServer(t *testing.T, p Planner) (*Server, http.Handler) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	migrationDir = filepath.Join(t.TempDir(), "migrate")
	s := NewServer(store, "ms_token", fakeResolver{}, &fakeRunner{}, p)
	return s, s.Handler()
}

func TestMigrationDetectionIsReportedWithoutAPlan(t *testing.T) {
	_, h := migrationServer(t, fakePlanner{plan: testPlan()})

	var got Migration
	w := do(h, "GET", "/api/migration", "ms_token", "")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Platform != "Dokploy" || got.Version != "v0.30.5" || !got.Supported {
		t.Fatalf("want Dokploy v0.30.5 supported, got %+v", got)
	}
	// Detection is the quick look; reading everything is a separate ask,
	// because on a busy server it takes a minute.
	if got.Plan != nil {
		t.Error("detection must not carry the whole plan")
	}
}

// A build without a planner still answers, so the page can leave the step out
// rather than break on it.
func TestMigrationWithoutAPlannerFindsNothing(t *testing.T) {
	_, h := migrationServer(t, nil)
	var got Migration
	_ = json.Unmarshal(do(h, "GET", "/api/migration", "ms_token", "").Body.Bytes(), &got)
	if got.Platform != "" {
		t.Errorf("want nothing found, got %+v", got)
	}
}

// Choices are only meaningful against the plan they answer, so saving before
// reading one is refused rather than written.
func TestChoicesWithoutAPlanAreRefused(t *testing.T) {
	_, h := migrationServer(t, fakePlanner{plan: testPlan()})
	if got := do(h, "POST", "/api/migration", "ms_token", `{}`).Code; got != http.StatusConflict {
		t.Fatalf("want 409, got %d", got)
	}
	if _, err := os.Stat(filepath.Join(migrationDir, migrationPlanFile)); !os.IsNotExist(err) {
		t.Error("nothing should have been written")
	}
}

func TestSavingChoicesWritesThePlanWithDefaultsFilledIn(t *testing.T) {
	_, h := migrationServer(t, fakePlanner{plan: testPlan()})
	do(h, "POST", "/api/migration/plan", "ms_token", "{}")

	body := `{"decisions":{"a2":{"mount:/srv/data":"copy"}}}`
	if w := do(h, "POST", "/api/migration", "ms_token", body); w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}

	plan, choices, err := ReadConfirmedPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || len(plan.Items) != 3 {
		t.Fatalf("want the plan saved beside the answers, got %+v", plan)
	}
	if choices.Decisions["a2"]["mount:/srv/data"] != "copy" {
		t.Errorf("the answer given was not saved: %+v", choices.Decisions)
	}
	// Saved whole rather than as a difference from a default that a later
	// release could change.
	if choices.Decisions["a1"]["github"] != "image_only" {
		t.Errorf("the default was not filled in: %+v", choices.Decisions)
	}
	if info, err := os.Stat(filepath.Join(migrationDir, migrationPlanFile)); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Errorf("the plan names every app, domain and path: want 0600, got %v", info.Mode().Perm())
	}
}

// A question with no safe answer is the one thing that has to be answered.
func TestAQuestionWithNoDefaultMustBeAnswered(t *testing.T) {
	_, h := migrationServer(t, fakePlanner{plan: testPlan()})
	do(h, "POST", "/api/migration/plan", "ms_token", "{}")

	w := do(h, "POST", "/api/migration", "ms_token", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "api: Bind mount of /srv/data") {
		t.Errorf("the refusal must name the question: %s", w.Body)
	}
}

// Left behind means not moved, so what it would have needed stops mattering.
func TestAnExcludedItemNeedsNoAnswers(t *testing.T) {
	_, h := migrationServer(t, fakePlanner{plan: testPlan()})
	do(h, "POST", "/api/migration/plan", "ms_token", "{}")

	if w := do(h, "POST", "/api/migration", "ms_token", `{"exclude":["a2"]}`); w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}
	_, choices, err := ReadConfirmedPlan()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := choices.Decisions["a2"]; ok {
		t.Errorf("an excluded item must not collect defaults: %+v", choices.Decisions)
	}
}

// An answer the plan never offered is a mistake worth catching here rather
// than when something is moving.
func TestAnswersAreCheckedAgainstThePlan(t *testing.T) {
	_, h := migrationServer(t, fakePlanner{plan: testPlan()})
	do(h, "POST", "/api/migration/plan", "ms_token", "{}")

	for _, c := range []struct{ name, body string }{
		{"unknown item", `{"decisions":{"nope":{"github":"reconnect"}}}`},
		{"unasked question", `{"decisions":{"a1":{"nope":"reconnect"}}}`},
		{"option not offered", `{"decisions":{"a1":{"github":"nope"}}}`},
		{"excluding what is not there", `{"exclude":["nope"]}`},
	} {
		if got := do(h, "POST", "/api/migration", "ms_token", c.body).Code; got != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", c.name, got)
		}
	}
}

// Skipping is an answer: the console then knows the operator saw what was here.
func TestSkippingIsRecordedWithoutAPlan(t *testing.T) {
	s, h := migrationServer(t, fakePlanner{plan: testPlan()})
	if w := do(h, "POST", "/api/migration", "ms_token", `{"skipped":true}`); w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}
	if m := s.store.Get().Migration; m == nil || !m.Skipped {
		t.Fatalf("want the skip recorded, got %+v", m)
	}
	if _, err := os.Stat(filepath.Join(migrationDir, migrationPlanFile)); !os.IsNotExist(err) {
		t.Error("a skip moves nothing, so there is no plan to write")
	}
}
