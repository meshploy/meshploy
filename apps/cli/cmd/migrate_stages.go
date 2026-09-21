package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
	"github.com/meshploy/apps/cli/internal/setup"
	"github.com/meshploy/packages/client"
	"github.com/meshploy/packages/hostagent"
)

// The stages that change a server, run on the host as root.
//
// Each one is reachable two ways - a host-agent request from the console, and a
// command here - and both go through the same function, so a migration driven
// from a terminal and one driven from the browser do exactly the same thing.

// migrationRuntime is what every stage after prepare needs: the plan the
// operator confirmed, the credential, the journal, and a way into Meshploy.
type migrationRuntime struct {
	plan    *dokploy.Plan
	api     dokploy.ClientAPI
	journal *journal.Journal
	// caddyData is where Meshploy's Caddy keeps its storage on the host.
	caddyData string
}

func openMigration() (*migrationRuntime, error) {
	plan, _, err := setup.ReadConfirmedPlan()
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("no confirmed migration plan on this server")
	}
	cred, err := readMigrationCredential()
	if err != nil {
		return nil, err
	}
	j, err := journal.Open(setup.MigrationDir())
	if err != nil {
		return nil, err
	}
	return &migrationRuntime{
		plan:      plan,
		api:       dokploy.ClientAPI{C: client.New(cred.BaseURL, cred.Token), OrgID: cred.OrgID},
		journal:   j,
		caddyData: caddyDataDir(),
	}, nil
}

// publishProgress summarises the journal where the console can read it.
//
// Called at the end of every stage, whichever way the stage was asked for: a
// migration driven from a terminal has to show the same state in the browser
// as it does in the shell, and the journal it is read from is not something the
// API may read for itself.
func (rt *migrationRuntime) publishProgress() {
	status := dokploy.Progress(*rt.plan, rt.journal, time.Now())
	body, err := json.Marshal(status)
	if err != nil {
		return
	}
	_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.StatusFile), body, 0o644)
}

// runMigrateMove moves one group: stage 2.
func runMigrateMove(groupID string) ([]byte, error) {
	if groupID == "" {
		return nil, fmt.Errorf("which group? name one from the plan")
	}
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()

	var group dokploy.Group
	for _, g := range rt.plan.Groups {
		if g.ID == groupID || g.Name == groupID {
			group = g
		}
	}
	if group.ID == "" {
		return nil, fmt.Errorf("no group %q in the confirmed plan", groupID)
	}

	// What the group carries is read from Dokploy now rather than from the
	// plan: plan.json holds no credentials by design, and a database is
	// reached with the ones the data already uses.
	data, err := moveDataMover(rt, group)
	if err != nil {
		return nil, err
	}

	result, err := dokploy.Move(dokploy.MoveDeps{
		Plan:    *rt.plan,
		Group:   group,
		API:     rt.api,
		Edge:    dokploy.EdgeSwitcher{Target: dokploy.ProxyTarget(proxyHostAddr(), 0), Journal: rt.journal},
		Control: dokploy.Control{Runner: migrate.ExecRunner{}, Journal: rt.journal},
		Probe:   dokploy.HTTPProbe{Addr: probeAddr()},
		Data:    data,
		Journal: rt.journal,
	})
	body, marshalErr := json.Marshal(result)
	defer rt.publishProgress()
	if err != nil {
		_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.MoveFile), body, 0o600)
		return nil, err
	}
	return body, marshalErr
}

// moveDataMover is what copies a group's data, or nil for a group that carries
// none - a stateless application needs no cluster access, and asking for it
// would fail a move that never had to touch the cluster directly.
func moveDataMover(rt *migrationRuntime, group dokploy.Group) (*dokploy.DataMover, error) {
	if len(group.Data) == 0 {
		return nil, nil
	}
	src, err := dokploy.Collect(migrate.ExecRunner{})
	if err != nil {
		return nil, err
	}
	kube, err := migrate.FindKube(migrate.ExecStreamer{})
	if err != nil {
		return nil, err
	}
	return &dokploy.DataMover{
		Runner:  migrate.ExecRunner{},
		Stream:  migrate.ExecStreamer{},
		Kube:    kube,
		API:     rt.api,
		Journal: rt.journal,
		Plan:    *rt.plan,
		Source:  src,
		Dir:     rt.journal.Dir(),
	}, nil
}

// unservedAtCutover is the domains that stop being served when the ports change
// hands, because what they point at could not move.
func unservedAtCutover() ([]string, error) {
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()
	return dokploy.PendingAtCutover(*rt.plan, rt.journal).StuckDomains, nil
}

// runMigrateCutover hands over ports 80 and 443: stage 3.
func runMigrateCutover() ([]byte, error) {
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()

	// What has not moved is Cutover's own rule now: it refuses while anything
	// can still move, and names what cannot.

	result, err := dokploy.Cutover(dokploy.CutoverDeps{
		Plan:    *rt.plan,
		Runner:  migrate.ExecRunner{},
		Journal: rt.journal,
		Probe:   dokploy.HTTPProbe{Addr: probeAddr()},
		Edge:    edgeHolderFor(*rt.plan),
		// Dokploy runs its own server as a Swarm service called "dokploy".
		ControlPlane:   dokploy.ControlPlane{Kind: "swarm", Name: "dokploy"},
		AcmePath:       dokploy.FindAcmeStore(dokploy.DefaultTraefikDir),
		CertDir:        dokploy.FindCertificateDir(),
		TraefikDir:     dokploy.DefaultTraefikDir,
		CaddyData:      rt.caddyData,
		StartCaddy:     startMeshployEdge,
		CaddyContainer: meshployCaddyContainer,
		Now:            time.Now,
	})
	body, marshalErr := json.Marshal(result)
	defer rt.publishProgress()
	if err != nil {
		_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.CutoverFile), body, 0o600)
		return nil, err
	}
	return body, marshalErr
}

// MovedData is data this migration already copied into Meshploy, and when.
//
// It is what a rollback cannot put back: Dokploy's copy is exactly as it was,
// because the copy only ever read it, but everything written into Meshploy
// since the group moved is in Meshploy alone. An operator has to hear that
// before the rollback, not after.
type MovedData struct {
	Name string
	At   time.Time
}

// movedData reads the journal for copies that already happened, newest first.
func movedData(group string) ([]MovedData, error) {
	entries, err := journal.Read(setup.MigrationDir())
	if err != nil {
		return nil, err
	}
	var out []MovedData
	for _, e := range entries {
		if e.Result != journal.OK || (group != "" && e.Group != group) {
			continue
		}
		switch e.Action {
		case "restore-database", "copy-volume":
			out = append(out, MovedData{Name: e.Target, At: e.At})
		}
	}
	return out, nil
}

// runMigrateFinish removes what is left of the platform that was migrated:
// stage 4. planOnly stops after working out what would go.
func runMigrateFinish(removeVolumes, planOnly bool) ([]byte, error) {
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()

	src, err := dokploy.Collect(migrate.ExecRunner{})
	if err != nil {
		return nil, err
	}
	deps := dokploy.FinishDeps{
		Plan:          *rt.plan,
		Source:        src,
		Runner:        migrate.ExecRunner{},
		Journal:       rt.journal,
		API:           rt.api,
		RemoveVolumes: removeVolumes,
		EtcDir:        dokploy.DefaultEtcDir,
		Now:           time.Now,
	}
	if planOnly {
		return json.Marshal(dokploy.FinishScope(dokploy.PlanFinish(deps)))
	}
	result, err := dokploy.Finish(deps)
	defer rt.publishProgress()
	body, marshalErr := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.FinishFile), body, 0o600)
	return body, marshalErr
}

// runMigrateRollback undoes one group, or everything.
func runMigrateRollback(groupID string) ([]byte, error) {
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()

	// Rollback is available until finish, and not after: what it would put
	// back has been removed.
	if dokploy.Finished(rt.journal) {
		return nil, fmt.Errorf("this migration was finished: the platform it would go back to has been removed")
	}
	entries, err := journal.Read(rt.journal.Dir())
	if err != nil {
		return nil, err
	}
	res := dokploy.Rollback{Runner: migrate.ExecRunner{}, Meshploy: rt.api, Journal: rt.journal}.
		Replay(journal.Undoable(entries, groupID))
	defer rt.publishProgress()
	body, marshalErr := json.Marshal(res)
	if len(res.Failures) > 0 {
		_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.RollbackFile), body, 0o600)
		return nil, fmt.Errorf("%d step(s) could not be undone; the first was: %s", len(res.Failures), res.Failures[0])
	}
	return body, marshalErr
}

// groupMoved reports whether every member of a group has started in Meshploy.
func groupMoved(j *journal.Journal, g dokploy.Group) bool {
	for _, m := range g.Members {
		if !j.Done(fmt.Sprintf("move/%s/start/%s", g.ID, m.ID)) {
			return false
		}
	}
	return len(g.Members) > 0
}

// edgeHolderFor reads what holds 80 and 443 from the plan's own detection.
//
// What the host found holding them comes first, whatever it turned out to be:
// an edge that is not the platform's is still a Swarm service, a container or a
// unit, and that is all cutover needs to take the ports. The names below are
// the fallback for a plan made before the host looked.
func edgeHolderFor(plan dokploy.Plan) dokploy.EdgeHolder {
	if h := plan.Edge.Holder; h != nil && h.Kind != "" {
		if h.Stoppable() {
			return dokploy.EdgeHolder{Kind: h.Kind, Name: h.Name}
		}
		return dokploy.EdgeHolder{Note: h.Detail}
	}
	switch plan.Edge.Kind {
	case "traefik-service":
		return dokploy.EdgeHolder{Kind: "swarm", Name: "dokploy-traefik"}
	case "traefik-container":
		return dokploy.EdgeHolder{Kind: "container", Name: "dokploy-traefik"}
	}
	return dokploy.EdgeHolder{}
}

// proxyHostAddr is where the edge still holding 443 forwards a switched domain:
// Meshploy's proxy, reached from inside a container.
//
// The mesh address first, because that is where the proxy answers a caller that
// is not in this host's network namespace. The proxy binds loopback for Caddy,
// which a container cannot use, and it is not published on the Docker bridge -
// a switch pointed there is refused, and the domain 502s while the move reports
// success. MESH_IP comes from the install's own environment; the bridge remains
// the fallback for an install without a mesh.
func proxyHostAddr() string {
	if v := os.Getenv("MESHPLOY_PROXY_ADDR"); v != "" {
		return v
	}
	if v := meshIPFromEnvFile(); v != "" {
		return v
	}
	return dockerBridgeIP()
}

// dockerBridgeIP is how a container reaches this host when there is no mesh.
func dockerBridgeIP() string {
	if v := os.Getenv("HOST_GATEWAY_IP"); v != "" {
		return v
	}
	return "172.17.0.1"
}

// meshIPFromEnvFile reads MESH_IP from the install's environment. The CLI runs
// as a host binary and does not inherit the API's environment, so the file the
// installer wrote is where this lives.
func meshIPFromEnvFile() string {
	if v := os.Getenv("MESH_IP"); v != "" {
		return v
	}
	f, err := os.Open("/opt/meshploy/.env")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "MESH_IP=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "MESH_IP="), `"'`)
	}
	return ""
}

// probeAddr is where a moved domain is checked.
//
// The edge that answers on 80 during stage 2 is Dokploy's, and on most servers
// it holds 80 directly. Where something else does - a host Caddy in front of
// Traefik, as on one of the reference servers - the port differs and the
// operator says so.
func probeAddr() string {
	if v := os.Getenv("MESHPLOY_MIGRATE_PROBE_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:80"
}

// caddyDataDir is where Meshploy's Caddy volume is mounted on the host.
func caddyDataDir() string {
	if v := os.Getenv("MESHPLOY_CADDY_DATA"); v != "" {
		return v
	}
	return "/var/lib/docker/volumes/meshploy_caddy_data/_data"
}

// meshployCaddyContainer is Meshploy's edge, named by its compose project -
// the install directory, which is always this one.
const meshployCaddyContainer = "meshploy-caddy-1"

// startMeshployEdge brings Meshploy's Caddy up on 80 and 443.
func startMeshployEdge() error {
	out, err := migrate.ExecRunner{}.Output("docker", "compose", "-f", "/opt/meshploy/docker-compose.yml", "up", "-d", "caddy")
	if err != nil {
		return fmt.Errorf("start Meshploy's edge: %w (%s)", err, out)
	}
	return nil
}
