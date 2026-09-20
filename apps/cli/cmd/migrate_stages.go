package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	result, err := dokploy.Move(dokploy.MoveDeps{
		Plan:    *rt.plan,
		Group:   group,
		API:     rt.api,
		Edge:    dokploy.EdgeSwitcher{Target: dokploy.ProxyTarget(dockerBridgeIP(), 0), Journal: rt.journal},
		Control: dokploy.Control{Runner: migrate.ExecRunner{}, Journal: rt.journal},
		Probe:   dokploy.HTTPProbe{Addr: probeAddr()},
		Journal: rt.journal,
	})
	body, marshalErr := json.Marshal(result)
	if err != nil {
		_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.MoveFile), body, 0o600)
		return nil, err
	}
	return body, marshalErr
}

// runMigrateCutover hands over ports 80 and 443: stage 3.
func runMigrateCutover() ([]byte, error) {
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()

	for _, g := range rt.plan.Groups {
		if !groupMoved(rt.journal, g) {
			return nil, fmt.Errorf("%s has not moved yet: cutover takes the ports from every domain at once", g.Name)
		}
	}

	result, err := dokploy.Cutover(dokploy.CutoverDeps{
		Plan:       *rt.plan,
		Runner:     migrate.ExecRunner{},
		Journal:    rt.journal,
		Probe:      dokploy.HTTPProbe{Addr: probeAddr()},
		Edge:       edgeHolderFor(*rt.plan),
		AcmePath:   filepath.Join(dokploy.DefaultTraefikDir, "acme.json"),
		TraefikDir: dokploy.DefaultTraefikDir,
		CaddyData:  rt.caddyData,
		StartCaddy: startMeshployEdge,
		Now:        time.Now,
	})
	body, marshalErr := json.Marshal(result)
	if err != nil {
		_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.CutoverFile), body, 0o600)
		return nil, err
	}
	return body, marshalErr
}

// runMigrateRollback undoes one group, or everything.
func runMigrateRollback(groupID string) ([]byte, error) {
	rt, err := openMigration()
	if err != nil {
		return nil, err
	}
	defer rt.journal.Close()

	entries, err := journal.Read(rt.journal.Dir())
	if err != nil {
		return nil, err
	}
	res := dokploy.Rollback{Runner: migrate.ExecRunner{}, Meshploy: rt.api}.
		Replay(journal.Undoable(entries, groupID))
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
func edgeHolderFor(plan dokploy.Plan) dokploy.EdgeHolder {
	switch plan.Edge.Kind {
	case "traefik-service":
		return dokploy.EdgeHolder{Kind: "swarm", Name: "dokploy-traefik"}
	case "traefik-container":
		return dokploy.EdgeHolder{Kind: "container", Name: "dokploy-traefik"}
	}
	return dokploy.EdgeHolder{}
}

// dockerBridgeIP is how a container reaches this host. Traefik forwards a
// switched domain to Meshploy's proxy there.
func dockerBridgeIP() string {
	if v := os.Getenv("HOST_GATEWAY_IP"); v != "" {
		return v
	}
	return "172.17.0.1"
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

// startMeshployEdge brings Meshploy's Caddy up on 80 and 443.
func startMeshployEdge() error {
	out, err := migrate.ExecRunner{}.Output("docker", "compose", "-f", "/opt/meshploy/docker-compose.yml", "up", "-d", "caddy")
	if err != nil {
		return fmt.Errorf("start Meshploy's edge: %w (%s)", err, out)
	}
	return nil
}
