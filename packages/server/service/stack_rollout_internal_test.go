package service

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
)

// A stack rolls out in the order compose starts it: what depends on nothing
// first, then each service once everything it depends on is up.
func TestAStackRollsOutInDependsOnOrder(t *testing.T) {
	dep := func(names ...string) composetypes.DependsOnConfig {
		out := composetypes.DependsOnConfig{}
		for _, n := range names {
			out[n] = composetypes.ServiceDependency{Condition: composetypes.ServiceConditionStarted}
		}
		return out
	}
	services := composetypes.Services{
		"db":       {Name: "db"},
		"broker":   {Name: "broker"},
		"migrator": {Name: "migrator", DependsOn: dep("db")},
		"topics":   {Name: "topics", DependsOn: dep("broker")},
		"api":      {Name: "api", DependsOn: dep("migrator", "topics")},
		"ui":       {Name: "ui", DependsOn: dep("api")},
	}
	layers := serviceLayers(services)
	for name, want := range map[string]int{"db": 0, "broker": 0, "migrator": 1, "topics": 1, "api": 2, "ui": 3} {
		if layers[name] != want {
			t.Errorf("%s: layer %d, want %d", name, layers[name], want)
		}
	}

	// Only what this apply rolls out is grouped, and empty layers go.
	items := []rollItem{{ID: uuid.New(), Name: "ui"}, {ID: uuid.New(), Name: "db"}, {ID: uuid.New(), Name: "api"}}
	got := rolloutLayers(items, layers)
	if len(got) != 3 || got[0][0].Name != "db" || got[1][0].Name != "api" || got[2][0].Name != "ui" {
		t.Errorf("layers = %+v", got)
	}
}

// A step that runs once and has completed runs again with a change, as it
// does on every deploy; a stopped service still does not start.
func TestACompletedStepRunsAgainWithAChange(t *testing.T) {
	deploy, warnings := rolloutPlan([]changedService{
		{Name: "migrator", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceCompleted},
		{Name: "worker", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceStopped},
	})
	if len(deploy) != 1 || deploy[0].Name != "migrator" {
		t.Errorf("deploy = %+v", deploy)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v", warnings)
	}
}

// A failed layer stops what depends on it, and the run says which never
// started.
func TestAFailedLayerStopsTheRest(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	dep := uuid.New()
	r := &runTracker{steps: []RolloutStep{
		{Name: "migrator", ServiceID: a, Layer: 0, Status: RolloutStarted, DeploymentID: &dep},
		{Name: "api", ServiceID: b, Layer: 1, Status: RolloutWaiting},
	}}
	r.stepByDeployment(dep, RolloutFailed)
	if !r.anyFailed() {
		t.Fatal("the migrator failed")
	}
	r.stopRest()
	if r.steps[1].Status != RolloutNotStarted {
		t.Errorf("api: %s", r.steps[1].Status)
	}
	var nilRun *runTracker
	nilRun.stopRest() // a run not kept is a no-op, not a panic
}
