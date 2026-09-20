package dokploy

import (
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// A database is grouped with the applications that use it even when the
// platform encrypts what it stores.
//
// Current Dokploy does: the env column holds "enc:v1:…", which contains no
// hostname, so searching it found nothing and every database came out as a
// group of its own - letting an application move away from its data. What the
// workload runs with is read from Docker instead.
func TestADatabaseIsGroupedByWhatTheApplicationRunsWith(t *testing.T) {
	src := Source{Rows: map[string][]Row{
		"project":     {{"projectId": "p1", "name": "Shop"}},
		"environment": {{"environmentId": "e1", "projectId": "p1", "name": "production"}},
		"application": {{
			"applicationId": "a1", "name": "admin", "environmentId": "e1", "appName": "shop-admin",
			"env": "enc:v1:0pAqUeblOb", "sourceType": "docker", "dockerImage": "adminer:4",
		}},
		"postgres": {{
			"postgresId": "d1", "name": "shopdb", "environmentId": "e1", "appName": "shop-db",
			"dockerImage": "postgres:16", "databaseName": "shop", "databaseUser": "shop",
		}},
		// A domain row carries the applicationId of what it points at, so it
		// answers to that id too - with no appName of its own. Whichever row a
		// map hands over first, the lookup has to find the one that names the
		// workload.
		"domain": {{"domainId": "dm1", "applicationId": "a1", "host": "shop.example.com",
			"port": 8080, "https": true, "certificateType": "letsencrypt", "enabled": true}},
	}}
	src.Docker.Services = []migrate.SwarmService{
		{Name: "shop-admin", Image: "adminer:4", Running: 1, Desired: 1,
			Env: []string{"DATABASE_URL=postgres://shop:pw@shop-db:5432/shop"}},
		{Name: "shop-db", Image: "postgres:16", Running: 1, Desired: 1},
	}

	plan := BuildPlan(src, time.Now())
	if len(plan.Groups) != 1 {
		var names []string
		for _, g := range plan.Groups {
			names = append(names, g.Name)
		}
		t.Fatalf("the database and the app that reads it are one group, got %d: %v", len(plan.Groups), names)
	}
	if len(plan.Groups[0].Members) != 2 {
		t.Errorf("members = %+v", plan.Groups[0].Members)
	}
}
