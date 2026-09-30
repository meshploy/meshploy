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

// A failure stops what depends on it, and what depends on that, and nothing
// else: a failed admin tool stopped every service after it.
func TestAFailureStopsOnlyWhatDependsOnIt(t *testing.T) {
	item := func(name string) rollItem { return rollItem{ID: uuid.New(), Name: name} }
	dependsOn := map[string][]string{
		"pgadmin":  {"db"},
		"api":      {"migrator"},
		"reports":  {"pgadmin"},
		"ui":       {"api"},
		"exporter": {"reports"},
	}
	blocked := map[string]string{"pgadmin": "pgadmin"}

	start := holdBack([]rollItem{item("api"), item("reports")}, dependsOn, blocked)
	if len(start) != 1 || start[0].Name != "api" {
		t.Errorf("layer 2 starts %+v, want api only", start)
	}
	start = holdBack([]rollItem{item("ui"), item("exporter")}, dependsOn, blocked)
	if len(start) != 1 || start[0].Name != "ui" {
		t.Errorf("layer 3 starts %+v, want ui only", start)
	}
	if blocked["exporter"] != "pgadmin" {
		t.Errorf("exporter is held back for %q, want the failure behind it, pgadmin", blocked["exporter"])
	}
}

// A cap on parallel builds starts that many builds and everything that only
// runs an image; the rest of the builds wait their turn, in order.
func TestACapOnParallelBuildsQueuesTheRest(t *testing.T) {
	item := func(name string, builds bool) rollItem { return rollItem{ID: uuid.New(), Name: name, Builds: builds} }
	layer := []rollItem{item("api", true), item("redis", false), item("worker", true), item("ui", true), item("proxy", false)}

	now, later := capBuilds(layer, 2)
	names := func(items []rollItem) (out []string) {
		for _, it := range items {
			out = append(out, it.Name)
		}
		return
	}
	if got := names(now); len(got) != 4 || got[0] != "api" || got[1] != "redis" || got[2] != "worker" || got[3] != "proxy" {
		t.Errorf("now %v", got)
	}
	if got := names(later); len(got) != 1 || got[0] != "ui" {
		t.Errorf("later %v", got)
	}
	if now, later := capBuilds(layer, 0); len(now) != len(layer) || len(later) != 0 {
		t.Errorf("no cap held back %v", names(later))
	}
}

// A run picked up after a restart goes on from the layer it was in: that
// layer's deployments are waited on, what it still queued under the build cap
// starts after them, and the waiting layers follow in order.
func TestAResumedRunGoesOnFromTheLayerItWasIn(t *testing.T) {
	d1, d2 := uuid.New(), uuid.New()
	steps := []RolloutStep{
		{Name: "db", ServiceID: uuid.New(), Layer: 0, Status: RolloutSucceeded, DeploymentID: &d1},
		{Name: "api", ServiceID: uuid.New(), Layer: 1, Status: RolloutStarted, DeploymentID: &d2},
		{Name: "worker", ServiceID: uuid.New(), Layer: 1, Status: RolloutWaiting},
		{Name: "ui", ServiceID: uuid.New(), Layer: 3, Status: RolloutWaiting},
		{Name: "proxy", ServiceID: uuid.New(), Layer: 2, Status: RolloutWaiting},
	}
	item := func(st RolloutStep) rollItem { return rollItem{ID: st.ServiceID, Name: st.Name} }
	previous, queued, layers := resumePlan(steps, item)
	if len(previous) != 1 || previous[0] != d2 {
		t.Errorf("previous %v, want the api's deployment", previous)
	}
	if len(queued) != 1 || queued[0].Name != "worker" {
		t.Errorf("queued %+v", queued)
	}
	if len(layers) != 2 || layers[0][0].Name != "proxy" || layers[1][0].Name != "ui" {
		t.Errorf("layers %+v", layers)
	}

	// A run a restart caught before anything started starts from its first layer.
	fresh := []RolloutStep{{Name: "db", Layer: 0, Status: RolloutWaiting}, {Name: "api", Layer: 1, Status: RolloutWaiting}}
	previous, queued, layers = resumePlan(fresh, item)
	if len(previous) != 0 || len(queued) != 0 || len(layers) != 2 || layers[0][0].Name != "db" {
		t.Errorf("fresh: %v %v %+v", previous, queued, layers)
	}
}
