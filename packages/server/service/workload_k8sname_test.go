package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// A database's Deployment carries a random suffix, so its k8s name cannot be
// derived from the service name. Addressing it by the service name silently
// targets a workload that does not exist: stop reported success while the
// database kept serving, delete orphaned the Deployment and its PVC, and the
// status reconciler read the absence as failure and marked every database
// failed while its pod ran perfectly.
func TestK8sNameUsesTheDatabaseSlug(t *testing.T) {
	s := &WorkloadService{}
	ctx := context.Background()

	app := &db.Service{
		Base: db.Base{ID: uuid.New()},
		Name: "Auth UI",
		Type: db.ServiceTypeApplication,
	}
	if got, want := s.k8sName(ctx, app), "auth-ui"; got != want {
		t.Errorf("application: k8sName = %q, want %q", got, want)
	}

	dbSvc := &db.Service{
		Base:           db.Base{ID: uuid.New()},
		Name:           "umami-db",
		Type:           db.ServiceTypeDatabase,
		DatabaseConfig: &db.DatabaseConfig{Slug: "umami-db-5e9871"},
	}
	if got, want := s.k8sName(ctx, dbSvc), "umami-db-5e9871"; got != want {
		t.Errorf("database: k8sName = %q, want %q — the suffix is not derivable from the name", got, want)
	}
	if s.k8sName(ctx, dbSvc) == slugify(dbSvc.Name) {
		t.Error("database resolved to the service name, which is the bug this guards")
	}
}
