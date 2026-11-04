package proxy

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/cache"
	"github.com/meshploy/packages/proxy/meshgate"
	"github.com/meshploy/packages/proxy/saved"
	"github.com/meshploy/packages/proxy/tcp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// What the edge loads from the database it saves, and an edge started while
// the database is down serves it: the route, who on the mesh may open it, and
// the TCP port.
func TestTheEdgeServesWhatItSavedWithoutTheDatabase(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	root.Exec("DROP DATABASE IF EXISTS edge_saved_test")
	if err := root.Exec("CREATE DATABASE edge_saved_test").Error; err != nil {
		t.Fatal(err)
	}
	base, _, _ := strings.Cut(dsn, "?")
	base = base[:strings.LastIndex(base, "/")+1]
	d, err := gorm.Open(postgres.Open(base+"edge_saved_test?sslmode=disable"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sql, _ := d.DB(); sql != nil {
			sql.Close()
		}
		root.Exec("DROP DATABASE IF EXISTS edge_saved_test")
	})
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	org := db.Organization{Name: "Acme", Slug: "acme"}
	must(d.Create(&org).Error)
	member := db.User{Username: "mia", Email: "mia@acme.dev"}
	must(d.Create(&member).Error)
	must(d.Create(&db.OrganizationMember{OrganizationID: org.ID, UserID: member.ID, Role: db.RoleMember}).Error)
	project := db.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop"}
	must(d.Create(&project).Error)
	app := db.Service{ProjectID: project.ID, Name: "web", Type: db.ServiceTypeApplication}
	must(d.Create(&app).Error)
	must(d.Create(&db.Node{OrganizationID: org.ID, Name: "mia-laptop", TailscaleIP: "100.64.0.5", MeshRole: db.MeshRoleMesh, OwnerID: &member.ID}).Error)
	must(d.Create(&db.MeshPolicyState{ID: 1, Enforced: true}).Error)
	must(d.Create(&db.ResourcePermission{OrganizationID: org.ID, UserID: member.ID, ResourceType: db.ResourceService,
		ResourceID: app.ID, Action: db.ActionView}).Error)
	route := db.Route{OrganizationID: org.ID, ProjectID: project.ID, Hostname: "shop.internal.acme.dev", Zone: db.RouteZoneInternal, Published: true}
	must(d.Create(&route).Error)
	must(d.Create(&db.RouteTarget{RouteID: route.ID, Path: "/", ServiceID: &app.ID, TargetIP: "100.64.0.2", TargetPort: 30080}).Error)

	// A free port for the TCP route, on loopback.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	must(d.Create(&db.TCPRoute{OrganizationID: org.ID, ProjectID: project.ID, GatewayPort: port, TargetIP: "127.0.0.1", TargetPort: 1,
		Zone: db.TCPZoneLocal, Published: true}).Error)

	dir, err := saved.Open(filepath.Join(t.TempDir(), "edge"))
	must(err)

	// The edge as it runs with its database: everything loads, and is saved.
	routes := cache.New(d, time.Hour)
	routes.Keep(dir)
	routes.Start()
	gate := meshgate.New(d)
	gate.Keep(dir)
	gate.Start(time.Hour)
	ports := tcp.New(d, "", time.Hour)
	ports.Keep(dir)
	ports.Start()
	if _, ok := routes.Get("shop.internal.acme.dev", "/"); !ok {
		t.Fatal("the route did not load")
	}
	for _, name := range []string{"routes", "mesh", "tcp"} {
		if _, err := os.Stat(filepath.Join(dir.Path(), name+".json")); err != nil {
			t.Errorf("%s was not saved: %v", name, err)
		}
	}
	// The database goes away, and the edge with it.
	if sql, _ := d.DB(); sql != nil {
		sql.Close()
	}
	ports.Stop()

	// The same edge restarted with its database down.
	down, err := gorm.Open(postgres.Open("postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	must(err)
	routes = cache.New(down, time.Hour)
	routes.Keep(dir)
	routes.Start()
	gate = meshgate.New(down)
	gate.Keep(dir)
	gate.Start(time.Hour)
	ports = tcp.New(down, "", time.Hour)
	ports.Keep(dir)
	ports.Start()
	defer ports.Stop()

	entry, ok := routes.Get("shop.internal.acme.dev", "/")
	if !ok || entry.TargetPort != 30080 || !entry.Internal {
		t.Fatalf("saved route: %+v %v", entry, ok)
	}
	target := meshgate.Target{RouteID: entry.RouteID, ServiceID: entry.ServiceID, ProjectID: entry.ProjectID, OrgID: entry.OrgID}
	if v := gate.Check("100.64.0.5", target); !v.Allowed {
		t.Errorf("the member granted the service was refused: %s", v.Reason)
	}
	if v := gate.Check("100.64.0.9", target); v.Allowed {
		t.Errorf("an unknown machine was let in: %s", v.Reason)
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatalf("the saved TCP port is not open: %v", err)
	}
	conn.Close()
}
