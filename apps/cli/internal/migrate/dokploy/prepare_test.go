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
	tcpRoutes  []TCPRouteSpec
	registries []RegistrySpec
	storages   []StorageSpec
	gits       []GitSpec
	backups    []BackupSpec
	stacks     []StackSpec
	volumes    map[string]string // name -> id
	volumeSize map[string]int
	mounts     []string
	files      []string
	fail       map[string]error  // target name -> error
	hostnames  map[string]string // service id -> its name in the cluster
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
	id := f.next("svc")
	if f.hostnames == nil {
		f.hostnames = map[string]string{}
	}
	// What Meshploy calls it in the cluster: its own name, without the random
	// suffix the platform being migrated gave it.
	f.hostnames[id] = strings.ToLower(spec.Name)
	return id, nil
}

// ClusterHostname is what another workload's environment has to reach this one
// by, once it lives here.
func (f *fakeAPI) ClusterHostname(projectID, serviceID string) (string, error) {
	return f.hostnames[serviceID], nil
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

func (f *fakeAPI) CreateRegistry(spec RegistrySpec) (string, error) {
	f.registries = append(f.registries, spec)
	return f.next("reg"), nil
}

func (f *fakeAPI) CreateStorage(spec StorageSpec) (string, error) {
	f.storages = append(f.storages, spec)
	return f.next("store"), nil
}

func (f *fakeAPI) CreateGit(spec GitSpec) (string, error) {
	f.gits = append(f.gits, spec)
	return f.next("git"), nil
}

func (f *fakeAPI) CreateStack(projectID string, spec StackSpec) (string, error) {
	f.stacks = append(f.stacks, spec)
	return f.next("stack"), nil
}

func (f *fakeAPI) CreateBackup(projectID string, spec BackupSpec) (string, error) {
	f.backups = append(f.backups, spec)
	return f.next("backup"), nil
}

func (f *fakeAPI) CreateTCPRoute(projectID string, spec TCPRouteSpec) (string, error) {
	f.tcpRoutes = append(f.tcpRoutes, spec)
	return f.next("tcp"), nil
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

	// A compose app, a registry and a host path are all carried now. A question
	// nobody has answered is not.
	withBinds := Plan{Items: []Item{
		{Kind: "compose", Name: "n8n", Verdict: Moves},
		{Kind: "registry", Name: "ghcr", Verdict: Moves},
		{Kind: "application", Name: "asked", Verdict: NeedsYou,
			Decisions: []Decision{{ID: "cert", Question: "its certificate was uploaded by hand"}}},
		{Kind: "application", Name: "gone", Verdict: NotMoved},
	}}
	got := Unsupported(withBinds)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"asked"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is not named: %v", want, got)
		}
	}
	for _, gone := range []string{"n8n", "ghcr"} {
		if strings.Contains(joined, gone) {
			t.Errorf("%s is created now and should not stop a plan: %v", gone, got)
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
// A host path an application mounts becomes a volume with the path's contents
// in it, at the same place inside the container. Whether it comes at all is the
// operator's decision, because a path outside the platform's own directory may
// be shared with the machine - and "copy" is what the plan suggests.
func TestABindMountBecomesAVolumeWithItsContents(t *testing.T) {
	src := Source{Rows: map[string][]Row{
		"project":     {{"projectId": "p1", "name": "Acme"}},
		"environment": {{"environmentId": "e1", "projectId": "p1", "name": "production"}},
		"application": {{"applicationId": "a1", "name": "web", "environmentId": "e1", "appName": "web-abc"}},
		"mount": {
			{"mountId": "m1", "applicationId": "a1", "type": "bind", "hostPath": "/srv/web-data", "mountPath": "/data"},
			{"mountId": "m2", "applicationId": "a1", "type": "bind", "hostPath": "/srv/shared", "mountPath": "/shared"},
		},
	}}
	src.PathMB = map[string]int{"/srv/web-data": 900}
	plan := BuildPlan(src, time.Now())

	// It no longer stops a plan: the question has a default, and the default
	// is to bring it.
	if missing := Unsupported(plan); len(missing) != 0 {
		t.Fatalf("Unsupported = %v", missing)
	}

	api := newFakeAPI()
	_, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t),
		// One path comes, one the operator chose to leave behind.
		Answers: map[string]map[string]string{"a1": {"mount:/srv/shared": "skip"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.volumes) != 1 {
		t.Fatalf("volumes = %v, want only the path that was chosen", api.volumes)
	}
	name := "web-srv-web-data"
	if _, ok := api.volumes[name]; !ok {
		t.Errorf("the volume should be named after the path: %v", api.volumes)
	}
	// Sized from what the path holds, with the same headroom a volume gets.
	if got := api.volumeSize[name]; got != 2 {
		t.Errorf("size = %d GB for 900 MB, want room to grow", got)
	}
	if len(api.mounts) != 1 || !strings.HasSuffix(api.mounts[0], ":/data") {
		t.Errorf("it should be mounted where the application expects it: %v", api.mounts)
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
	if len(missing) != 1 || !strings.Contains(missing[0], "copy /srv/data?") {
		t.Fatalf("Unsupported = %v", missing)
	}

	// The same question with a default does not: it has an answer already, and
	// the plan says what it is.
	answered := Plan{Items: []Item{
		{Kind: "application", Name: "web", Verdict: NeedsYou,
			Decisions: []Decision{{ID: "mount", Question: "copy /srv/data?", Default: "copy"}}},
	}}
	if got := Unsupported(answered); len(got) != 0 {
		t.Errorf("Unsupported = %v", got)
	}
}

// A database is created on the engine Meshploy names and the tag Dokploy was
// running. The plan shows "Postgres" to a reader; the API takes "postgres",
// and sending the label built an image with no tag at all - a database that
// can never pull, let alone hold the data about to be copied into it.
func TestADatabaseKeepsItsEngineAndExactTag(t *testing.T) {
	for _, tc := range []struct{ image, engine, version string }{
		{"postgres:16", "postgres", "16"},
		{"mysql:8.0.35", "mysql", "8.0.35"},
		{"registry.example:5000/mongo", "mongodb", ""},
	} {
		label := map[string]string{"postgres": "Postgres", "mysql": "MySQL", "mongodb": "MongoDB"}[tc.engine]
		it := Item{Kind: "database", Name: "db", Details: map[string]string{"engine": label, "image": tc.image}}
		engine, version := engineAndVersion(it)
		if engine != tc.engine || version != tc.version {
			t.Errorf("%s → %q %q, want %q %q", tc.image, engine, version, tc.engine, tc.version)
		}
	}
}

// A migrated application reaches its database at the name it answers to here.
//
// Dokploy's hostname for a database - its appName, with a random suffix - means
// nothing in the cluster, so an application carried across with its connection
// string untouched comes up pointing at nothing. Carried across and rewritten,
// it points at the database that moved with it.
func TestAMigratedApplicationPointsAtTheMigratedDatabase(t *testing.T) {
	plan, src := smallPlan(t)
	// What the application runs with, which is where the environment is read
	// from: it names the database by Dokploy's hostname.
	src.Docker.Services[0].Env = []string{"PORT=3000", "DATABASE_URL=postgres://app:pw@db-xyz:5432/app"}

	api := newFakeAPI()
	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if err != nil {
		t.Fatal(err)
	}
	env := api.envs[out.Services["a1"]]
	if strings.Contains(env, "db-xyz") {
		t.Errorf("the old hostname survived: %q", env)
	}
	if !strings.Contains(env, "postgres://app:pw@db:5432/app") {
		t.Errorf("env = %q, want the database's name here", env)
	}
}

// Dokploy publishes a database by giving it a host port; here that is a TCP
// route on the gateway, created closed and keeping the same port number - a
// connection string in somebody's notes should not change because the platform
// did.
func TestAPublishedDatabasePortBecomesATCPRoute(t *testing.T) {
	plan, src := smallPlan(t)
	for i := range src.Rows["postgres"] {
		src.Rows["postgres"][i]["externalPort"] = "5433"
	}
	plan = BuildPlan(src, time.Now())

	api := newFakeAPI()
	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.tcpRoutes) != 1 {
		t.Fatalf("tcp routes = %+v", api.tcpRoutes)
	}
	r := api.tcpRoutes[0]
	if r.GatewayPort != 5433 {
		t.Errorf("gateway port = %d, want the one Dokploy published", r.GatewayPort)
	}
	if r.ServiceID != out.Services["d1"] {
		t.Errorf("it should point at the database that moved, got %q", r.ServiceID)
	}
	if r.ServicePort != 5432 {
		t.Errorf("service port = %d, want the engine's own", r.ServicePort)
	}
}

// A database nobody published gets no port on the gateway.
func TestADatabaseWithNoPublishedPortGetsNoTCPRoute(t *testing.T) {
	plan, src := smallPlan(t)
	api := newFakeAPI()
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)}); err != nil {
		t.Fatal(err)
	}
	if len(api.tcpRoutes) != 0 {
		t.Errorf("tcp routes = %+v", api.tcpRoutes)
	}
}

// What a platform is connected to comes across with the credentials that are
// the operator's: a registry's login, an object store's keys. What is bound to
// the server being replaced does not, and is reported rather than refused.
func TestIntegrationsComeAcrossWithTheCredentialsThatTravel(t *testing.T) {
	plan, src := smallPlan(t)
	src.Rows["registry"] = []Row{{
		"registryId": "r1", "registryName": "ghcr", "registryUrl": "ghcr.io",
		"username": "acme", "password": "ghp_secret", "imagePrefix": "acme",
	}}
	src.Rows["destination"] = []Row{{
		"destinationId": "s1", "name": "backups", "provider": "s3", "bucket": "acme-backups",
		"endpoint": "https://s3.example", "region": "eu-west-1",
		"accessKey": "AKIA", "secretAccessKey": "shh",
	}}
	src.Rows["git_provider"] = []Row{
		{"gitProviderId": "gp1", "name": "acme on Bitbucket", "providerType": "bitbucket"},
		{"gitProviderId": "gp2", "name": "acme on GitHub", "providerType": "github"},
	}
	src.Rows["bitbucket"] = []Row{{
		"bitbucketId": "b1", "gitProviderId": "gp1", "bitbucketUsername": "acme-bot",
		"apiToken": "atl-token", "bitbucketWorkspaceName": "acme",
	}}
	src.Rows["github"] = []Row{{"githubId": "gh1", "gitProviderId": "gp2", "githubAppId": "12345"}}
	plan = BuildPlan(src, time.Now())

	api := newFakeAPI()
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)}); err != nil {
		t.Fatal(err)
	}

	if len(api.registries) != 1 || api.registries[0].Password != "ghp_secret" || api.registries[0].Endpoint != "ghcr.io" {
		t.Fatalf("registries = %+v", api.registries)
	}
	if len(api.storages) != 1 || api.storages[0].SecretKey != "shh" || api.storages[0].Bucket != "acme-backups" {
		t.Fatalf("storages = %+v", api.storages)
	}
	// Bitbucket's token belongs to the account, so it travels. The GitHub App
	// is registered against this server's callback and does not.
	if len(api.gits) != 1 {
		t.Fatalf("git integrations = %+v", api.gits)
	}
	if api.gits[0].Provider != "bitbucket" || !strings.Contains(api.gits[0].Token, "atl-token") {
		t.Errorf("git = %+v", api.gits[0])
	}
	if api.gits[0].Groups != "acme" {
		t.Errorf("the workspace scopes what it can list: %+v", api.gits[0])
	}
}

// A schedule is recreated against the store it wrote to, and one the operator
// had turned off stays off: starting to write to somebody's bucket unasked is
// not a migration, it is a surprise.
func TestBackupSchedulesAreRecreatedAgainstTheirStore(t *testing.T) {
	plan, src := smallPlan(t)
	src.Rows["destination"] = []Row{{
		"destinationId": "s1", "name": "backups", "provider": "s3", "bucket": "b",
		"accessKey": "AKIA", "secretAccessKey": "shh",
	}}
	src.Rows["backup"] = []Row{{
		"backupId": "bk1", "postgresId": "d1", "destinationId": "s1",
		"schedule": "0 2 * * *", "keepLatestCount": "7", "prefix": "db/", "enabled": "true",
	}}
	plan = BuildPlan(src, time.Now())

	api := newFakeAPI()
	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.backups) != 1 {
		t.Fatalf("backups = %+v (failures %v)", api.backups, out.Failures)
	}
	b := api.backups[0]
	if b.ServiceID != out.Services["d1"] {
		t.Errorf("it should back up the database that moved: %+v", b)
	}
	if b.Schedule != "0 2 * * *" || b.Prefix != "db" {
		t.Errorf("schedule = %+v", b)
	}
	// Seven daily copies is a week; the same count on an hourly schedule is not.
	if b.Retention != 7 {
		t.Errorf("retention = %d days, want 7", b.Retention)
	}
	if got := retentionDays("0 * * * *", 48); got != 2 {
		t.Errorf("hourly, keeping 48: %d days, want 2", got)
	}
}

// A compose app becomes a stack: the file its author pasted comes across as the
// stack's own spec, and one that reads its file from git goes on reading it
// from git.
func TestComposeAppsBecomeStacks(t *testing.T) {
	plan, src := smallPlan(t)
	src.Rows["compose"] = []Row{
		{"composeId": "c1", "name": "analytics", "appName": "analytics-abc", "environmentId": "e1",
			"sourceType": "raw", "composeType": "docker-compose",
			"composeFile": "services:\n  web:\n    image: plausible\n", "env": "PORT=8000"},
		{"composeId": "c2", "name": "wiki", "appName": "wiki-def", "environmentId": "e1",
			"sourceType": "github", "repository": "wiki", "owner": "acme", "branch": "main",
			"composePath": "./docker-compose.yml"},
	}
	plan = BuildPlan(src, time.Now())

	api := newFakeAPI()
	out, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: api, Journal: newJournal(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.stacks) != 2 {
		t.Fatalf("stacks = %+v (failures %v)", api.stacks, out.Failures)
	}
	byName := map[string]StackSpec{}
	for _, s := range api.stacks {
		byName[s.Name] = s
	}
	if got := byName["analytics"]; !strings.Contains(got.Spec, "image: plausible") || got.Repo != "" {
		t.Errorf("a pasted compose file should come across as the spec: %+v", got)
	}
	if got := byName["analytics"]; got.Variables["PORT"] != "8000" {
		t.Errorf("its environment interpolates into the file: %+v", got.Variables)
	}
	if got := byName["wiki"]; got.Spec != "" || got.Repo == "" || got.Path != "./docker-compose.yml" {
		t.Errorf("a git-sourced app should keep reading from git: %+v", got)
	}
	// And the stack is what the move starts.
	if out.Services["c1"] == "" {
		t.Error("the stack should be recorded as what was created for the compose app")
	}
}
