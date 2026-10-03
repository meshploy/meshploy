package meshgate

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// testDB is a fresh database beside the one DATABASE_URL names; the test is
// skipped without one.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	root.Exec("DROP DATABASE IF EXISTS meshgate_test")
	if err := root.Exec("CREATE DATABASE meshgate_test").Error; err != nil {
		t.Fatal(err)
	}
	base, _, _ := strings.Cut(dsn, "?")
	base = base[:strings.LastIndex(base, "/")+1]
	database, err := gorm.Open(postgres.Open(base+"meshgate_test?sslmode=disable"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sql, _ := database.DB(); sql != nil {
			sql.Close()
		}
		root.Exec("DROP DATABASE IF EXISTS meshgate_test")
	})
	return database
}

// Load reads what Decide needs from the tables the API writes.
func TestLoadReadsTheDatabase(t *testing.T) {
	d := testDB(t)
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
	level := db.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop-staging", ParentProjectID: &project.ID, EnvLevel: 1}
	must(d.Create(&level).Error)
	app := db.Service{ProjectID: project.ID, Name: "web", Type: db.ServiceTypeApplication}
	must(d.Create(&app).Error)
	laptop := db.Node{OrganizationID: org.ID, Name: "mia-laptop", TailscaleIP: "100.64.0.5", MeshRole: db.MeshRoleMesh, OwnerID: &member.ID}
	must(d.Create(&laptop).Error)

	target := Target{RouteID: uuid.New(), ServiceID: app.ID, ProjectID: project.ID, OrgID: org.ID}
	s, err := Load(d)
	must(err)
	if s.Enforced {
		t.Fatal("enforced before anyone switched it on")
	}
	must(d.Create(&db.MeshPolicyState{ID: 1, Enforced: true}).Error)
	s, err = Load(d)
	must(err)
	if got := s.Decide("100.64.0.5", target); got.Allowed || got.Machine != "mia-laptop" {
		t.Fatalf("before the grant: %+v", got)
	}

	must(d.Create(&db.ResourcePermission{OrganizationID: org.ID, UserID: member.ID, ResourceType: db.ResourceProject,
		ResourceID: project.ID, Action: db.ActionView}).Error)
	s, err = Load(d)
	must(err)
	if got := s.Decide("100.64.0.5", target); !got.Allowed {
		t.Fatalf("granted the project: %+v", got)
	}
	if got := s.Decide("100.64.0.5", Target{RouteID: uuid.New(), ProjectID: level.ID, OrgID: org.ID}); !got.Allowed {
		t.Fatalf("a route in the project's level: %+v", got)
	}

	must(d.Create(&db.MeshReach{OrganizationID: org.ID, UserID: member.ID, ResourceType: db.ResourceService,
		ResourceID: app.ID, Reach: false}).Error)
	s, err = Load(d)
	must(err)
	if got := s.Decide("100.64.0.5", target); got.Allowed {
		t.Fatalf("switched off from their machines: %+v", got)
	}
}
