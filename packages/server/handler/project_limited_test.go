package handler

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/middleware"
)

// A member granted one service, and not its project, opens the project limited
// to that service: the project and its service list show only it, the
// project's own lists are empty, and its project-wide views stay closed. A
// member granted nothing in it is refused, as before.
func TestAGrantInsideAProjectOpensItLimited(t *testing.T) {
	h, orgIDStr, adminCtx, memberCtx := setupClusterAuthz(t)
	ctx := adminCtx
	orgID := uuid.MustParse(orgIDStr)
	gdb := h.svc.DB

	project := db.Project{OrganizationID: orgID, Name: "Shop", Slug: "shop"}
	if err := gdb.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	mine := db.Service{ProjectID: project.ID, Name: "web", Slug: "web", Type: db.ServiceTypeApplication}
	other := db.Service{ProjectID: project.ID, Name: "admin", Slug: "admin", Type: db.ServiceTypeApplication}
	for _, s := range []*db.Service{&mine, &other} {
		if err := gdb.Create(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	memberID, _ := middleware.UserFromContext(memberCtx)
	pid := project.ID.String()

	// Nothing granted: refused.
	if _, err := h.GetProject(memberCtx, &ProjectPathInput{OrgID: orgIDStr, ProjectID: pid}); err == nil {
		t.Fatal("a member granted nothing in the project opened it")
	}

	if err := h.svc.Permissions.Grant(ctx, orgID, memberID, mine.ID, db.ResourceService, db.ActionDeploy); err != nil {
		t.Fatal(err)
	}
	got, err := h.GetProject(memberCtx, &ProjectPathInput{OrgID: orgIDStr, ProjectID: pid})
	if err != nil {
		t.Fatalf("a member granted a service in it could not open the project: %v", err)
	}
	if !got.Body.Limited || got.Body.ServicesCount != 1 || got.Body.RoutesCount != 0 {
		t.Errorf("the limited project: %+v", got.Body.ProjectCounts)
	}
	list, err := h.ListWorkloads(memberCtx, &ListWorkloadsInput{OrgID: orgIDStr, ProjectID: pid})
	if err != nil || len(list.Body) != 1 || list.Body[0].ID != mine.ID {
		t.Errorf("the services listed: %v %+v", err, list)
	}
	// The routes that lead to their service, and not the others.
	toMine := db.Route{OrganizationID: orgID, ProjectID: project.ID, Subdomain: "web", Hostname: "web.example.test", Targets: []db.RouteTarget{{ServiceID: &mine.ID, Path: "/"}}}
	toOther := db.Route{OrganizationID: orgID, ProjectID: project.ID, Subdomain: "admin", Hostname: "admin.example.test", Targets: []db.RouteTarget{{ServiceID: &other.ID, Path: "/"}}}
	for _, rt := range []*db.Route{&toMine, &toOther} {
		if err := gdb.Create(rt).Error; err != nil {
			t.Fatal(err)
		}
	}
	routes, err := h.ListRoutes(memberCtx, &ListRoutesInput{OrgID: orgIDStr, ProjectID: pid})
	if err != nil || len(routes.Body) != 1 || routes.Body[0].ID != toMine.ID {
		t.Errorf("a limited reader's routes: %v %d", err, len(routes.Body))
	}

	// Their service's page knows what they may do: view and deploy here, and
	// no route, since that is created in the project.
	if err := h.svc.Permissions.Grant(ctx, orgID, memberID, mine.ID, db.ResourceService, db.ActionView); err != nil {
		t.Fatal(err)
	}
	view, err := h.GetWorkload(memberCtx, &WorkloadPathInput{OrgID: orgIDStr, ProjectID: pid, ServiceID: mine.ID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(view.Body.Can, []db.ResourceAction{db.ActionView, db.ActionDeploy}) || view.Body.CanRoute {
		t.Errorf("what the member may do: %v, route %v", view.Body.Can, view.Body.CanRoute)
	}
	raw, _ := json.Marshal(view.Body)
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)
	if wire["name"] != "web" || wire["can"] == nil {
		t.Errorf("the service and what may be done to it, on the wire: %s", raw)
	}
	if _, err := h.GetProjectMap(memberCtx, &ProjectPathInput{OrgID: orgIDStr, ProjectID: pid}); err == nil {
		t.Error("the project's map opened for a limited reader")
	}

	// Its card on the projects list says so, and counts what they may open,
	// not the project's two services and two routes.
	listed, err := h.ListProjects(memberCtx, &ListProjectsInput{OrgID: orgIDStr})
	if err != nil || len(listed.Body) != 1 {
		t.Fatalf("the member's projects: %v %+v", err, listed)
	}
	if card := listed.Body[0]; !card.Limited || card.ServicesCount != 1 || card.RoutesCount != 0 {
		t.Errorf("the limited project's card: limited %v, %+v", card.Limited, card.ProjectCounts)
	}

	// A grant on the project itself opens it whole.
	if err := h.svc.Permissions.Grant(ctx, orgID, memberID, project.ID, db.ResourceProject, db.ActionView); err != nil {
		t.Fatal(err)
	}
	whole, err := h.GetProject(memberCtx, &ProjectPathInput{OrgID: orgIDStr, ProjectID: pid})
	if err != nil || whole.Body.Limited {
		t.Errorf("with view on the project it should open whole: %v", err)
	}
	all, _ := h.ListWorkloads(memberCtx, &ListWorkloadsInput{OrgID: orgIDStr, ProjectID: pid})
	if len(all.Body) != 2 {
		t.Errorf("with view on the project, every service: %d", len(all.Body))
	}
	if listed, _ := h.ListProjects(memberCtx, &ListProjectsInput{OrgID: orgIDStr}); listed.Body[0].Limited || listed.Body[0].ServicesCount != 2 {
		t.Errorf("with view on the project, its card is whole: %+v", listed.Body[0].ProjectCounts)
	}
}
