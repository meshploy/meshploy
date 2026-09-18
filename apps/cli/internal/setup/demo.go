package setup

import (
	"context"
	"net"
	"path/filepath"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
)

// Demo pieces stand in for a real server, so the setup page can be opened and
// worked on without one. `meshploy setup serve --demo` wires them up.

// DemoToken is the token the demo page accepts.
const DemoToken = "ms_demo_token"

// NewDemoServer wires the page to the stand-ins below, with its state and its
// confirmed plan inside dir. Nothing outside dir is read or written, which is
// what lets the demo run unprivileged.
// platform names what the stand-in host has on it: "dokploy", or "none" for a
// fresh server, where the page should show no migration step at all.
func NewDemoServer(dir, platform string) (*Server, error) {
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	if err := store.Update(func(st *State) {
		st.Answers.PublicIP = "203.0.113.10"
		st.Answers.MeshIP = "100.64.0.1"
	}); err != nil {
		return nil, err
	}
	migrationDir = filepath.Join(dir, "migrate")

	var planner Planner
	if platform != "none" {
		planner = DemoPlanner()
	}
	return NewServer(store, DemoToken, DemoResolver{}, DemoRunner{}, planner), nil
}

// DemoResolver answers as if DNS were already pointing at this machine.
type DemoResolver struct{}

func (DemoResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	return []string{"203.0.113.10"}, nil
}

func (DemoResolver) LookupNS(_ context.Context, host string) ([]*net.NS, error) {
	return []*net.NS{{Host: "ns1.meshploy.example."}, {Host: "ns2.meshploy.example."}}, nil
}

// DemoRunner prints what an install would print, at a readable pace.
type DemoRunner struct{}

func (DemoRunner) Run(ctx context.Context, a Answers, out func(string)) error {
	for _, line := range []string{
		"▸ Checking the machine",
		"  4 cores, 3.8 GB memory, 38 GB free",
		"▸ Writing /opt/meshploy/.env",
		"▸ Starting PostgreSQL, Headscale, the API and the console",
		"  meshploy-postgres-1  started",
		"  meshploy-headscale-1 started",
		"  meshploy-api-1       started",
		"▸ Joining the mesh",
		"  gateway is 100.64.0.1",
		"▸ Starting CoreDNS and Caddy",
		"▸ Waiting for the API to answer",
		"  healthy",
		"✔ Meshploy is ready",
	} {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		out(line)
	}
	return nil
}

// DemoPlanner returns a plan like a small Dokploy server's, so the migration
// step can be worked on without one.
func DemoPlanner() Planner { return demoPlanner{} }

type demoPlanner struct{}

func (d demoPlanner) Detect(context.Context) (dokploy.Plan, error) {
	return dokploy.BuildPlan(demoSource(false), time.Now()), nil
}

func (d demoPlanner) Plan(context.Context) (dokploy.Plan, error) {
	return dokploy.BuildPlan(demoSource(true), time.Now()), nil
}

func demoSource(withRows bool) dokploy.Source {
	src := dokploy.Source{
		Detection: dokploy.Detection{Dokploy: true, Version: "v0.30.5", Migrations: 191, Supported: true},
		Docker: migrate.Docker{
			Services: []migrate.SwarmService{
				{Name: "shop-web-a1b2c3", Running: 1, Desired: 1},
				{Name: "shop-api-d4e5f6", Running: 1, Desired: 1},
				{Name: "shop-db-g7h8i9", Running: 1, Desired: 1},
			},
			Containers: []migrate.Container{
				{Name: "dokploy-traefik", Image: "traefik:v3.6", State: "running", Ports: "0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp"},
			},
			Volumes: []migrate.Volume{{Name: "shop-db-g7h8i9-data", MB: 1200}},
		},
		Listeners: []migrate.Listener{{Port: 80, Address: "0.0.0.0", Process: "docker-proxy"}, {Port: 443, Address: "0.0.0.0", Process: "docker-proxy"}},
		Resources: migrate.Resources{Cores: 4, MemoryMB: 7900, AvailableMB: 5200, DiskFreeMB: 64000, DockerVolumeMB: 1300},
		PathMB:    map[string]int{"/home/ubuntu/shop/uploads": 340},
	}
	if !withRows {
		return src
	}
	src.Rows = map[string][]dokploy.Row{
		"project":     {{"projectId": "p1", "name": "Shop"}},
		"environment": {{"environmentId": "e1", "name": "production", "projectId": "p1", "isDefault": true}},
		"application": {
			{"applicationId": "a1", "name": "web", "appName": "shop-web-a1b2c3", "environmentId": "e1", "sourceType": "gitlab",
				"buildType": "railpack", "gitlabPathNamespace": "shop/web", "gitlabBranch": "main", "env": "API_URL=https://api.shop.example"},
			{"applicationId": "a2", "name": "api", "appName": "shop-api-d4e5f6", "environmentId": "e1", "sourceType": "github",
				"buildType": "dockerfile", "owner": "shop", "repository": "api", "branch": "main",
				"env": "DATABASE_URL=postgres://shop:secret@shop-db-g7h8i9:5432/shop"},
		},
		"postgres": {{"postgresId": "d1", "name": "db", "appName": "shop-db-g7h8i9", "environmentId": "e1",
			"dockerImage": "postgres:16", "databaseName": "shop", "externalPort": 5432}},
		"mount": {{"mountId": "m1", "type": "bind", "hostPath": "/home/ubuntu/shop/uploads", "applicationId": "a2"}},
		"domain": {
			{"domainId": "dm1", "host": "shop.example", "path": "/", "https": true, "certificateType": "letsencrypt", "applicationId": "a1", "domainType": "application"},
			{"domainId": "dm2", "host": "www.shop.example", "path": "/", "https": true, "certificateType": "letsencrypt", "applicationId": "a1", "domainType": "application"},
			{"domainId": "dm3", "host": "api.shop.example", "path": "/", "https": true, "certificateType": "letsencrypt", "applicationId": "a2", "domainType": "application"},
		},
		"redirect":     {{"redirectId": "r1", "applicationId": "a1", "regex": `^https?://www\.shop\.example(.*)$`, "replacement": "https://shop.example$1", "permanent": true}},
		"git_provider": {{"gitProviderId": "g1", "name": "shop-gitlab", "providerType": "gitlab"}, {"gitProviderId": "g2", "name": "shop-github", "providerType": "github"}},
	}
	return src
}
