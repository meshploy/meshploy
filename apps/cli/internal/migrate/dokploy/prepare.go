package dokploy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Stage 1: build the Meshploy side, with nothing serving yet.
//
// Everything is created in dependency order and left inert: services stopped,
// routes paused. Dokploy keeps serving throughout, so this stage has no
// downtime and can be run, interrupted and run again - the journal decides what
// has already been done.
//
// What this stage does *not* do is as important: it never stops a Dokploy
// workload, never touches its data, and never takes a domain. Those are stage 2,
// per group, when the operator says so.

// API is the part of Meshploy that prepare writes to.
//
// An interface rather than the typed client, for two reasons: the engine is
// then testable without a server, and the surface a migration is allowed to
// touch is written down in one place - it can create, and it cannot delete.
type API interface {
	// CreateProject returns the new project's id. Called with a name that may
	// already exist, so the implementation reuses one it finds.
	CreateProject(name string) (string, error)
	// CreateService creates a service or database, stopped.
	CreateService(projectID string, spec ServiceSpec) (string, error)
	// ClusterHostname is the name a created workload answers to inside the
	// cluster, which is what another workload's environment reaches it by.
	ClusterHostname(projectID, serviceID string) (string, error)
	// SetEnvVars replaces a service's environment block.
	SetEnvVars(projectID, serviceID, env string) error
	// CreateRoute creates a route, paused.
	CreateRoute(projectID string, spec RouteSpec) (string, error)
	// CreateTCPRoute publishes a database's port on the gateway, paused: the
	// Meshploy equivalent of the host port Dokploy gave it.
	CreateTCPRoute(projectID string, spec TCPRouteSpec) (string, error)
	// CreateRegistry, CreateStorage and CreateGit carry the platform's
	// connections to the outside world, with the credentials that belong to
	// the operator rather than to the server.
	CreateRegistry(spec RegistrySpec) (string, error)
	CreateStorage(spec StorageSpec) (string, error)
	CreateGit(spec GitSpec) (string, error)
	// CreateBackup recreates a database's schedule, writing where it wrote.
	CreateBackup(projectID string, spec BackupSpec) (string, error)
	// CreateStack creates a compose app, not applied: like every workload here
	// it arrives stopped, and its group's move is what starts it.
	CreateStack(projectID string, spec StackSpec) (string, error)
	// CreateVolume makes a volume of the given size and returns its id.
	CreateVolume(projectID, name string, storageGB int) (string, error)
	// AttachVolume mounts a volume into a service at a path.
	AttachVolume(projectID, volumeID, serviceID, mountPath string) error
	// CreateConfigFile stores a file and attaches it to a service at a path.
	CreateConfigFile(projectID, serviceID, name, path, content string) error
}

// ServiceSpec is one workload to create.
type ServiceSpec struct {
	Name string
	// Image is what runs now in Dokploy, not the latest build: after a rollback
	// in Dokploy those differ, and what is running is the truth.
	Image string
	// Type is "application" or "database"; Engine and the DB fields are set
	// only for a database.
	Type     string
	Engine   string
	Version  string
	DBName   string
	DBUser   string
	Password string
	// GitRepo and Branch keep the source, so the next deploy builds as usual
	// and the migration never depends on a build succeeding.
	GitRepo string
	Branch  string
	// EnvVars is the environment block, read from Dokploy at apply time and
	// never written to plan.json.
	EnvVars string
	// StorageGB sizes a database's claim, from what its data measures now.
	StorageGB int
	// Ports are the container ports the workload listens on, taken from the
	// domains Dokploy routes to it. Without them a service is created on
	// Meshploy's default port 3000 and its route is published to a port
	// nothing answers on.
	Ports []int
}

// StackSpec is one compose app to create.
type StackSpec struct {
	Name string
	// Spec is the compose file itself, for an app whose author wrote it here.
	Spec string
	// Repo, Branch and Path are the other kind: the file lives in git, and the
	// stack reads it from there as Dokploy did.
	Repo   string
	Branch string
	Path   string
	// Variables are the app's environment, which a compose file interpolates
	// as ${NAME}.
	Variables map[string]string
}

// TCPRouteSpec is one published database port to carry across.
type TCPRouteSpec struct {
	// GatewayPort is the port Dokploy published, kept so a connection string
	// somebody has written down still works.
	GatewayPort int
	ServiceID   string
	// ServicePort is the database's own port, which the gateway forwards to.
	ServicePort int
}

// RouteSpec is one hostname to create, paused.
type RouteSpec struct {
	Hostname  string
	ServiceID string
	Port      int
	// Path and StripPath carry Dokploy's per-path routing, where it used it.
	Path      string
	StripPath bool
}

// PrepareDeps is what stage 1 needs.
type PrepareDeps struct {
	Plan    Plan
	Source  Source
	API     API
	Journal *journal.Journal
	// Answers are what the operator chose, by item id and decision id. A
	// decision nobody answered keeps its default, which is how a plan
	// confirmed without touching anything still does the obvious thing.
	Answers map[string]map[string]string
	// Images carries locally built images into the built-in registry. Nil skips
	// that, which is right for a server whose images all came from registries.
	Images *ImageMover
}

// PrepareResult is what stage 1 did.
type PrepareResult struct {
	Created int
	Skipped int
	// Projects and Services map a Dokploy id to the Meshploy id it became, so
	// stage 2 knows what to start.
	Projects map[string]string
	Services map[string]string
	Failures []string
}

// Unsupported lists what this stage would silently drop, so it can refuse
// before it starts.
//
// A migration that creates most of an application is worse than one that will
// not start: the operator believes the half it can see. Each entry names the
// item and what is not carried yet.
func Unsupported(plan Plan) []string {
	var out []string
	for _, it := range plan.Items {
		if it.Verdict != Moves && it.Verdict != NeedsYou {
			continue
		}
		// A bind mount is a host path. Whether it moves is the operator's
		// decision, and copying it is stage 2's data step, which does not exist
		// yet - so a plan that carries one is refused rather than half-applied.
		// Prepare creates what the plan says moves. An item still carrying an
		// unanswered question is not that, and leaving it out quietly is how
		// an operator ends up with a server missing one application.
		// A question with a default is one the operator may answer; one
		// without is one they must. Only the second stops a plan - the same
		// rule a group uses to decide whether it can move, so an item and its
		// group never disagree about whether it is ready.
		for _, d := range it.Decisions {
			if d.Default == "" {
				out = append(out, fmt.Sprintf("%s (%s): %s", it.Name, it.Kind, d.Question))
			}
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

// Prepare runs stage 1.
//
// Every step is journalled under a stable id, so a run that fails partway
// continues where it stopped rather than creating a second copy of everything.
// A failure does not stop the stage: the rest is created and the failures are
// returned together, because the operator wants one list, not one at a time.
func Prepare(d PrepareDeps) (PrepareResult, error) {
	out := PrepareResult{Projects: map[string]string{}, Services: map[string]string{}}
	if d.API == nil {
		return out, fmt.Errorf("prepare needs an API to create into")
	}

	byID := map[string]Item{}
	for _, it := range d.Plan.Items {
		byID[it.ID] = it
	}

	// 1. The connections everything else refers to: a service names a registry
	// to pull from, a schedule names an object store to write to. A failure
	// here is collected like any other - a registry that could not be created
	// leaves the workloads that used it pulling by hand, which is something to
	// fix rather than a reason to stop.
	storages := map[string]string{}
	for _, it := range d.Plan.Items {
		switch it.Kind {
		case "registry":
			for _, r := range d.Source.Rows["registry"] {
				if r.Str("registryId") != it.ID {
					continue
				}
				spec := registrySpec(r)
				_, _ = d.step(&out, "prepare/registry/"+it.ID, "", "create-registry", it.Name, func() (string, error) {
					return d.API.CreateRegistry(spec)
				})
			}
		case "destination":
			for _, r := range d.Source.Rows["destination"] {
				if r.Str("destinationId") != it.ID {
					continue
				}
				spec := storageSpec(r)
				id, err := d.step(&out, "prepare/storage/"+it.ID, "", "create-storage", it.Name, func() (string, error) {
					return d.API.CreateStorage(spec)
				})
				if err == nil && id != "" {
					storages[it.ID] = id
				}
			}
		case "git_provider":
			spec, ok := gitSpec(d.Source, it.ID, it.Name)
			if !ok {
				continue // its credentials belong to the server being replaced
			}
			_, _ = d.step(&out, "prepare/git/"+it.ID, "", "create-git-integration", it.Name, func() (string, error) {
				return d.API.CreateGit(spec)
			})
		}
	}

	// 2. Projects, so everything below has somewhere to go.
	for _, it := range d.Plan.Items {
		if it.Kind != "project" || it.Verdict != Moves {
			continue
		}
		id, err := d.step(&out, "prepare/project/"+it.ID, "", "create-project", it.Name, func() (string, error) {
			return d.API.CreateProject(it.Name)
		})
		if err == nil {
			out.Projects[it.ID] = id
		}
	}

	// 3. Compose apps, as stacks. Created and not applied: a stack that is
	// applied is running, and nothing this stage creates runs.
	for _, it := range d.Plan.Items {
		if it.Kind != "compose" || !d.ready(it) {
			continue
		}
		projectID := d.projectFor(out, it)
		if projectID == "" {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: no project was created for %q", it.Name, it.Project))
			continue
		}
		spec, ok := d.stackSpec(it)
		if !ok {
			out.Failures = append(out.Failures, fmt.Sprintf(
				"%s: its compose file is not on this server and its git source is not readable from here", it.Name))
			continue
		}
		id, err := d.step(&out, "prepare/stack/"+it.ID, "", "create-stack", it.Name, func() (string, error) {
			return d.API.CreateStack(projectID, spec)
		})
		if err == nil {
			out.Services[it.ID] = id
		}
	}

	// 4. Applications and databases, created stopped.
	var pending []pendingEnv
	hostnames := map[string]string{}
	for _, it := range d.Plan.Items {
		if it.Kind != "application" && it.Kind != "database" {
			continue
		}
		if !d.ready(it) {
			continue
		}
		projectID := d.projectFor(out, it)
		if projectID == "" {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: no project was created for %q", it.Name, it.Project))
			continue
		}
		spec := d.specFor(it)
		// An image Dokploy built on this host exists nowhere else, so it is
		// pushed into the built-in registry before anything is asked to pull
		// it. An image from a registry is left as it is.
		if d.Images != nil && spec.Image != "" {
			pushed, err := d.Images.Push("prepare/image/"+it.ID, "", spec.Image)
			if err != nil {
				out.Failures = append(out.Failures, fmt.Sprintf("push %s: %v", it.Name, err))
				continue
			}
			spec.Image = pushed
		}
		id, err := d.step(&out, "prepare/service/"+it.ID, "", "create-service", it.Name, func() (string, error) {
			return d.API.CreateService(projectID, spec)
		})
		if err != nil {
			continue
		}
		out.Services[it.ID] = id
		d.mounts(&out, it, projectID, id)
		// The environment is set in a second pass, once every workload of this
		// migration exists: what an application reaches its database by has to
		// be rewritten, and the new name is only known after the database has
		// been created.
		pending = append(pending, pendingEnv{itemID: it.ID, name: it.Name, projectID: projectID, serviceID: id, env: spec.EnvVars})
		if host, err := d.API.ClusterHostname(projectID, id); err == nil && host != "" {
			if from := d.appNameOf(it.ID); from != "" && from != host {
				hostnames[from] = host
			}
		}
	}

	for _, p := range pending {
		if p.env == "" {
			continue
		}
		env := rewriteHostnames(p.env, hostnames)
		_, _ = d.step(&out, "prepare/env/"+p.itemID, "", "set-env", p.name, func() (string, error) {
			return p.serviceID, d.API.SetEnvVars(p.projectID, p.serviceID, env)
		})
	}

	// 5. Published database ports. Dokploy gives a database a host port; here
	// that is a TCP route on the gateway, created closed like every other
	// route, and keeping the same port number - a connection string in
	// somebody's notes should not change because the platform did.
	for _, it := range d.Plan.Items {
		if it.Kind != "database" || !d.ready(it) || it.Details["external_port"] == "" {
			continue
		}
		serviceID := out.Services[it.ID]
		if serviceID == "" {
			continue // its database was not created; the failure is already recorded
		}
		port := atoiOr(it.Details["external_port"], 0)
		if port <= 0 {
			continue
		}
		projectID := d.projectFor(out, it)
		spec := TCPRouteSpec{GatewayPort: port, ServiceID: serviceID, ServicePort: enginePort(it.Details["engine"])}
		_, _ = d.step(&out, "prepare/tcp/"+it.ID, "", "create-tcp-route",
			fmt.Sprintf("%s on :%d", it.Name, port), func() (string, error) {
				return d.API.CreateTCPRoute(projectID, spec)
			})
	}

	// 6. Routes, paused: they exist, serve nothing, and get no certificate
	// until their group moves.
	for _, it := range d.Plan.Items {
		if it.Kind != "domain" || !d.ready(it) {
			continue
		}
		serviceID := out.Services[it.Details["application_id"]]
		if serviceID == "" {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: the workload it points at was not created", it.Name))
			continue
		}
		projectID := d.projectFor(out, byID[it.Details["application_id"]])
		spec := RouteSpec{
			Hostname:  splitHostPath(it.Name),
			ServiceID: serviceID,
			Port:      atoiOr(it.Details["port"], 0),
			Path:      it.Details["path"],
			StripPath: it.Details["strip_path"] == "true",
		}
		_, _ = d.step(&out, "prepare/route/"+it.ID, "", "create-route", spec.Hostname, func() (string, error) {
			return d.API.CreateRoute(projectID, spec)
		})
	}

	// 7. Backup schedules, which need both the database and the store they
	// write to, so they come after everything else.
	for _, it := range d.Plan.Items {
		if it.Kind != "database" || !d.ready(it) {
			continue
		}
		serviceID, projectID := out.Services[it.ID], d.projectFor(out, it)
		if serviceID == "" || projectID == "" {
			continue
		}
		for _, r := range backupSpecs(d.Source, it.ID) {
			storageID := storages[r.Str("destinationId")]
			if storageID == "" {
				out.Failures = append(out.Failures, fmt.Sprintf(
					"%s: its backup schedule writes to a destination that was not created", it.Name))
				continue
			}
			spec := BackupSpec{
				ServiceID: serviceID, StorageID: storageID,
				Schedule:  r.Str("schedule"),
				Retention: retentionDays(r.Str("schedule"), atoiOr(r.Str("keepLatestCount"), 0)),
				Prefix:    backupPrefix(r, it.Name),
				Enabled:   r.Str("enabled") != "false",
			}
			_, _ = d.step(&out, "prepare/backup/"+r.Str("backupId"), "", "create-backup", it.Name, func() (string, error) {
				return d.API.CreateBackup(projectID, spec)
			})
		}
	}

	return out, nil
}

// stackSpec reads a compose app from Dokploy at apply time.
//
// Two shapes, and they are not interchangeable. An app whose author pasted a
// compose file has that file in Dokploy's database, and it comes across as the
// stack's own spec. An app that reads its file from git keeps doing that: the
// repository and branch move with it, so the next deploy reads what the
// repository says, which is what its author expects.
func (d PrepareDeps) stackSpec(it Item) (StackSpec, bool) {
	for _, r := range d.Source.Rows["compose"] {
		if r.Str("composeId") != it.ID {
			continue
		}
		spec := StackSpec{Name: it.Name, Variables: envMap(d.runningEnv(it), r.Str("env"))}
		if file := r.Str("composeFile"); r.Str("sourceType") == "raw" && strings.TrimSpace(file) != "" {
			spec.Spec = file
			return spec, true
		}
		repo, branch := gitSource(r)
		if repo == "" {
			return spec, false
		}
		spec.Repo, spec.Branch, spec.Path = repo, branch, r.Str("composePath")
		return spec, true
	}
	return StackSpec{}, false
}

// envMap turns an environment block into the variables a compose file
// interpolates. What the workload runs with is preferred, for the same reason
// everywhere else: the stored block is encrypted on current Dokploy, and a
// shared variable is resolved when the workload is deployed.
func envMap(running, stored string) map[string]string {
	block := running
	if strings.TrimSpace(block) == "" {
		block = stored
	}
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			continue
		}
		out[strings.TrimSpace(key)] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// enginePort is the port an engine listens on, which is what the gateway
// forwards a published port to. Zero lets the API use the database's own.
func enginePort(engine string) int {
	switch engine {
	case "Postgres":
		return 5432
	case "MySQL", "MariaDB":
		return 3306
	case "MongoDB":
		return 27017
	case "Redis":
		return 6379
	}
	return 0
}

// ready reports whether an item can be created: it was not ruled out, and
// every question it carries has an answer - the operator's, or the default the
// plan offered. A question with no default is the only kind that stops it,
// which is the same rule a group uses to decide whether it can move.
func (d PrepareDeps) ready(it Item) bool {
	if it.Verdict == NotMoved {
		return false
	}
	for _, dec := range it.Decisions {
		if dec.Default == "" && d.Answers[it.ID][dec.ID] == "" {
			return false
		}
	}
	return true
}

// answer is what the operator chose for one decision, or its default.
func (d PrepareDeps) answer(itemID, decisionID string) string {
	if got := d.Answers[itemID][decisionID]; got != "" {
		return got
	}
	for _, it := range d.Plan.Items {
		if it.ID != itemID {
			continue
		}
		for _, dec := range it.Decisions {
			if dec.ID == decisionID {
				return dec.Default
			}
		}
	}
	return ""
}

// bindVolumeName names the volume a host path's contents move into. The path
// is what the operator recognises, so it is what the name is built from.
func bindVolumeName(workload, host string) string {
	base := strings.Trim(strings.ReplaceAll(strings.Trim(host, "/"), "/", "-"), "-")
	if base == "" {
		base = "data"
	}
	name := strings.ToLower(workload + "-" + base)
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}

// pendingEnv is one workload's environment, waiting for every name in it to be
// known.
type pendingEnv struct {
	itemID, name, projectID, serviceID, env string
}

// rewriteHostnames points a migrated environment at the migrated workloads.
//
// An application reaches its database by the hostname the old platform gave
// it - "shop-db-ttikjv" on Dokploy's network - and that name means nothing in
// the cluster, so a connection string carried across verbatim resolves to
// nothing. It is replaced by the name the same workload answers to here.
//
// A plain replacement, because these names are not words that occur by
// accident: Dokploy builds every one of them as the workload's name with a
// random suffix.
func rewriteHostnames(env string, hostnames map[string]string) string {
	for from, to := range hostnames {
		env = strings.ReplaceAll(env, from, to)
	}
	return env
}

// mounts creates a workload's volumes and config files and attaches them.
//
// A volume is created empty here and filled in stage 2, when the group moves
// and its data is copied with the workload stopped. Creating it now means the
// move has somewhere to put the data and nothing to invent under downtime.
func (d PrepareDeps) mounts(out *PrepareResult, it Item, projectID, serviceID string) {
	for _, m := range d.mountRows(it.ID) {
		switch m.Str("type") {
		case "volume":
			// A managed database keeps its data in a claim of its own, sized
			// with the service. Dokploy records that same data as a mount, and
			// creating a volume for it would be a second, empty copy of the
			// storage - which Meshploy refuses to attach anyway, because a
			// database is not an application.
			if it.Kind == "database" {
				continue
			}
			name, path := m.Str("volumeName"), m.Str("mountPath")
			if name == "" || path == "" {
				continue
			}
			// Sized from what the volume holds now, with room to grow: a
			// volume restored into one exactly its own size is full on arrival.
			size := volumeSizeGB(d.Source.Docker.VolumeMB(name))
			volumeID, err := d.step(out, "prepare/volume/"+m.Str("mountId"), "", "create-volume", name, func() (string, error) {
				return d.API.CreateVolume(projectID, name, size)
			})
			if err != nil || volumeID == "" {
				continue
			}
			_, _ = d.step(out, "prepare/mount/"+m.Str("mountId"), "", "attach-volume", name+" at "+path, func() (string, error) {
				return volumeID, d.API.AttachVolume(projectID, volumeID, serviceID, path)
			})

		case "bind":
			// A host path an application mounts. Whether it comes across is the
			// operator's decision, because a path outside the platform's own
			// directory may be shared with the machine; the plan asks, and
			// "copy" is what it suggests.
			host, path := m.Str("hostPath"), m.Str("mountPath")
			if host == "" || path == "" || d.answer(it.ID, "mount:"+host) != "copy" {
				continue
			}
			name := bindVolumeName(it.Name, host)
			size := volumeSizeGB(d.Source.PathMB[host])
			volumeID, err := d.step(out, "prepare/bind/"+m.Str("mountId"), "", "create-volume",
				name+" for "+host, func() (string, error) {
					return d.API.CreateVolume(projectID, name, size)
				})
			if err != nil || volumeID == "" {
				continue
			}
			_, _ = d.step(out, "prepare/bind-mount/"+m.Str("mountId"), "", "attach-volume", name+" at "+path,
				func() (string, error) {
					return volumeID, d.API.AttachVolume(projectID, volumeID, serviceID, path)
				})

		case "file":
			// Dokploy keeps small files - an nginx.conf, an htpasswd - beside
			// the workload. They are config files here, projected the same way.
			path, content := m.Str("mountPath"), m.Str("content")
			if path == "" {
				continue
			}
			name := fileNameFor(it.Name, path)
			_, _ = d.step(out, "prepare/file/"+m.Str("mountId"), "", "create-config-file", name, func() (string, error) {
				return "", d.API.CreateConfigFile(projectID, serviceID, name, path, content)
			})
		}
	}
}

// mountRows finds a workload's mounts, whichever column the table uses to own
// them.
func (d PrepareDeps) mountRows(itemID string) []Row {
	var out []Row
	for _, m := range d.Source.Rows["mount"] {
		for _, key := range []string{"applicationId", "composeId", "postgresId", "mysqlId", "mariadbId", "mongoId", "redisId"} {
			if m.Str(key) == itemID {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// volumeSizeGB turns the size a volume holds into the size to create, rounded
// up with headroom. A volume restored into one exactly its own size is full the
// moment it arrives, and growing a PVC afterwards is not always possible.
func volumeSizeGB(mb int) int {
	if mb <= 0 {
		return 1
	}
	gb := (mb*2 + 1023) / 1024
	if gb < 1 {
		return 1
	}
	return gb
}

// fileNameFor names a config file after the workload and the path it lands at,
// because a project may hold several and "config" tells nobody anything.
func fileNameFor(workload, path string) string {
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 && i+1 < len(base) {
		base = base[i+1:]
	}
	if base == "" {
		base = "config"
	}
	return workload + "-" + base
}

// step runs one piece of work, once. A step already recorded as done is
// skipped; a failure is recorded with its reason and does not stop the stage.
func (d PrepareDeps) step(out *PrepareResult, id, group, action, target string, run func() (string, error)) (string, error) {
	if d.Journal != nil && d.Journal.Done(id) {
		out.Skipped++
		// What it created the first time, so everything that belongs inside it
		// still finds it.
		return d.Journal.CreatedBy(id), nil
	}
	created, err := run()
	entry := journal.Entry{Step: id, Group: group, Action: action, Target: target, Result: journal.OK, Created: created}
	if err != nil {
		entry.Result, entry.Error = journal.Failed, err.Error()
		out.Failures = append(out.Failures, fmt.Sprintf("%s %s: %v", action, target, err))
	} else {
		out.Created++
	}
	if d.Journal != nil {
		_ = d.Journal.Append(entry)
	}
	return created, err
}

// projectFor finds the Meshploy project an item belongs in.
func (d PrepareDeps) projectFor(out PrepareResult, it Item) string {
	for _, p := range d.Plan.Items {
		if p.Kind == "project" && p.Name == it.Project {
			return out.Projects[p.ID]
		}
	}
	// One project on the plan and no name to match is the common single-project
	// server, not an error.
	if len(out.Projects) == 1 {
		for _, id := range out.Projects {
			return id
		}
	}
	return ""
}

// specFor reads what the workload needs from the plan and from Dokploy's own
// rows, which is where the environment lives. Values are read here, at apply
// time, and never written to plan.json.
func (d PrepareDeps) specFor(it Item) ServiceSpec {
	spec := ServiceSpec{
		Name:    it.Name,
		Image:   d.runningImage(it),
		GitRepo: it.Details["repository"],
		Branch:  it.Details["branch"],
		Type:    "application",
	}
	if it.Kind == "database" {
		spec.Type = "database"
		spec.Engine, spec.Version = engineAndVersion(it)
		// Room for the data that is about to be copied in, with the same
		// headroom a volume gets: a database restored into a claim exactly its
		// own size is full on arrival.
		spec.StorageGB = volumeSizeGB(atoiOr(it.Details["data_mb"], 0))
	}
	spec.Ports = d.portsFor(it)
	for _, table := range []string{"application", "postgres", "mysql", "mariadb", "mongo", "redis", "compose"} {
		for _, r := range d.Source.Rows[table] {
			if idOf(r) != it.ID {
				continue
			}
			spec.EnvVars = d.runningEnv(it)
			if spec.EnvVars == "" {
				spec.EnvVars = r.Str("env")
			}
			if spec.Type == "database" {
				spec.DBName, spec.DBUser = r.Str("databaseName"), r.Str("databaseUser")
				spec.Password = r.Str("databasePassword")
			}
		}
	}
	return spec
}

// portsFor is every container port Dokploy sends this workload's domains to.
//
// Dokploy records the port on the domain, not on the application, because that
// is where its router needs it. Meshploy records it on the service, so the
// migration moves it across: each distinct port becomes a port on the service,
// and the first one - the port the app's own hostname uses - is primary.
//
// A workload with no domain keeps no ports here and is created on the API's
// default, which is all a workload nothing routes to needs.
func (d PrepareDeps) portsFor(it Item) []int {
	var ports []int
	seen := map[int]bool{}
	for _, dom := range d.Plan.Items {
		if dom.Kind != "domain" || dom.Verdict != Moves {
			continue
		}
		if dom.Details["application_id"] != it.ID && dom.Details["compose_id"] != it.ID {
			continue
		}
		port := atoiOr(dom.Details["port"], 0)
		if port <= 0 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// runningImage is the image this workload is running right now.
//
// Not the latest successful build and not Dokploy's stored image: an app built
// from git has no stored image at all, and after a rollback in Dokploy the
// stored one and the running one differ. What is running is the truth, so it
// comes from Docker, found by the appName Dokploy gave the workload.
func (d PrepareDeps) runningImage(it Item) string {
	appName := it.Details["app_name"]
	if appName == "" {
		appName = d.appNameOf(it.ID)
	}
	if appName != "" {
		for _, s := range d.Source.Docker.Services {
			if s.Name == appName {
				return s.Image
			}
		}
		for _, c := range d.Source.Docker.Containers {
			if c.Name == appName || strings.HasPrefix(c.Name, appName+".") {
				return c.Image
			}
		}
	}
	// Nothing is running: fall back to what Dokploy stored, which is what a
	// stopped workload would be started from.
	return it.Details["image"]
}

// meshployEngine maps the name the plan shows a reader to the engine name
// Meshploy's API takes. They are not the same string, and sending the label
// created a database on an image with no tag at all.
var meshployEngine = map[string]string{
	"Postgres": "postgres",
	"MySQL":    "mysql",
	"MongoDB":  "mongodb",
	"Redis":    "redis",
	"MariaDB":  "mariadb",
}

// engineAndVersion is the engine to create and the tag to create it on.
//
// The tag comes from the image Dokploy runs, because a database has to come up
// on the version its files were written by: restoring a Postgres 16 cluster
// into 15 does not work, and neither does letting Meshploy pick its default.
func engineAndVersion(it Item) (engine, version string) {
	engine = meshployEngine[it.Details["engine"]]
	if engine == "" {
		engine = strings.ToLower(it.Details["engine"])
	}
	if v := it.Details["version"]; v != "" {
		return engine, v
	}
	return engine, imageTag(it.Details["image"])
}

// imageTag is the tag of an image reference, empty when it carries none. A
// registry's port is not a tag: "registry:5000/postgres" has none.
func imageTag(image string) string {
	at := strings.LastIndex(image, ":")
	if at < 0 || strings.Contains(image[at:], "/") {
		return ""
	}
	return image[at+1:]
}

// runningEnv is the environment this workload runs with, read from Docker.
//
// Not the env column: current Dokploy encrypts it, so copying that across
// would give a migrated application a block of ciphertext for its
// environment - and even in plaintext it is not the whole truth, because a
// shared project variable is resolved when the workload is deployed. What it
// runs with is what it needs to keep running.
func (d PrepareDeps) runningEnv(it Item) string {
	name := it.Details["app_name"]
	if name == "" {
		name = d.appNameOf(it.ID)
	}
	if name == "" {
		return ""
	}
	for _, s := range d.Source.Docker.Services {
		if s.Name == name {
			return strings.Join(s.Env, "\n")
		}
	}
	for _, c := range d.Source.Docker.Containers {
		if c.Name == name {
			return strings.Join(c.Env, "\n")
		}
	}
	return ""
}

// appNameOf is the name Dokploy gave a workload on Docker.
func (d PrepareDeps) appNameOf(id string) string {
	return appNameIn(d.Source.Rows, id)
}

// appNameIn finds the Docker name of the workload with this id.
//
// Only a row that carries one counts. A domain row holds the applicationId of
// the workload it points at, so it answers to that id as well - and being the
// first row visited, in a map whose order is random, it would answer with the
// empty string it has. That silently cost a database its group: the
// application's environment could not be found, so nothing joined the two.
func appNameIn(rows map[string][]Row, id string) string {
	for _, table := range rows {
		for _, r := range table {
			if idOf(r) == id && r.Str("appName") != "" {
				return r.Str("appName")
			}
		}
	}
	return ""
}

// idOf finds a Dokploy row's own id, whatever the table calls it.
func idOf(r Row) string {
	for _, key := range []string{"applicationId", "postgresId", "mysqlId", "mariadbId", "mongoId", "redisId", "composeId"} {
		if v := r.Str(key); v != "" {
			return v
		}
	}
	return ""
}

// splitHostPath takes the hostname out of a domain item's name, which carries
// the path after it.
func splitHostPath(name string) string {
	host, _, _ := strings.Cut(name, "/")
	return host
}

func atoiOr(s string, fallback int) int {
	if n, ok := atoi(s); ok {
		return n
	}
	return fallback
}
