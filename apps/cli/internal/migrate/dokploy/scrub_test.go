package dokploy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// A fixture is committed and read by strangers, so nothing of the server may
// survive - and everything the plan reads must.
func TestScrubKeepsTheShapeAndDropsTheServer(t *testing.T) {
	src := Source{
		Detection: Detection{Dokploy: true, Version: "v0.30.5", Migrations: 191, Supported: true},
		Rows: map[string][]Row{
			"project": {{"projectId": "p1", "name": "Acme Internal"}},
			"application": {{
				"applicationId": "a1", "name": "billing-api", "appName": "acme-billing-x1y2",
				"environmentId": "e1", "sourceType": "github", "owner": "acme-inc", "repository": "billing",
				"branch": "main",
				"env":    "DATABASE_URL=postgres://acme:hunter2@db:5432/acme\nSTRIPE_KEY=sk_live_abcdef",
			}},
			"postgres": {{"postgresId": "d1", "name": "db", "appName": "acme-db-z9", "databaseName": "acme",
				"databasePassword": "hunter2"}},
			"domain": {{"domainId": "dm1", "host": "billing.acme.example.com", "applicationId": "a1", "https": true}},
			"mount":  {{"mountId": "m1", "type": "bind", "hostPath": "/home/acme/uploads", "applicationId": "a1"}},
		},
		Docker: migrate.Docker{
			Containers: []migrate.Container{{
				Name: "acme-billing-x1y2.1.abc", Image: "registry.acme.example.com/billing:1.4",
				State: "running", Service: "acme-billing-x1y2", BindSources: []string{"/home/acme/uploads"},
			}},
			Services: []migrate.SwarmService{{Name: "acme-billing-x1y2", Image: "registry.acme.example.com/billing:1.4", Running: 1, Desired: 1}},
			Volumes:  []migrate.Volume{{Name: "acme-db-z9-data", MB: 900}},
		},
		PathMB: map[string]int{"/home/acme/uploads": 340},
	}

	out := Scrub(src)
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)

	// Nothing of theirs.
	for _, secret := range []string{
		"hunter2", "sk_live_abcdef", "Acme Internal", "acme-inc", "billing-api",
		"billing.acme.example.com", "/home/acme/uploads", "registry.acme.example.com",
		"acme-billing-x1y2", "acme-db-z9",
	} {
		if strings.Contains(text, secret) {
			t.Errorf("%q survived the scrub", secret)
		}
	}

	// Everything the plan reads.
	app := out.Rows["application"][0]
	for _, keep := range []string{"applicationId", "environmentId", "sourceType", "branch", "name", "appName", "repository"} {
		if app.Str(keep) == "" {
			t.Errorf("application lost %s, which the plan reads", keep)
		}
	}
	if app.Str("sourceType") != "github" || app.Str("branch") != "main" {
		t.Errorf("structure changed: %+v", app)
	}
	// An env block keeps its line count: the plan reports how many variables
	// move.
	if got := app.Lines("env"); got != 2 {
		t.Errorf("env lines: got %d, want 2", got)
	}

	// An app and the swarm service running it must still name each other, or
	// grouping falls apart.
	if app.Str("appName") != out.Docker.Services[0].Name {
		t.Errorf("the app and its service stopped matching: %q vs %q", app.Str("appName"), out.Docker.Services[0].Name)
	}
	// And a volume must stay attached to the workload it belongs to, which the
	// plan reads from its name's prefix.
	db := out.Rows["postgres"][0]
	if !strings.HasPrefix(out.Docker.Volumes[0].Name, db.Str("appName")) {
		t.Errorf("the volume lost its owner: %q is not under %q", out.Docker.Volumes[0].Name, db.Str("appName"))
	}
	// So must a bind mount and the container that mounts it.
	if out.Rows["mount"][0].Str("hostPath") != out.Docker.Containers[0].BindSources[0] {
		t.Errorf("the mount and its container stopped matching")
	}
	// And the sizes, which the downtime estimate is built from.
	if out.Docker.Volumes[0].MB != 900 || len(out.PathMB) != 1 {
		t.Errorf("sizes changed: %+v %+v", out.Docker.Volumes, out.PathMB)
	}
	for _, mb := range out.PathMB {
		if mb != 340 {
			t.Errorf("path size changed: %d", mb)
		}
	}
}

// The same value scrubs to the same stand-in every time, so a fixture can be
// regenerated and diffed rather than re-read.
func TestScrubIsStable(t *testing.T) {
	src := Source{Rows: map[string][]Row{
		"project":     {{"projectId": "p1", "name": "Acme"}},
		"application": {{"applicationId": "a1", "name": "web", "appName": "acme-web-1"}},
	}}
	first, _ := json.Marshal(Scrub(src))
	second, _ := json.Marshal(Scrub(src))
	if string(first) != string(second) {
		t.Error("two scrubs of the same reading differ")
	}
}
