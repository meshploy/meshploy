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
	// SetEnvVars replaces a service's environment block.
	SetEnvVars(projectID, serviceID, env string) error
	// CreateRoute creates a route, paused.
	CreateRoute(projectID string, spec RouteSpec) (string, error)
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
		switch it.Kind {
		case "compose":
			out = append(out, fmt.Sprintf("%s (compose app): stacks are not created yet", it.Name))
		case "backup":
			out = append(out, fmt.Sprintf("%s: backup schedules are not created yet", it.Name))
		case "registry", "destination", "git_provider":
			out = append(out, fmt.Sprintf("%s (%s): integrations are not created yet", it.Name, it.Kind))
		}
		if it.Details["mounts"] != "" {
			out = append(out, fmt.Sprintf("%s: %s - volumes and bind mounts are not created yet", it.Name, it.Details["mounts"]))
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

	// 1. Projects, so everything below has somewhere to go.
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

	// 2. Applications and databases, created stopped.
	for _, it := range d.Plan.Items {
		if it.Kind != "application" && it.Kind != "database" {
			continue
		}
		if it.Verdict != Moves {
			continue
		}
		projectID := d.projectFor(out, it)
		if projectID == "" {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: no project was created for %q", it.Name, it.Project))
			continue
		}
		spec := d.specFor(it)
		id, err := d.step(&out, "prepare/service/"+it.ID, "", "create-service", it.Name, func() (string, error) {
			return d.API.CreateService(projectID, spec)
		})
		if err != nil {
			continue
		}
		out.Services[it.ID] = id
		if spec.EnvVars != "" {
			_, _ = d.step(&out, "prepare/env/"+it.ID, "", "set-env", it.Name, func() (string, error) {
				return id, d.API.SetEnvVars(projectID, id, spec.EnvVars)
			})
		}
	}

	// 3. Routes, paused: they exist, serve nothing, and get no certificate
	// until their group moves.
	for _, it := range d.Plan.Items {
		if it.Kind != "domain" || it.Verdict != Moves {
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

	return out, nil
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
		spec.Engine = it.Details["engine"]
		spec.Version = it.Details["version"]
	}
	for _, table := range []string{"application", "postgres", "mysql", "mariadb", "mongo", "redis", "compose"} {
		for _, r := range d.Source.Rows[table] {
			if idOf(r) != it.ID {
				continue
			}
			spec.EnvVars = r.Str("env")
			if spec.Type == "database" {
				spec.DBName, spec.DBUser = r.Str("databaseName"), r.Str("databaseUser")
				spec.Password = r.Str("databasePassword")
			}
		}
	}
	return spec
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

// appNameOf is the name Dokploy gave a workload on Docker.
func (d PrepareDeps) appNameOf(id string) string {
	for _, rows := range d.Source.Rows {
		for _, r := range rows {
			if idOf(r) == id {
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
