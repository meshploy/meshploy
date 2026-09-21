package dokploy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// secretValues are planted in the fixture; none may appear in a plan.
var secretValues = []string{"supersecret-env-value", "db-password-123", "ghp_clonetoken", "gitlab-access-token", "s3-secret-key", "basic-auth-pass"}

func fixture() Source {
	rows := map[string][]Row{
		"project": {
			{"projectId": "p1", "name": "Shop", "env": ""},
		},
		"environment": {
			{"environmentId": "e1", "name": "production", "projectId": "p1", "isDefault": true, "env": ""},
			{"environmentId": "e2", "name": "staging", "projectId": "p1", "isDefault": false, "env": "SHARED=supersecret-env-value"},
		},
		"application": {
			{"applicationId": "a1", "name": "api", "appName": "shop-api-abc123", "environmentId": "e1", "sourceType": "gitlab", "buildType": "nixpacks",
				"gitlabPathNamespace": "team/api", "gitlabBranch": "main", "env": "A=supersecret-env-value\nDATABASE_URL=postgres://shop:db-password-123@shop-db-stu901:5432/shop\n# comment", "applicationStatus": "done", "replicas": 1},
			{"applicationId": "a2", "name": "worker", "appName": "shop-worker-def456", "environmentId": "e1", "sourceType": "docker",
				"dockerImage": "ghcr.io/shop/worker:1", "applicationStatus": "idle", "password": "db-password-123"},
			{"applicationId": "a3", "name": "admin", "appName": "shop-admin-ghi789", "environmentId": "e2", "sourceType": "git", "buildType": "dockerfile",
				"customGitUrl": "https://oauth2:ghp_clonetoken@github.com/shop/admin.git", "customGitBranch": "dev"},
			{"applicationId": "a4", "name": "site", "appName": "shop-site-jkl012", "environmentId": "e1", "sourceType": "github", "buildType": "static",
				"owner": "shop", "repository": "site", "branch": "main"},
			{"applicationId": "a5", "name": "far", "appName": "shop-far-mno345", "environmentId": "e1", "sourceType": "gitlab", "buildType": "nixpacks", "serverId": "s1"},
		},
		"security": {{"securityId": "x1", "applicationId": "a1", "username": "admin", "password": "basic-auth-pass"}},
		"redirect": {
			// A host-to-host rule, as Dokploy servers have them, and one that rewrites the path.
			{"redirectId": "r1", "applicationId": "a4", "regex": `^https?://www\.site\.example(.*)$`, "replacement": "https://site.example$1", "permanent": true},
			{"redirectId": "r2", "applicationId": "a4", "regex": `^https?://site\.example/old/(.*)`, "replacement": "https://site.example/new/${1}", "permanent": false},
		},
		"port": {{"portId": "pt1", "applicationId": "a2", "publishedPort": 8085, "targetPort": 80, "protocol": "tcp"}},
		"mount": {
			{"mountId": "m1", "type": "bind", "hostPath": "/srv/uploads", "applicationId": "a1"},
			{"mountId": "m3", "type": "bind", "hostPath": "/srv/shared", "applicationId": "a2"},
			// sh-hg's case: two apps in different environments share a folder.
			{"mountId": "m4", "type": "bind", "hostPath": "/srv/uploads", "applicationId": "a3"},
			{"mountId": "m2", "type": "volume", "volumeName": "shop-db-data", "postgresId": "d1"},
		},
		"compose": {
			{"composeId": "c1", "name": "stack", "appName": "shop-stack-pqr678", "environmentId": "e1", "sourceType": "raw", "composeType": "docker-compose", "isolatedDeployment": true,
				"composeFile": "services:\n  web:\n    environment:\n      DB_HOST: shop-wp-vwx234\n      DB_PASSWORD: supersecret-env-value\n"},
		},
		"postgres": {
			{"postgresId": "d1", "name": "db", "appName": "shop-db-stu901", "environmentId": "e1", "dockerImage": "postgres:17", "databaseName": "shop",
				"databasePassword": "db-password-123", "externalPort": 5432},
		},
		"mariadb": {{"mariadbId": "d2", "name": "wp", "appName": "shop-wp-vwx234", "environmentId": "e1", "dockerImage": "mariadb:11"}},
		"domain": {
			{"domainId": "dm1", "host": "api.shop.example", "path": "/", "https": true, "certificateType": "letsencrypt", "applicationId": "a1", "domainType": "application"},
			{"domainId": "dm2", "host": "shop-api-1-2-3-4.traefik.me", "applicationId": "a1", "domainType": "application"},
			{"domainId": "dm3", "host": "legacy.shop.example", "certificateType": "custom", "applicationId": "a4", "domainType": "application"},
			{"domainId": "dm5", "host": "site.example", "applicationId": "a4", "domainType": "application"},
			{"domainId": "dm6", "host": "www.site.example", "applicationId": "a4", "domainType": "application"},
			{"domainId": "dm4", "host": "stack.shop.example", "path": "/app", "stripPath": true, "composeId": "c1", "serviceName": "web", "domainType": "compose"},
		},
		"git_provider": {
			{"gitProviderId": "g1", "name": "GitLab", "providerType": "gitlab"},
			{"gitProviderId": "g2", "name": "GitHub", "providerType": "github"},
		},
		"destination": {{"destinationId": "b1", "name": "s3", "provider": "aws", "bucket": "backups", "secretAccessKey": "s3-secret-key"}},
		"backup": {
			{"backupId": "k1", "schedule": "0 2 * * *", "databaseType": "postgres", "keepLatestCount": 7, "enabled": true},
			{"backupId": "k2", "schedule": "0 3 * * *", "databaseType": "web-server"},
		},
		"server": {{"serverId": "s1", "name": "remote-1"}},
	}
	return Source{
		Detection: Detection{Dokploy: true, Migrations: 196, Supported: true, Version: "v0.30.6"},
		Rows:      rows,
		Docker: migrate.Docker{
			Services: []migrate.SwarmService{
				{Name: "dokploy", Running: 1, Desired: 1},
				{Name: "shop-api-abc123", Running: 1, Desired: 1},
				{Name: "shop-worker-def456", Running: 0, Desired: 0},
				{Name: "shop-db-stu901", Running: 1, Desired: 1},
				{Name: "handmade", Image: "nginx", Running: 1, Desired: 1},
			},
			Containers: []migrate.Container{
				{Name: "dokploy-traefik", Image: "traefik:v3.6", State: "running", Ports: "0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp"},
				{Name: "shop-stack-pqr678-web-1", Project: "shop-stack-pqr678", State: "running", Image: "web"},
				{Name: "shop-stack-pqr678-db-1", Project: "shop-stack-pqr678", State: "exited", Image: "postgres:16"},
				{Name: "saathealth-db-1", Project: "saathealth", State: "running", Image: "postgres:16", BindSources: []string{"/srv/shared/pg"}},
				{Name: "shop-api-abc123.1.x", Service: "shop-api-abc123", State: "running", BindSources: []string{"/srv/uploads"}},
				{Name: "shop-admin-ghi789.1.y", Service: "shop-admin-ghi789", State: "running", BindSources: []string{"/srv/uploads"}},
				{Name: "saathealth-app-1", Project: "saathealth", State: "running", Image: "app:prod"},
			},
			Volumes: []migrate.Volume{{Name: "shop-db-stu901-data", MB: 3000}, {Name: "shop-stack-pqr678_pgdata", MB: 700}},
		},
		Listeners: []migrate.Listener{{Port: 80, Address: "0.0.0.0", Process: "docker-proxy"}, {Port: 443, Address: "0.0.0.0", Process: "docker-proxy"}},
		Resources: migrate.Resources{Cores: 8, MemoryMB: 32000, AvailableMB: 24000, DiskFreeMB: 300000, DockerVolumeMB: 3000},
		PathMB:    map[string]int{"/srv/uploads": 420},
	}
}

func decision(it Item, id string) (Decision, bool) {
	for _, d := range it.Decisions {
		if d.ID == id {
			return d, true
		}
	}
	return Decision{}, false
}

func item(t *testing.T, p Plan, kind, id string) Item {
	t.Helper()
	for _, it := range p.Items {
		if it.Kind == kind && it.ID == id {
			return it
		}
	}
	t.Fatalf("no %s %s in plan", kind, id)
	return Item{}
}

func hasReason(it Item, part string) bool {
	for _, r := range it.Reasons {
		if strings.Contains(r, part) {
			return true
		}
	}
	return false
}

func TestPlanMapsEachKind(t *testing.T) {
	p := BuildPlan(fixture(), time.Now())

	if it := item(t, p, "project", "e2"); it.Name != "Shop · staging" || it.Verdict != Moves || it.Details["shared_variables"] != "1" {
		t.Errorf("staging environment: %+v", it)
	}
	api := item(t, p, "application", "a1")
	auth, hasAuth := decision(api, "basic_auth")
	mount, hasMount := decision(api, "mount:/srv/uploads")
	if api.Verdict != NeedsYou || !hasAuth || auth.Default != "keep_paused" || !hasMount || api.Details["variables"] != "2" {
		t.Errorf("api: %+v", api)
	}
	if mount.Default != "copy" || !strings.Contains(mount.Question, "admin, which moves in the same group") {
		t.Errorf("a folder shared only within the group is copied once into a shared volume: %+v", mount)
	}
	if api.Details["repository"] != "team/api" || api.Details["branch"] != "main" || api.Running == nil || !*api.Running {
		t.Errorf("api source or state: %+v", api)
	}
	worker := item(t, p, "application", "a2")
	if worker.MapsTo != "Image service" || !hasReason(worker, "created stopped") || worker.Details["published_ports"] != "8085/tcp" {
		t.Errorf("worker: %+v", worker)
	}
	// Another project's container mounts a folder inside this path: no default.
	if shared, ok := decision(worker, "mount:/srv/shared"); !ok || shared.Default != "" || !strings.Contains(shared.Question, "also mounted by saathealth") {
		t.Errorf("shared mount: %+v", worker.Decisions)
	}
	if admin := item(t, p, "application", "a3"); admin.Project != "Shop · staging" || admin.Details["repository"] != "https://github.com/shop/admin.git" {
		t.Errorf("admin: %+v", admin)
	}
	site := item(t, p, "application", "a4")
	// A GitHub connection cannot move, but that is not a question: the service
	// runs its current image and builds again after one reconnect, which it
	// says as a reason rather than asking.
	if _, asks := decision(site, "github"); asks {
		t.Errorf("a reconnect is not a decision: %+v", site.Decisions)
	}
	if !hasReason(site, "until GitHub is reconnected") || !hasReason(site, "Railpack") {
		t.Errorf("site: %+v", site)
	}
	if len(site.Redirects) != 1 || site.Redirects[0] != (Redirect{From: "www.site.example", To: "site.example", Code: 301}) {
		t.Errorf("host-to-host redirect: %+v", site.Redirects)
	}
	if rd, ok := decision(site, "redirects"); !ok || rd.Default != "" || !strings.Contains(rd.Question, "some paths of site.example") {
		t.Errorf("path-rewriting redirect: %+v", site.Decisions)
	}
	if far := item(t, p, "application", "a5"); far.Verdict != NotMoved || len(far.Reasons) != 1 {
		t.Errorf("app on a remote server: %+v", far)
	}
	if stack := item(t, p, "compose", "c1"); stack.Verdict != Moves || stack.Details["containers"] != "1 of 2 running" || stack.Details["isolated"] != "true" {
		t.Errorf("compose: %+v", stack)
	}
	db := item(t, p, "database", "d1")
	if db.Details["external_port"] != "5432" || db.Details["data_move"] != "volume copy, stopped" || db.MapsTo != "Managed Postgres" {
		t.Errorf("postgres: %+v", db)
	}
	if wp := item(t, p, "database", "d2"); !hasReason(wp, "no managed MariaDB") {
		t.Errorf("mariadb: %+v", wp)
	}
	if d := item(t, p, "domain", "dm2"); d.Verdict != NotMoved {
		t.Errorf("traefik.me domain: %+v", d)
	}
	if d := item(t, p, "domain", "dm3"); d.Verdict != NeedsYou || d.Decisions[0].Default != "caddy" {
		t.Errorf("custom certificate: %+v", d)
	}
	if d := item(t, p, "domain", "dm4"); d.MapsTo != "Route to stack / web" || d.Details["path"] != "/app" {
		t.Errorf("compose domain: %+v", d)
	}
	// The integration moves with its repositories and branches; only its
	// credentials stay behind.
	if g := item(t, p, "git_provider", "g2"); g.Verdict != Moves || len(g.Decisions) != 0 ||
		!hasReason(g, "reconnect it once in Meshploy") {
		t.Errorf("github provider: %+v", g)
	}
	// The shared mount and the path-rewriting redirect have no safe default.
	if p.Summary["open_decisions"] != 2 {
		t.Errorf("open decisions = %d, want 2", p.Summary["open_decisions"])
	}
	if k := item(t, p, "backup", "k2"); k.Verdict != NotMoved {
		t.Errorf("web-server backup: %+v", k)
	}
	if s := item(t, p, "server", "s1"); s.Verdict != NotMoved {
		t.Errorf("remote server: %+v", s)
	}
	if p.Summary[Moves]+p.Summary[NeedsYou]+p.Summary[NotMoved] != len(p.Items) {
		t.Errorf("summary %v does not add up to %d items", p.Summary, len(p.Items))
	}
}

// The plan is printed and written to disk; nothing read from Dokploy's
// database that is a secret may reach it.
func TestPlanCarriesNoSecrets(t *testing.T) {
	b, err := json.Marshal(BuildPlan(fixture(), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secretValues {
		if strings.Contains(string(b), secret) {
			t.Errorf("plan contains %q", secret)
		}
	}
}

func TestPlanFindsUnmanagedWorkloads(t *testing.T) {
	p := BuildPlan(fixture(), time.Now())
	names := map[string]Unmanaged{}
	for _, u := range p.Unmanaged {
		names[u.Name] = u
	}
	if u, ok := names["saathealth"]; !ok || u.Kind != "compose-project" || u.Total != 2 {
		t.Errorf("hand-run compose project: %+v", names)
	}
	if _, ok := names["handmade"]; !ok {
		t.Error("a Swarm service no row owns was missed")
	}
	for _, own := range []string{"dokploy", "dokploy-traefik", "shop-stack-pqr678", "shop-api-abc123"} {
		if _, ok := names[own]; ok {
			t.Errorf("%s is Dokploy's or owned by a row, not unmanaged", own)
		}
	}
}

func TestPlanReadsTheEdge(t *testing.T) {
	src := fixture()
	if e := BuildPlan(src, time.Now()).Edge; e.Kind != "traefik-container" {
		t.Errorf("default edge: %+v", e)
	}

	src.Docker.Services = append(src.Docker.Services, migrate.SwarmService{Name: "dokploy-traefik", Running: 1, Desired: 1})
	if e := BuildPlan(src, time.Now()).Edge; e.Kind != "traefik-service" {
		t.Errorf("Traefik as a Swarm service: %+v", e)
	}

	// sh-hg: a host Caddy in front, Traefik moved to other ports.
	src = fixture()
	src.Docker.Containers[0].Ports = "0.0.0.0:6080->80/tcp, 0.0.0.0:6443->443/tcp"
	src.Listeners = []migrate.Listener{{Port: 80, Address: "*", Process: "caddy"}, {Port: 443, Address: "*", Process: "caddy"}}
	if e := BuildPlan(src, time.Now()).Edge; e.Kind != "custom" || e.Holders[0] != "caddy" || !strings.Contains(e.Traefik, "6080") {
		t.Errorf("custom edge: %+v", e)
	}
}

func TestPlanChoosesAMode(t *testing.T) {
	src := fixture()
	if m := BuildPlan(src, time.Now()).Mode.Choice; m != ModeSideBySide {
		t.Errorf("large host: %s", m)
	}
	src.Resources = migrate.Resources{Cores: 1, MemoryMB: 3800, AvailableMB: 1400, DiskFreeMB: 30000, DockerVolumeMB: 200}
	if m := BuildPlan(src, time.Now()).Mode.Choice; m != ModeHandOver {
		t.Errorf("small host: %s", m)
	}
	// tcm-hg's shape: 4 GB free, but its workloads use far more than that, so
	// a second copy does not fit beside them.
	src.Docker.Containers[1].MemoryMB = 9000
	if m := BuildPlan(src, time.Now()); m.Mode.Choice != ModeHandOver || m.Resources.WorkloadMemoryMB != 9000 {
		t.Errorf("workloads too big to run twice: %s, workload memory %d", m.Mode.Choice, m.Resources.WorkloadMemoryMB)
	}
	src.Resources = migrate.Resources{Cores: 4, MemoryMB: 15000, AvailableMB: 4000, DiskFreeMB: 5000, DockerVolumeMB: 40000}
	src.Docker.Volumes = []migrate.Volume{{Name: "big-data", MB: 16000}}
	if m := BuildPlan(src, time.Now()).Mode.Choice; m != ModeMoveToNode {
		t.Errorf("data too large for the disk: %s", m)
	}
}

// Consumers read these as lists, so none is null.
func TestPlanListsAreNeverNull(t *testing.T) {
	b, _ := json.Marshal(BuildPlan(Source{Detection: Detection{Dokploy: true, Supported: true}}, time.Now()))
	for _, field := range []string{`"items":null`, `"unmanaged":null`, `"holders":null`} {
		if strings.Contains(string(b), field) {
			t.Errorf("%s in %s", field, b)
		}
	}
}

// The rules on the reference servers, copied as they are: each sends hostnames
// of the app to another, keeping the path.
func TestResolveRealRedirects(t *testing.T) {
	cases := []struct {
		regex, replacement string
		hosts              []string
		want               []Redirect
	}{
		{`^https?://(?:www\.)?solontio\.(?:net|cloud)/(.*)`, "https://solontio.com/${1}",
			[]string{"solontio.net", "www.solontio.cloud", "solontio.com"},
			[]Redirect{{"solontio.net", "solontio.com", 301}, {"www.solontio.cloud", "solontio.com", 301}}},
		{`^https?://www\.saathealth\.com(.*)$`, "https://saathealth.com$1",
			[]string{"www.saathealth.com", "saathealth.com"},
			[]Redirect{{"www.saathealth.com", "saathealth.com", 301}}},
		{`^https?://(www\.)?tcmstunner\.in(.*)$`, "https://tcmstunner.com$2",
			[]string{"tcmstunner.in", "www.tcmstunner.in"},
			[]Redirect{{"tcmstunner.in", "tcmstunner.com", 301}, {"www.tcmstunner.in", "tcmstunner.com", 301}}},
	}
	migrated := map[string]bool{"solontio.com": true, "saathealth.com": true, "tcmstunner.com": true}
	for _, c := range cases {
		got, unresolved := resolveRedirects([]Row{{"regex": c.regex, "replacement": c.replacement, "permanent": true}}, c.hosts, migrated)
		if len(unresolved) != 0 || len(got) != len(c.want) {
			t.Errorf("%s: resolved %+v, unresolved %v", c.regex, got, unresolved)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %+v, want %+v", c.regex, got[i], c.want[i])
			}
		}
	}

	// Sending to a host that does not move cannot be a route redirect.
	_, unresolved := resolveRedirects([]Row{{"regex": `^https?://a\.example(.*)`, "replacement": "https://elsewhere.example$1"}}, []string{"a.example"}, migrated)
	if len(unresolved) != 1 || !strings.Contains(unresolved[0], "not a migrated domain") {
		t.Errorf("unresolved = %v", unresolved)
	}
}

func groupWith(t *testing.T, p Plan, id string) Group {
	t.Helper()
	for _, g := range p.Groups {
		for _, m := range g.Members {
			if m.ID == id {
				return g
			}
		}
	}
	t.Fatalf("%s is in no group", id)
	return Group{}
}

// A database moves with every app that uses it, found from the app's env or
// its compose file, so no data lives in two places while they move.
func TestPlanGroupsDatabasesWithTheirApps(t *testing.T) {
	p := BuildPlan(fixture(), time.Now())

	// api names the database in DATABASE_URL, and shares a folder with admin.
	g := groupWith(t, p, "a1")
	if len(g.Members) != 3 || groupWith(t, p, "d1").ID != g.ID || groupWith(t, p, "a3").ID != g.ID {
		t.Errorf("api, its database and the app sharing its folder: %+v", g)
	}
	if len(g.Data) != 2 || g.Data[0].MB+g.Data[1].MB != 3420 || !g.CanMove {
		t.Errorf("the shared folder is copied once: %+v", g.Data)
	}
	// Members span two projects (Shop, Shop · staging), so no project prefix.
	if g.Name != "db with admin, api" {
		t.Errorf("name = %q", g.Name)
	}

	stack := groupWith(t, p, "c1")
	if groupWith(t, p, "d2").ID != stack.ID || !hasData(stack.Data, "shop-stack-pqr678_pgdata") {
		t.Errorf("a compose file naming a database groups them, and its own volume is data: %+v", stack)
	}
	if !strings.HasPrefix(stack.Name, "Shop / ") {
		t.Errorf("a group within one project is named with it: %q", stack.Name)
	}

	worker := groupWith(t, p, "a2")
	if worker.CanMove || len(worker.Blockers) != 1 || len(worker.Members) != 1 {
		t.Errorf("a group with a decision that has no choice cannot move: %+v", worker)
	}
	if worker.Downtime != "a restart plus under a minute to copy data" && worker.Downtime != "a restart, usually under a minute" {
		t.Errorf("downtime = %q", worker.Downtime)
	}

	// Every moving application, compose app and database is in exactly one
	// group; one on a remote server is in none.
	seen := map[string]int{}
	for _, gr := range p.Groups {
		for _, m := range gr.Members {
			seen[m.ID]++
		}
	}
	for _, it := range p.Items {
		switch it.Kind {
		case "application", "compose", "database":
			want := 1
			if it.Verdict == NotMoved {
				want = 0
			}
			if seen[it.ID] != want {
				t.Errorf("%s %s is in %d groups, want %d", it.Kind, it.Name, seen[it.ID], want)
			}
		}
	}
	// Groups that can move come before blocked ones, and among them those
	// without data first.
	rank := func(g Group) int {
		switch {
		case !g.CanMove:
			return 2
		case len(g.Data) > 0:
			return 1
		}
		return 0
	}
	for i := 1; i < len(p.Groups); i++ {
		if rank(p.Groups[i]) < rank(p.Groups[i-1]) {
			t.Errorf("group %d (%s) is out of order", i, p.Groups[i].Name)
		}
	}
	if rank(p.Groups[len(p.Groups)-1]) != 2 {
		t.Error("the blocked groups are not last")
	}
}

func TestDowntimeEstimate(t *testing.T) {
	for _, c := range []struct {
		minutes float64
		data    bool
		want    string
	}{
		{0, false, "a restart, usually under a minute"},
		{0.4, true, "a restart plus under a minute to copy data"},
		{7.2, true, "a restart plus about 8 minutes to copy data"},
	} {
		if got := downtimeEstimate(c.minutes, c.data); got != c.want {
			t.Errorf("%v: %q", c, got)
		}
	}
}

func TestSupportRange(t *testing.T) {
	for n, want := range map[int]bool{132: false, 133: true, 191: true, 196: true, 197: false} {
		if got := n >= MinMigrations && n <= MaxMigrations; got != want {
			t.Errorf("level %d supported=%v", n, got)
		}
	}
}

// Meshploy installs itself on this host as a compose project, so without a
// filter of its own it turned up in the plan as something to import: the
// platform offering to run itself as one of its own workloads.
func TestThePlanDoesNotOfferToImportMeshployItself(t *testing.T) {
	src := Source{Docker: migrate.Docker{
		Containers: []migrate.Container{
			{Name: "meshploy-api-1", Project: "meshploy", Image: "ghcr.io/meshploy/api", State: "running"},
			{Name: "meshploy-postgres-1", Project: "meshploy", Image: "postgres:17", State: "running"},
			{Name: "dokploy-traefik", Image: "traefik:v3", State: "running"},
			{Name: "wakapi", Image: "wakapi:latest", State: "running"},
		},
	}}
	got := unmanaged(src, map[string]bool{})
	if len(got) != 1 || got[0].Name != "wakapi" {
		t.Fatalf("unmanaged = %+v, want only the one workload that is not a platform", got)
	}
}

// A published database port can only be taken if nothing else on the gateway
// holds it - and Meshploy's own database sits on 5433, which is exactly the
// kind of port another platform publishes one on. The plan says so while it can
// still be changed.
func TestAPublishedPortAlreadyInUseIsNamedInThePlan(t *testing.T) {
	src := Source{Rows: map[string][]Row{
		"project":     {{"projectId": "p1", "name": "Shop"}},
		"environment": {{"environmentId": "e1", "projectId": "p1", "name": "production"}},
		"postgres": {{
			"postgresId": "d1", "name": "ordersdb", "environmentId": "e1", "appName": "orders-db",
			"dockerImage": "postgres:16", "databaseName": "orders", "databaseUser": "orders",
			"externalPort": "5433",
		}},
	}}
	src.Listeners = []migrate.Listener{
		{Port: 5433, Address: "127.0.0.1", Process: "postgres"},
		{Port: 8081, Address: "127.0.0.1", Process: "proxy"},
	}

	plan := BuildPlan(src, time.Now())
	var reasons string
	for _, it := range plan.Items {
		if it.Kind == "database" {
			reasons = strings.Join(it.Reasons, " | ")
		}
	}
	if !strings.Contains(reasons, "already in use") || !strings.Contains(reasons, "postgres") {
		t.Errorf("the collision should be named: %q", reasons)
	}

	// With the port free, there is nothing to warn about.
	src.Listeners = []migrate.Listener{{Port: 8081, Address: "127.0.0.1", Process: "proxy"}}
	for _, it := range BuildPlan(src, time.Now()).Items {
		if it.Kind == "database" && strings.Contains(strings.Join(it.Reasons, " "), "already in use") {
			t.Errorf("nothing holds the port: %v", it.Reasons)
		}
	}
}

// The edge the host found holding the ports decides what cutover will stop,
// because on a server whose edge is not the platform's there is nothing else to
// go on.
func TestReadEdgeUsesWhatTheHostFound(t *testing.T) {
	cases := []struct {
		name   string
		holder migrate.PortHolder
		kind   string
	}{
		{"the platform's own edge as a Swarm service",
			migrate.PortHolder{Kind: "swarm", Name: "dokploy-traefik"}, "traefik-service"},
		{"the platform's own edge as a container",
			migrate.PortHolder{Kind: "container", Name: "dokploy-traefik"}, "traefik-container"},
		{"a host nginx under systemd",
			migrate.PortHolder{Kind: "systemd", Name: "nginx.service", Detail: "nginx runs under the unit nginx.service"}, "custom"},
		{"somebody's node process",
			migrate.PortHolder{Kind: "process", Name: "node", Detail: "node (pid 7) holds it, outside any container or unit"}, "custom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := readEdge(Source{EdgeHolder: c.holder})
			if got.Kind != c.kind {
				t.Fatalf("kind: got %q, want %q", got.Kind, c.kind)
			}
			if got.Holder == nil || got.Holder.Name != c.holder.Name {
				t.Fatalf("the holder was not carried into the plan: %+v", got.Holder)
			}
		})
	}
}

// A holder nothing supervises is named, and the plan says the handover cannot
// do it alone rather than promising it will.
func TestReadEdgeSaysWhenNothingCanStopTheEdge(t *testing.T) {
	e := readEdge(Source{EdgeHolder: migrate.PortHolder{Kind: "process", Name: "node", Detail: "node (pid 7) holds it"}})
	if !strings.Contains(e.Note, "by hand") {
		t.Fatalf("note: %q", e.Note)
	}
}
