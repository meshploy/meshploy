package dokploy

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// fakeAPI stands in for Meshploy, recording what prepare asked it to create.
type fakeAPI struct {
	projects   map[string]string // name -> id
	services   []ServiceSpec
	envs       map[string]string // service id -> env block
	routes     []RouteSpec
	volumes    map[string]string // name -> id
	volumeSize map[string]int
	mounts     []string
	files      []string
	fail       map[string]error // target name -> error
	n          int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{projects: map[string]string{}, envs: map[string]string{}, fail: map[string]error{},
		volumes: map[string]string{}, volumeSize: map[string]int{}}
}

func (f *fakeAPI) next(prefix string) string {
	f.n++
	return fmt.Sprintf("%s-%d", prefix, f.n)
}

func (f *fakeAPI) CreateProject(name string) (string, error) {
	if err := f.fail[name]; err != nil {
		return "", err
	}
	if id, ok := f.projects[name]; ok {
		return id, nil // an existing project is reused, not duplicated
	}
	id := f.next("proj")
	f.projects[name] = id
	return id, nil
}

func (f *fakeAPI) CreateService(projectID string, spec ServiceSpec) (string, error) {
	if err := f.fail[spec.Name]; err != nil {
		return "", err
	}
	f.services = append(f.services, spec)
	return f.next("svc"), nil
}

func (f *fakeAPI) SetEnvVars(projectID, serviceID, env string) error {
	f.envs[serviceID] = env
	return nil
}

func (f *fakeAPI) CreateVolume(projectID, name string, storageGB int) (string, error) {
	if err := f.fail[name]; err != nil {
		return "", err
	}
	if id, ok := f.volumes[name]; ok {
		return id, nil
	}
	id := f.next("vol")
	f.volumes[name] = id
	f.volumeSize[name] = storageGB
	return id, nil
}

func (f *fakeAPI) AttachVolume(projectID, volumeID, serviceID, mountPath string) error {
	f.mounts = append(f.mounts, volumeID+" -> "+serviceID+":"+mountPath)
	return nil
}

func (f *fakeAPI) CreateConfigFile(projectID, serviceID, name, path, content string) error {
	f.files = append(f.files, name+" at "+path+" = "+content)
	return nil
}

func (f *fakeAPI) CreateRoute(projectID string, spec RouteSpec) (string, error) {
	if err := f.fail[spec.Hostname]; err != nil {
		return "", err
	}
	f.routes = append(f.routes, spec)
	return f.next("route"), nil
}

func (f *fakeAPI) service(name string) (ServiceSpec, bool) {
	for _, s := range f.services {
		if s.Name == name {
			return s, true
		}
	}
	return ServiceSpec{}, false
}

// A small server: one project, an app with a domain, and a database.
func smallPlan(t *testing.T) (Plan, Source) {
	t.Helper()
	src := Source{Rows: map[string][]Row{
		"project":     {{"projectId": "p1", "name": "Acme"}},
		"environment": {{"environmentId": "e1", "projectId": "p1", "name": "production"}},
		"application": {{
			"applicationId": "a1", "name": "web", "environmentId": "e1", "appName": "web-abc",
			"env":        "PORT=3000\nDATABASE_URL=postgres://db-xyz/app",
			"repository": "web", "owner": "acme", "branch": "main", "sourceType": "github",
		}},
		"postgres": {{
			"postgresId": "d1", "name": "db", "environmentId": "e1", "appName": "db-xyz",
			"dockerImage": "postgres:16", "databaseName": "app", "databaseUser": "app",
			"databasePassword": "s3cret", "env": "",
		}},
		"domain": {{"domainId": "dm1", "applicationId": "a1", "host": "web.example.com", "port": 3000,
			"https": true, "certificateType": "letsencrypt", "enabled": true}},
	}}
	// What is actually running, which is where the image comes from: an app
	// built from git has no stored image, and after a rollback in Dokploy the
	// stored one and the running one differ.
	src.Docker.Services = []migrate.SwarmService{
		{Name: "web-abc", Image: "ghcr.io/acme/web:sha-9f3", Running: 1, Desired: 1},
		{Name: "db-xyz", Image: "postgres:16", Running: 1, Desired: 1},
	}
	return BuildPlan(src, time.Now()), src
}

func newJournal(t *testing.T) *journal.Journal {
	t.Helper()
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

// Stage 1 builds the Meshploy side and nothing serves yet.
func TestPrepareCreatesProjectsServicesAndRoutes(t *testing.T) {
	plan, src := smallPlan(t)
	api := newFakeAPI()

	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failures) != 0 {
		t.Fatalf("failures: %v", out.Failures)
	}

	if len(api.projects) != 1 {
		t.Errorf("projects = %v", api.projects)
	}
	if len(api.services) != 2 {
		t.Fatalf("services = %+v", api.services)
	}

	// The image that runs now, not the latest build: after a rollback in
	// Dokploy those differ, and what is running is the truth.
	web, ok := api.service("web")
	if !ok {
		t.Fatal("the application was not created")
	}
	if web.Image != "ghcr.io/acme/web:sha-9f3" {
		t.Errorf("image = %q", web.Image)
	}
	// Its source is kept, so the next deploy builds as usual and the migration
	// never depends on a build succeeding.
	if web.GitRepo == "" || web.Branch != "main" {
		t.Errorf("git source = %q %q", web.GitRepo, web.Branch)
	}
	if !strings.Contains(web.EnvVars, "DATABASE_URL=") {
		t.Errorf("env = %q", web.EnvVars)
	}

	db, ok := api.service("db")
	if !ok {
		t.Fatal("the database was not created")
	}
	if db.Type != "database" || db.DBName != "app" || db.DBUser != "app" || db.Password != "s3cret" {
		t.Errorf("database = %+v", db)
	}

	if len(api.routes) != 1 {
		t.Fatalf("routes = %+v", api.routes)
	}
	r := api.routes[0]
	if r.Hostname != "web.example.com" || r.Port != 3000 {
		t.Errorf("route = %+v", r)
	}
	if r.ServiceID != out.Services["a1"] {
		t.Errorf("the route points at %q, want the service created for the app", r.ServiceID)
	}
}

// Secrets are read at apply time and never written to the plan, which is what
// makes plan.json safe to keep and to show.
func TestPrepareReadsSecretsFromDokployNotFromThePlan(t *testing.T) {
	plan, src := smallPlan(t)
	for _, it := range plan.Items {
		for k, v := range it.Details {
			if strings.Contains(v, "s3cret") || strings.Contains(v, "DATABASE_URL=") {
				t.Fatalf("plan item %s carries a secret in %s: %q", it.Name, k, v)
			}
		}
	}
	api := newFakeAPI()
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)}); err != nil {
		t.Fatal(err)
	}
	db, _ := api.service("db")
	if db.Password != "s3cret" {
		t.Error("the password should have been read from Dokploy's rows at apply time")
	}
}

// Run again, it creates nothing twice - which is what makes a failed stage
// resumable rather than restartable.
func TestPrepareIsResumable(t *testing.T) {
	plan, src := smallPlan(t)
	api := newFakeAPI()
	j := newJournal(t)

	first, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	created := first.Created

	second, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 {
		t.Errorf("the second run created %d things", second.Created)
	}
	if second.Skipped != created {
		t.Errorf("skipped %d, created %d on the first run", second.Skipped, created)
	}
	if len(api.services) != 2 {
		t.Errorf("services were created twice: %+v", api.services)
	}
}

// One failure does not stop the stage: the operator wants the whole list, not
// one at a time. And the failed step is retried on the next run.
func TestPrepareCollectsFailuresAndRetriesThem(t *testing.T) {
	plan, src := smallPlan(t)
	api := newFakeAPI()
	api.fail["db"] = fmt.Errorf("engine not supported")
	j := newJournal(t)

	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "engine not supported") {
		t.Fatalf("failures = %v", out.Failures)
	}
	// The rest was still created.
	if _, ok := api.service("web"); !ok {
		t.Error("the application should have been created anyway")
	}

	delete(api.fail, "db")
	again, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Failures) != 0 {
		t.Fatalf("failures on retry = %v", again.Failures)
	}
	if _, ok := api.service("db"); !ok {
		t.Error("the retry should have created the database")
	}
}

// A route whose workload was not created is reported rather than created
// pointing at nothing.
func TestARouteWithoutItsWorkloadIsReported(t *testing.T) {
	plan, src := smallPlan(t)
	api := newFakeAPI()
	api.fail["web"] = fmt.Errorf("refused")

	out, _ := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if len(api.routes) != 0 {
		t.Errorf("a route was created with no workload: %+v", api.routes)
	}
	var found bool
	for _, f := range out.Failures {
		found = found || strings.Contains(f, "web.example.com")
	}
	if !found {
		t.Errorf("the route should be reported: %v", out.Failures)
	}
}

// A migration that creates most of an application is worse than one that will
// not start, so what is not carried yet is named before anything runs.
func TestUnsupportedNamesWhatWouldBeDropped(t *testing.T) {
	plan, _ := smallPlan(t)
	if got := Unsupported(plan); len(got) != 0 {
		t.Errorf("a plain app and database should be supported, got %v", got)
	}

	withCompose := Plan{Items: []Item{
		{Kind: "compose", Name: "n8n", Verdict: Moves},
		{Kind: "registry", Name: "ghcr", Verdict: Moves},
		{Kind: "application", Name: "web", Verdict: Moves, Details: map[string]string{"bind_mounts": "1"}},
		{Kind: "application", Name: "gone", Verdict: NotMoved},
	}}
	got := Unsupported(withCompose)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"n8n", "ghcr", "web"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is not named: %v", want, got)
		}
	}
	if strings.Contains(joined, "gone") {
		t.Error("something that is not moving should not be listed")
	}
}

func TestPrepareNeedsAnAPI(t *testing.T) {
	plan, src := smallPlan(t)
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src}); err == nil {
		t.Error("expected an error")
	}
}

// A workload that is not running has no image on Docker to read, so what
// Dokploy stored is what a start would use.
func TestTheImageFallsBackToWhatDokployStored(t *testing.T) {
	plan, src := smallPlan(t)
	src.Docker.Services = nil
	for _, r := range src.Rows["postgres"] {
		r["dockerImage"] = "postgres:16"
	}
	api := newFakeAPI()
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)}); err != nil {
		t.Fatal(err)
	}
	db, ok := api.service("db")
	if !ok || db.Image != "postgres:16" {
		t.Errorf("database image = %q", db.Image)
	}
}

// A workload's volumes are created empty in stage 1 so the move has somewhere
// to put the data, and its config files come with it.
func TestPrepareCreatesVolumesAndConfigFiles(t *testing.T) {
	plan, src := smallPlan(t)
	src.Rows["mount"] = []Row{
		{"mountId": "m1", "applicationId": "a1", "type": "volume", "volumeName": "web-uploads", "mountPath": "/app/uploads"},
		{"mountId": "m2", "applicationId": "a1", "type": "file", "mountPath": "/etc/nginx/nginx.conf", "content": "server { }"},
	}
	src.Docker.Volumes = []migrate.Volume{{Name: "web-uploads", MB: 3000}}
	plan = BuildPlan(src, time.Now())

	api := newFakeAPI()
	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failures) != 0 {
		t.Fatalf("failures: %v", out.Failures)
	}

	if _, ok := api.volumes["web-uploads"]; !ok {
		t.Fatalf("volumes = %v", api.volumes)
	}
	// Sized with headroom: a volume restored into one exactly its own size is
	// full the moment it arrives.
	if got := api.volumeSize["web-uploads"]; got < 6 {
		t.Errorf("size = %d GB for 3 GB of data, want room to grow", got)
	}
	if len(api.mounts) != 1 || !strings.HasSuffix(api.mounts[0], ":/app/uploads") {
		t.Errorf("mounts = %v", api.mounts)
	}
	if len(api.files) != 1 || !strings.Contains(api.files[0], "/etc/nginx/nginx.conf") || !strings.Contains(api.files[0], "server { }") {
		t.Errorf("config files = %v", api.files)
	}
}

// A bind mount is a host path somebody chose to keep or copy, and copying is
// stage 2's data step. A plan carrying one is refused rather than half-applied.
func TestABindMountIsRefusedUntilItCanBeCopied(t *testing.T) {
	src := Source{Rows: map[string][]Row{
		"project":     {{"projectId": "p1", "name": "Acme"}},
		"environment": {{"environmentId": "e1", "projectId": "p1", "name": "production"}},
		"application": {{"applicationId": "a1", "name": "web", "environmentId": "e1", "appName": "web-abc"}},
		"mount":       {{"mountId": "m1", "applicationId": "a1", "type": "bind", "hostPath": "/srv/web-data", "mountPath": "/data"}},
	}}
	plan := BuildPlan(src, time.Now())

	missing := Unsupported(plan)
	if len(missing) == 0 || !strings.Contains(strings.Join(missing, " "), "bind mount") {
		t.Fatalf("Unsupported = %v", missing)
	}
}

func TestVolumeSizeAlwaysLeavesRoom(t *testing.T) {
	for mb, want := range map[int]int{0: 1, -1: 1, 10: 1, 600: 2, 3000: 6, 8000: 16} {
		if got := volumeSizeGB(mb); got != want {
			t.Errorf("%d MB -> %d GB, want %d", mb, got, want)
		}
	}
}

// Leaving an unanswered item out quietly is how an operator ends up with a
// server missing one application.
func TestAnUnansweredItemStopsPrepare(t *testing.T) {
	plan := Plan{Items: []Item{
		{Kind: "application", Name: "web", Verdict: NeedsYou,
			Decisions: []Decision{{ID: "mount", Question: "copy /srv/data?"}}},
	}}
	missing := Unsupported(plan)
	if len(missing) != 1 || !strings.Contains(missing[0], "question to answer") {
		t.Fatalf("Unsupported = %v", missing)
	}
}
