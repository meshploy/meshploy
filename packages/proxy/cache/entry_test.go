package cache

import (
	"testing"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// An entry carries whose route it is, so a gate needs no lookup of its own;
// a target that is no service leaves ServiceID zero.
func TestEntryCarriesItsOwners(t *testing.T) {
	route := &db.Route{OrganizationID: uuid.New(), ProjectID: uuid.New()}
	route.ID = uuid.New()
	svc := uuid.New()

	e := entryFor(db.RouteTarget{Path: "/", TargetIP: "100.64.0.2", TargetPort: 30080, ServiceID: &svc}, route)
	if e.RouteID != route.ID || e.ProjectID != route.ProjectID || e.OrgID != route.OrganizationID || e.ServiceID != svc {
		t.Fatalf("owners not carried: %+v", e)
	}
	if e.TargetIP != "100.64.0.2" || e.TargetPort != 30080 {
		t.Fatalf("target changed: %+v", e)
	}
	if e := entryFor(db.RouteTarget{Path: "/", TargetIP: "100.64.0.3", TargetPort: 22}, route); e.ServiceID != uuid.Nil {
		t.Fatalf("an address target named a service: %v", e.ServiceID)
	}
}
