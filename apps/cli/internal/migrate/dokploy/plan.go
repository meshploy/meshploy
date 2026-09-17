package dokploy

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// Verdicts.
const (
	Moves    = "moves"
	NeedsYou = "needs_you"
	NotMoved = "not_moved"
)

// Run modes, chosen from the host's resources.
const (
	ModeSideBySide = "side_by_side"
	ModeHandOver   = "hand_over"
	ModeMoveToNode = "move_to_node"
)

// logicalDumpLimitMB is the size up to which a database moves by dump and
// restore; larger ones move by copying the volume with the container stopped.
const logicalDumpLimitMB = 2048

// meshployBudgetMB is roughly what Meshploy's own stack needs: API, Postgres,
// Headscale, Caddy, the proxy and k3s.
const meshployBudgetMB = 1536

// Plan is what moving this host would take. It holds no secret values.
type Plan struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Detection   Detection         `json:"detection"`
	Edge        Edge              `json:"edge"`
	Resources   migrate.Resources `json:"resources"`
	Mode        Mode              `json:"mode"`
	Items       []Item            `json:"items"`
	Unmanaged   []Unmanaged       `json:"unmanaged"`
	Summary     map[string]int    `json:"summary"`
}

// Item is one thing in Dokploy and what becomes of it.
type Item struct {
	Kind    string            `json:"kind"`
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Project string            `json:"project,omitempty"`
	MapsTo  string            `json:"maps_to"`
	Verdict string            `json:"verdict"`
	Reasons []string          `json:"reasons,omitempty"`
	Running *bool             `json:"running,omitempty"`
	Details map[string]string `json:"details,omitempty"`
	// Decisions are what needs you: one per question, each with a default
	// where one is safe.
	Decisions []Decision `json:"decisions,omitempty"`
	// Redirects are Dokploy regex redirects worked out into route redirects.
	Redirects []Redirect `json:"redirects,omitempty"`
}

// Edge is what serves ports 80 and 443.
type Edge struct {
	// Kind is traefik-service, traefik-container, custom, or none.
	Kind    string   `json:"kind"`
	Holders []string `json:"holders"`
	// Traefik is where Dokploy's Traefik runs when something else holds the
	// ports in front of it.
	Traefik      string `json:"traefik,omitempty"`
	DynamicFiles int    `json:"dynamic_files"`
	AcmeBytes    int64  `json:"acme_bytes"`
	Note         string `json:"note,omitempty"`
}

// Mode is how the move would run on this host, and why.
type Mode struct {
	Choice string `json:"choice"`
	Reason string `json:"reason"`
}

// Unmanaged is a container or service no Dokploy row owns.
type Unmanaged struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // compose-project, container, swarm-service
	Images  string `json:"images"`
	Running int    `json:"running"`
	Total   int    `json:"total"`
	MapsTo  string `json:"maps_to"`
}

// BuildPlan maps what was read into a plan. It changes nothing.
func BuildPlan(src Source, now time.Time) Plan {
	p := Plan{
		GeneratedAt: now.UTC(),
		Detection:   src.Detection,
		Resources:   src.Resources,
		Summary:     map[string]int{Moves: 0, NeedsYou: 0, NotMoved: 0},
	}
	b := &builder{src: src, rows: src.Rows}
	b.index()

	b.projects()
	b.applications()
	b.composes()
	b.databases()
	b.domains()
	b.integrations()
	b.backups()
	b.servers()

	p.Items = b.items
	if p.Items == nil {
		p.Items = []Item{}
	}
	p.Summary["open_decisions"] = 0
	for _, it := range p.Items {
		p.Summary[it.Verdict]++
		for _, d := range it.Decisions {
			if d.Default == "" {
				p.Summary["open_decisions"]++
			}
		}
	}
	p.Edge = readEdge(src)
	p.Unmanaged = unmanaged(src, b.appNames)
	for _, c := range src.Docker.Containers {
		if !dokployOwn(c.Name) && !dokployOwn(c.Service) && c.State == "running" {
			p.Resources.WorkloadMemoryMB += c.MemoryMB
		}
	}
	src.Resources = p.Resources
	p.Mode = chooseMode(src)
	return p
}

type builder struct {
	src      Source
	rows     map[string][]Row
	items    []Item
	env      map[string]Row // environmentId -> environment
	project  map[string]Row // projectId -> project
	appNames map[string]bool
	// secured counts basic auth rows, and redirects holds redirect rows, per
	// applicationId.
	secured   map[string]int
	redirects map[string][]Row
	hosts     map[string][]string // applicationId -> its domains
	migrated  map[string]bool     // every domain that moves
	providers map[string]Row      // gitProviderId -> git_provider
}

func (b *builder) index() {
	b.env, b.project, b.appNames = map[string]Row{}, map[string]Row{}, map[string]bool{}
	b.secured, b.redirects, b.providers = map[string]int{}, map[string][]Row{}, map[string]Row{}
	b.hosts, b.migrated = map[string][]string{}, map[string]bool{}
	for _, r := range b.rows["domain"] {
		host := r.Str("host")
		if r.Str("applicationId") != "" {
			b.hosts[r.Str("applicationId")] = append(b.hosts[r.Str("applicationId")], host)
		}
		if !strings.HasSuffix(host, ".traefik.me") && r.Str("previewDeploymentId") == "" {
			b.migrated[host] = true
		}
	}
	for _, r := range b.rows["environment"] {
		b.env[r.Str("environmentId")] = r
	}
	for _, r := range b.rows["project"] {
		b.project[r.Str("projectId")] = r
	}
	for _, r := range b.rows["security"] {
		b.secured[r.Str("applicationId")]++
	}
	for _, r := range b.rows["redirect"] {
		b.redirects[r.Str("applicationId")] = append(b.redirects[r.Str("applicationId")], r)
	}
	for _, r := range b.rows["git_provider"] {
		b.providers[r.Str("gitProviderId")] = r
	}
}

func (b *builder) add(it Item) {
	// Whatever a check found, an item that asks something needs you, and one
	// that asks nothing and was not ruled out moves.
	if it.Verdict != NotMoved {
		it.Verdict = Moves
		if len(it.Decisions) > 0 {
			it.Verdict = NeedsYou
		}
	}
	if it.Details != nil {
		for k, v := range it.Details {
			if v == "" {
				delete(it.Details, k)
			}
		}
	}
	b.items = append(b.items, it)
}

// projectName is the Meshploy project a row lands in: the Dokploy project for
// its default environment, "project · environment" for any other.
func (b *builder) projectName(r Row) string {
	env, ok := b.env[r.Str("environmentId")]
	if !ok {
		return b.project[r.Str("projectId")].Str("name")
	}
	name := b.project[env.Str("projectId")].Str("name")
	if env.Bool("isDefault") || env.Str("name") == "" {
		return name
	}
	return name + " · " + env.Str("name")
}

func (b *builder) projects() {
	for _, env := range b.rows["environment"] {
		proj := b.project[env.Str("projectId")]
		it := Item{Kind: "project", ID: env.Str("environmentId"), MapsTo: "Project", Verdict: Moves,
			Details: map[string]string{"dokploy_project": proj.Str("name"), "dokploy_environment": env.Str("name")}}
		it.Name = proj.Str("name")
		if !env.Bool("isDefault") {
			it.Name += " · " + env.Str("name")
			it.Reasons = append(it.Reasons, "a non-default environment becomes a project of its own")
		}
		if n := proj.Lines("env") + env.Lines("env"); n > 0 {
			it.Details["shared_variables"] = fmt.Sprint(n)
			it.Reasons = append(it.Reasons, "project and environment variables become project variables, merged into every service")
		}
		b.add(it)
	}
}

// swarmRunning reports a Swarm service's state: running, stopped, or unknown.
func (b *builder) swarmRunning(appName string) *bool {
	svc, ok := b.src.Docker.Service(appName)
	if !ok {
		return nil
	}
	running := svc.Running > 0
	return &running
}

func (b *builder) applications() {
	for _, r := range b.rows["application"] {
		name := r.Str("appName")
		b.appNames[name] = true
		it := Item{Kind: "application", ID: r.Str("applicationId"), Name: r.Str("name"), Project: b.projectName(r),
			Verdict: Moves, Running: b.swarmRunning(name),
			Details: map[string]string{
				"app_name": name, "source": r.Str("sourceType"), "build": r.Str("buildType"),
				"replicas": r.Str("replicas"), "status": r.Str("applicationStatus"),
			}}
		if n := r.Lines("env"); n > 0 {
			it.Details["variables"] = fmt.Sprint(n)
		}

		switch src := r.Str("sourceType"); src {
		case "docker":
			it.MapsTo = "Image service"
			it.Details["image"] = r.Str("dockerImage")
		case "drop":
			it.MapsTo = "Image service, from the image running now"
			it.Decisions = append(it.Decisions, Decision{ID: "source", Question: "Uploaded source (drop): there is no repository to build from",
				Options: []Option{optImageOnly}, Default: optImageOnly.ID})
		default:
			it.MapsTo = "Service built from git (" + r.Str("buildType") + ")"
			it.Details["repository"], it.Details["branch"] = gitSource(r)
			switch r.Str("buildType") {
			case "heroku_buildpacks", "paketo_buildpacks":
				it.Decisions = append(it.Decisions, Decision{ID: "builder", Question: r.Str("buildType") + " has no Meshploy builder",
					Options: []Option{optImageOnly, {"nixpacks", "Build with Nixpacks"}, {"railpack", "Build with Railpack"}, {"dockerfile", "Build from a Dockerfile"}},
					Default: optImageOnly.ID})
			case "static":
				it.Reasons = append(it.Reasons, "static site: rebuilt with Railpack")
			}
			if src == "github" {
				it.Decisions = append(it.Decisions, githubDecision())
			}
		}
		if r.Str("sourceType") != "docker" {
			it.Reasons = append(it.Reasons, "starts from the image running now; the next deploy builds from its source")
		}

		if b.secured[it.ID] > 0 {
			it.Decisions = append(it.Decisions, Decision{ID: "basic_auth", Question: "Protected with basic auth, which Meshploy routes do not have yet",
				Options: []Option{{"keep_paused", "Keep its routes paused"}, {"publish", "Publish its routes without a password"}}, Default: "keep_paused"})
		}
		if rules := b.redirects[it.ID]; len(rules) > 0 {
			resolved, unresolved := resolveRedirects(rules, b.hosts[it.ID], b.migrated)
			it.Redirects = resolved
			for _, rd := range resolved {
				it.Reasons = append(it.Reasons, fmt.Sprintf("redirect %s → %s (%d) becomes a route redirect", rd.From, rd.To, rd.Code))
			}
			if len(unresolved) > 0 {
				it.Decisions = append(it.Decisions, Decision{ID: "redirects",
					Question: "Redirects that are not a plain host-to-host redirect: " + strings.Join(unresolved, "; "),
					Options:  []Option{{"by_hand", "Recreate them by hand after the move"}, {"drop", "Drop them"}}})
			}
		}
		if r.Str("serverId") != "" {
			it.Verdict = NotMoved
			it.Reasons = []string{"runs on a remote Dokploy server, which is not migrated"}
		}
		if r.Bool("isPreviewDeploymentsActive") {
			it.Reasons = append(it.Reasons, "preview deployments are not migrated")
		}
		b.stoppedNote(&it)
		if it.Verdict != NotMoved {
			b.mounts(&it, "applicationId")
			b.ports(&it)
		}
		b.add(it)
	}
}

// gitSource returns a repository and branch without credentials, whichever
// provider columns the row uses.
func gitSource(r Row) (repo, branch string) {
	switch r.Str("sourceType") {
	case "github":
		return join(r.Str("owner"), r.Str("repository")), r.Str("branch")
	case "gitlab":
		repo = r.Str("gitlabPathNamespace")
		if repo == "" {
			repo = join(r.Str("gitlabOwner"), r.Str("gitlabRepository"))
		}
		return repo, r.Str("gitlabBranch")
	case "gitea":
		return join(r.Str("giteaOwner"), r.Str("giteaRepository")), r.Str("giteaBranch")
	case "bitbucket":
		return join(r.Str("bitbucketOwner"), r.Str("bitbucketRepository")), r.Str("bitbucketBranch")
	case "git", "raw":
		return withoutUserinfo(r.Str("customGitUrl")), r.Str("customGitBranch")
	}
	return "", ""
}

func join(owner, repo string) string {
	if owner == "" {
		return repo
	}
	return owner + "/" + repo
}

// withoutUserinfo removes credentials from a clone URL.
func withoutUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func (b *builder) stoppedNote(it *Item) {
	if it.Running != nil && !*it.Running && it.Verdict != NotMoved {
		it.Reasons = append(it.Reasons, "stopped in Dokploy: created stopped")
	}
}

func (b *builder) mounts(it *Item, key string) {
	appName := it.Details["app_name"]
	var volumes, binds, files int
	for _, m := range b.rows["mount"] {
		if m.Str(key) != it.ID {
			continue
		}
		switch m.Str("type") {
		case "volume":
			volumes++
		case "file":
			files++
		case "bind":
			binds++
			if host := m.Str("hostPath"); host != "" && !strings.HasPrefix(host, "/etc/dokploy/") {
				it.Decisions = append(it.Decisions, b.mountDecision(host, appName))
			}
		}
	}
	if volumes > 0 {
		it.Details["volumes"] = fmt.Sprint(volumes)
	}
	if binds > 0 {
		it.Details["bind_mounts"] = fmt.Sprint(binds)
	}
	if files > 0 {
		it.Details["config_files"] = fmt.Sprint(files)
	}
}

func (b *builder) ports(it *Item) {
	var published []string
	for _, p := range b.rows["port"] {
		if p.Str("applicationId") == it.ID {
			published = append(published, p.Str("publishedPort")+"/"+p.Str("protocol"))
		}
	}
	if len(published) > 0 {
		it.Details["published_ports"] = strings.Join(published, ", ")
		it.Reasons = append(it.Reasons, "published ports become TCP routes on the gateway")
	}
}

func (b *builder) composes() {
	for _, r := range b.rows["compose"] {
		name := r.Str("appName")
		b.appNames[name] = true
		it := Item{Kind: "compose", ID: r.Str("composeId"), Name: r.Str("name"), Project: b.projectName(r), Verdict: Moves,
			Details: map[string]string{"app_name": name, "source": r.Str("sourceType"), "type": r.Str("composeType"),
				"compose_path": r.Str("composePath"), "status": r.Str("composeStatus")}}
		if r.Str("sourceType") == "raw" {
			it.MapsTo = "Stack from the stored compose file"
		} else {
			it.MapsTo = "Stack from git"
			it.Details["repository"], it.Details["branch"] = gitSource(r)
			if r.Str("sourceType") == "github" {
				it.Decisions = append(it.Decisions, githubDecision())
			}
		}
		if r.Str("composeType") == "stack" {
			it.Reasons = append(it.Reasons, "Swarm stack: deploy keys that map (replicas, resources) are kept, the rest reported")
		}
		if r.Bool("isolatedDeployment") {
			it.Details["isolated"] = "true"
		}
		if n := r.Lines("env"); n > 0 {
			it.Details["variables"] = fmt.Sprint(n)
		}
		running, total := 0, 0
		for _, c := range b.src.Docker.Containers {
			if c.Project == name {
				total++
				if c.State == "running" {
					running++
				}
			}
		}
		if total > 0 {
			on := running > 0
			it.Running = &on
			it.Details["containers"] = fmt.Sprintf("%d of %d running", running, total)
		}
		it.Reasons = append(it.Reasons, "Dokploy's Traefik labels and dokploy-network are removed; routes come from its domains")
		if r.Str("serverId") != "" {
			it.Verdict = NotMoved
			it.Reasons = []string{"runs on a remote Dokploy server, which is not migrated"}
		}
		b.stoppedNote(&it)
		if it.Verdict != NotMoved {
			b.mounts(&it, "composeId")
		}
		b.add(it)
	}
}

var engines = []struct{ table, id, label, target string }{
	{"postgres", "postgresId", "Postgres", "Managed Postgres"},
	{"mysql", "mysqlId", "MySQL", "Managed MySQL"},
	{"mariadb", "mariadbId", "MariaDB", "Stack service with the same image"},
	{"mongo", "mongoId", "MongoDB", "Managed MongoDB"},
	{"redis", "redisId", "Redis", "Managed Redis"},
}

func (b *builder) databases() {
	for _, e := range engines {
		for _, r := range b.rows[e.table] {
			name := r.Str("appName")
			b.appNames[name] = true
			it := Item{Kind: "database", ID: r.Str(e.id), Name: r.Str("name"), Project: b.projectName(r),
				MapsTo: e.target, Verdict: Moves, Running: b.swarmRunning(name),
				Details: map[string]string{"engine": e.label, "image": r.Str("dockerImage"), "app_name": name,
					"database": r.Str("databaseName"), "status": r.Str("applicationStatus")}}
			if e.table == "mariadb" {
				it.Reasons = append(it.Reasons, "Meshploy has no managed MariaDB")
			}
			if port := r.Str("externalPort"); port != "" {
				it.Details["external_port"] = port
				it.Reasons = append(it.Reasons, "published on port "+port+": becomes a TCP route on the gateway")
			}
			if mb := b.src.Docker.VolumeMB(name + "-data"); mb >= 0 {
				it.Details["data_mb"] = fmt.Sprint(mb)
				if mb < logicalDumpLimitMB {
					it.Details["data_move"] = "dump and restore"
				} else {
					it.Details["data_move"] = "volume copy, stopped"
					it.Reasons = append(it.Reasons, fmt.Sprintf("%d MB of data: moved by copying the volume with the database stopped", mb))
				}
			}
			if r.Str("serverId") != "" {
				it.Verdict = NotMoved
				it.Reasons = []string{"runs on a remote Dokploy server, which is not migrated"}
			}
			b.stoppedNote(&it)
			b.add(it)
		}
	}
}

func (b *builder) domains() {
	owner := map[string]string{}
	for _, r := range b.rows["application"] {
		owner[r.Str("applicationId")] = r.Str("name")
	}
	for _, r := range b.rows["compose"] {
		owner[r.Str("composeId")] = r.Str("name")
	}
	for _, r := range b.rows["domain"] {
		host := r.Str("host")
		target := owner[r.Str("applicationId")]
		if target == "" {
			target = owner[r.Str("composeId")]
			if svc := r.Str("serviceName"); svc != "" && target != "" {
				target += " / " + svc
			}
		}
		it := Item{Kind: "domain", ID: r.Str("domainId"), Name: host + r.Str("path"), MapsTo: "Route to " + target, Verdict: Moves,
			Details: map[string]string{"https": r.Str("https"), "certificate": r.Str("certificateType"), "port": r.Str("port")}}
		switch {
		case r.Str("previewDeploymentId") != "" || r.Str("domainType") == "preview":
			it.Verdict, it.Reasons = NotMoved, []string{"a preview deployment's domain"}
		case strings.HasSuffix(host, ".traefik.me"):
			it.Verdict, it.Reasons = NotMoved, []string{"a throwaway traefik.me host Dokploy generates"}
		case r.Str("certificateType") == "custom":
			it.Decisions = append(it.Decisions, certificateDecision())
		}
		if v, ok := r["enabled"].(bool); ok && !v && it.Verdict == Moves {
			it.Reasons = append(it.Reasons, "disabled in Dokploy: the route is created paused")
		}
		if p := r.Str("path"); p != "" && p != "/" {
			it.Details["path"] = p
			if r.Bool("stripPath") {
				it.Details["strip_path"] = "true"
			}
		}
		b.add(it)
	}
}

func (b *builder) integrations() {
	for _, r := range b.rows["git_provider"] {
		it := Item{Kind: "git_provider", ID: r.Str("gitProviderId"), Name: r.Str("name"), Verdict: Moves,
			Details: map[string]string{"provider": r.Str("providerType")}}
		switch r.Str("providerType") {
		case "github":
			it.MapsTo = "Git integration"
			it.Decisions = append(it.Decisions, Decision{ID: "github", Question: "A GitHub App installation cannot be moved",
				Options: []Option{{"reconnect", "Reconnect GitHub in Meshploy"}, {"skip", "Skip it: apps built from it run from their images"}}, Default: "skip"})
		default:
			it.MapsTo = "Git integration, token copied"
		}
		b.add(it)
	}
	for _, r := range b.rows["registry"] {
		b.add(Item{Kind: "registry", ID: r.Str("registryId"), Name: r.Str("registryName"), MapsTo: "Registry integration", Verdict: Moves,
			Details: map[string]string{"url": r.Str("registryUrl")}})
	}
	for _, r := range b.rows["destination"] {
		b.add(Item{Kind: "destination", ID: r.Str("destinationId"), Name: r.Str("name"), MapsTo: "Storage integration", Verdict: Moves,
			Details: map[string]string{"provider": r.Str("provider"), "bucket": r.Str("bucket"), "endpoint": r.Str("endpoint")}})
	}
	for _, r := range b.rows["certificate"] {
		b.add(Item{Kind: "certificate", ID: r.Str("certificateId"), Name: r.Str("name"), MapsTo: "Route certificate",
			Decisions: []Decision{certificateDecision()}})
	}
}

func (b *builder) backups() {
	for _, r := range b.rows["backup"] {
		it := Item{Kind: "backup", ID: r.Str("backupId"), Name: r.Str("databaseType") + " backup, " + r.Str("schedule"),
			Details: map[string]string{"schedule": r.Str("schedule"), "keep": r.Str("keepLatestCount"), "enabled": r.Str("enabled")}}
		switch r.Str("databaseType") {
		case "postgres", "mysql", "mariadb", "mongo":
			it.MapsTo, it.Verdict = "Backup schedule to the same storage", Moves
		default:
			it.MapsTo, it.Verdict = "Not a database backup", NotMoved
			it.Reasons = []string{r.Str("databaseType") + " backups are not migrated; Meshploy's system backup covers its own database"}
		}
		b.add(it)
	}
}

func (b *builder) servers() {
	for _, r := range b.rows["server"] {
		b.add(Item{Kind: "server", ID: r.Str("serverId"), Name: r.Str("name"), MapsTo: "Nothing", Verdict: NotMoved,
			Reasons: []string{"remote Dokploy servers are not migrated; their workloads stay where they are"}})
	}
}

// ── Edge, unmanaged, mode ────────────────────────────────────────────────────

func readEdge(src Source) Edge {
	e := Edge{DynamicFiles: src.DynamicFiles, AcmeBytes: src.AcmeBytes, Kind: "none", Holders: []string{}}
	holders := append(migrate.Holders(src.Listeners, 80), migrate.Holders(src.Listeners, 443)...)
	e.Holders = dedupe(holders)

	var traefikContainer *migrate.Container
	for i, c := range src.Docker.Containers {
		if c.Name == "dokploy-traefik" || strings.HasPrefix(c.Service, "dokploy-traefik") {
			traefikContainer = &src.Docker.Containers[i]
		}
	}
	_, traefikService := src.Docker.Service("dokploy-traefik")
	onDefaultPorts := traefikContainer != nil && publishes(traefikContainer.Ports, 80)

	switch {
	case len(e.Holders) == 0:
		e.Note = "nothing listens on ports 80 or 443"
	case onlyDockerProxy(e.Holders) && traefikService && onDefaultPorts:
		e.Kind = "traefik-service"
	case onlyDockerProxy(e.Holders) && onDefaultPorts:
		e.Kind = "traefik-container"
	default:
		e.Kind = "custom"
		if traefikContainer != nil {
			e.Traefik = traefikContainer.Ports
		}
		e.Note = "ports 80 and 443 are held by " + strings.Join(e.Holders, ", ") +
			", not by Dokploy's Traefik: its configuration is read and handed over separately"
	}
	return e
}

func publishes(ports string, port int) bool {
	return strings.Contains(ports, fmt.Sprintf(":%d->", port))
}

func onlyDockerProxy(holders []string) bool {
	for _, h := range holders {
		if h != "docker-proxy" && h != "dockerd" {
			return false
		}
	}
	return true
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// dokployOwn are Dokploy's own parts, never workloads.
func dokployOwn(name string) bool {
	for _, p := range []string{"dokploy", "dokploy-postgres", "dokploy-redis", "dokploy-traefik"} {
		if name == p || strings.HasPrefix(name, p+".") {
			return true
		}
	}
	return false
}

// unmanaged lists what runs on the host that no Dokploy row owns. Compose
// containers group by project; Swarm tasks belong to their service.
func unmanaged(src Source, owned map[string]bool) []Unmanaged {
	projects := map[string]*Unmanaged{}
	out := []Unmanaged{}
	for _, c := range src.Docker.Containers {
		if c.Service != "" || dokployOwn(c.Name) {
			continue
		}
		if c.Project != "" {
			if owned[c.Project] {
				continue
			}
			u := projects[c.Project]
			if u == nil {
				u = &Unmanaged{Name: c.Project, Kind: "compose-project", MapsTo: "Stack, through the Docker importer"}
				projects[c.Project] = u
			}
			u.Total++
			if c.State == "running" {
				u.Running++
			}
			if !strings.Contains(u.Images, c.Image) {
				u.Images = strings.TrimPrefix(u.Images+", "+c.Image, ", ")
			}
			continue
		}
		running := 0
		if c.State == "running" {
			running = 1
		}
		out = append(out, Unmanaged{Name: c.Name, Kind: "container", Images: c.Image, Running: running, Total: 1, MapsTo: "Service, through the Docker importer"})
	}
	for _, u := range projects {
		out = append(out, *u)
	}
	for _, s := range src.Docker.Services {
		if dokployOwn(s.Name) || owned[s.Name] {
			continue
		}
		out = append(out, Unmanaged{Name: s.Name, Kind: "swarm-service", Images: s.Image, Running: s.Running, Total: s.Desired, MapsTo: "Service, through the Docker importer"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// chooseMode picks how the move runs from the host's room to spare.
func chooseMode(src Source) Mode {
	res := src.Resources
	largest := 0
	if len(src.Docker.Volumes) > 0 {
		largest = src.Docker.Volumes[0].MB
	}
	// Side by side runs Meshploy and a second copy of every workload while
	// Dokploy's keep serving.
	needed := meshployBudgetMB + res.WorkloadMemoryMB
	switch {
	case res.AvailableMB >= needed && res.DiskFreeMB >= res.DockerVolumeMB*2:
		return Mode{ModeSideBySide, fmt.Sprintf("%d MB of memory free covers Meshploy and a second copy of the workloads (%d MB), and %d MB of disk the data twice: both can run until everything is ready", res.AvailableMB, needed, res.DiskFreeMB)}
	case res.DiskFreeMB >= largest*2:
		return Mode{ModeHandOver, fmt.Sprintf("%d MB of memory free is less than Meshploy and a second copy of the workloads need (%d MB): Dokploy's own UI stops first, then workloads move one at a time", res.AvailableMB, needed)}
	default:
		return Mode{ModeMoveToNode, fmt.Sprintf("the largest volume (%d MB) does not fit twice in %d MB of free disk: move it to another node", largest, res.DiskFreeMB)}
	}
}
