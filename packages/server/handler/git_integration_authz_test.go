package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
)

// An integration id names one org's credentials. Being an admin or member of
// the org in the path must not reach another org's: its repositories and
// branches, its push-hook secret, deleting it, or building with it.
//
// Regression guard: these routes checked the caller's role in the path's org
// and then used the id as given.
func TestAnotherOrgsGitIntegrationIsNotFound(t *testing.T) {
	ctx := context.Background()
	database := newAuthzTestDB(t)
	svc := service.New(database)
	h := New(nil, svc)

	register := func(name string) (uuid.UUID, uuid.UUID) {
		u, err := svc.Auth.Register(ctx, service.RegisterInput{Username: name, Email: name + "@example.com", Password: "password123"})
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		orgs, err := svc.Orgs.ListForUser(ctx, u.ID)
		if err != nil || len(orgs) != 1 {
			t.Fatalf("orgs of %s: %v", name, err)
		}
		return u.ID, orgs[0].ID
	}
	attacker, attackerOrg := register("attacker")
	// Another org on the same instance, as EE or Cloud has.
	victim := db.Organization{Name: "Victim", Slug: "victim"}
	if err := database.Create(&victim).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	victimOrg := victim.ID

	victims := db.GitIntegration{OrganizationID: victimOrg, Provider: "gitea", AuthMethod: "pat", Name: "victim", BaseURL: "https://git.example.com"}
	if err := database.Create(&victims).Error; err != nil {
		t.Fatalf("create integration: %v", err)
	}
	own := db.GitIntegration{OrganizationID: attackerOrg, Provider: "gitea", AuthMethod: "pat", Name: "own", BaseURL: "https://git.example.com"}
	if err := database.Create(&own).Error; err != nil {
		t.Fatalf("create integration: %v", err)
	}
	project, err := svc.Projects.Create(ctx, attackerOrg, "App", "app")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	_, api := humatest.New(t)
	api.UseMiddleware(func(hc huma.Context, next func(huma.Context)) {
		next(huma.WithContext(hc, middleware.ContextWithUser(hc.Context(), attacker)))
	})
	h.registerGitIntegrationRoutes(api)
	h.registerWorkloadRoutes(api)
	h.registerStackRoutes(api)

	base := "/api/v1/orgs/" + attackerOrg.String()
	gi := base + "/git-integrations/" + victims.ID.String()
	for _, c := range []struct {
		name string
		call func() int
	}{
		{"repos", func() int { return api.Get(gi + "/repos").Code }},
		{"branches", func() int { return api.Get(gi + "/branches?repo=victim/app").Code }},
		{"install url", func() int { return api.Get(gi + "/install-url").Code }},
		{"oauth reconnect", func() int { return api.Get(gi + "/oauth-reconnect").Code }},
		{"push hook secret", func() int { return api.Get(gi + "/push-hook").Code }},
		{"install push hook", func() int { return api.Post(gi+"/push-hook", map[string]any{"repo": "victim/app"}).Code }},
		{"delete", func() int { return api.Delete(gi).Code }},
		{"detect", func() int {
			return api.Post(base+"/detect-stack", map[string]any{"git_integration_id": victims.ID.String(), "repo": "victim/app", "branch": "main"}).Code
		}},
		{"create service", func() int {
			return api.Post(base+"/projects/"+project.ID.String()+"/services", map[string]any{
				"name": "stolen", "git_integration_id": victims.ID.String(), "git_repo": "victim/app", "branch": "main"}).Code
		}},
		{"create stack", func() int {
			return api.Post(base+"/projects/"+project.ID.String()+"/stacks", map[string]any{
				"name": "stolen", "spec": "", "git_mode": "repo", "git_integration_id": victims.ID.String(), "git_repo": "victim/app", "git_branch": "main", "git_path": "compose.yml"}).Code
		}},
	} {
		if code := c.call(); code != http.StatusNotFound {
			t.Errorf("%s with another org's integration: got %d, want 404", c.name, code)
		}
	}

	var n int64
	database.Model(&db.GitIntegration{}).Where("id = ?", victims.ID).Count(&n)
	if n != 1 {
		t.Fatal("the other org's integration was deleted")
	}
	if code := api.Delete(base + "/git-integrations/" + own.ID.String()).Code; code >= 300 {
		t.Fatalf("deleting the org's own integration: %d", code)
	}
}

// A connection reserved for one project is another project's member's to
// neither see nor use: absent from their list, not found by its id, and
// refused as a service's source in their own project. Their own project's
// is listed for it.
func TestAGitConnectionReservedForAnotherProject(t *testing.T) {
	ctx := context.Background()
	database := newAuthzTestDB(t)
	svc := service.New(database)
	h := New(nil, svc)

	owner, err := svc.Auth.Register(ctx, service.RegisterInput{Username: "owner", Email: "owner@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	orgs, err := svc.Orgs.ListForUser(ctx, owner.ID)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("orgs: %v", err)
	}
	org := orgs[0].ID
	ann, err := svc.Projects.Create(ctx, org, "Ann", "ann")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	bo, err := svc.Projects.Create(ctx, org, "Bo", "bo")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	member := db.User{Username: "bo", Email: "bo@example.com"}
	rows := []any{&member}
	if err := database.Create(&member).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	rows = []any{&db.OrganizationMember{OrganizationID: org, UserID: member.ID, Role: db.RoleMember}}
	for _, a := range []db.ResourceAction{db.ActionView, db.ActionCreate, db.ActionUpdate} {
		rows = append(rows, &db.ResourcePermission{OrganizationID: org, UserID: member.ID,
			ResourceType: db.ResourceProject, ResourceID: bo.ID, Action: a})
	}
	annsConn := db.GitIntegration{OrganizationID: org, Provider: "gitea", AuthMethod: "pat", Name: "ann's", BaseURL: "https://git.example.com", ProjectID: &ann.ID}
	rows = append(rows, &annsConn)
	for _, r := range rows {
		if err := database.Create(r).Error; err != nil {
			t.Fatalf("create %T: %v", r, err)
		}
	}

	_, api := humatest.New(t)
	api.UseMiddleware(func(hc huma.Context, next func(huma.Context)) {
		next(huma.WithContext(hc, middleware.ContextWithUser(hc.Context(), member.ID)))
	})
	h.registerGitIntegrationRoutes(api)
	h.registerWorkloadRoutes(api)

	base := "/api/v1/orgs/" + org.String()
	if body := api.Get(base + "/git-integrations").Body.String(); strings.Contains(body, annsConn.ID.String()) {
		t.Error("another project's connection is listed")
	}
	gi := base + "/git-integrations/" + annsConn.ID.String()
	for _, c := range []struct {
		name string
		call func() int
		want int
	}{
		{"repos", func() int { return api.Get(gi + "/repos").Code }, http.StatusNotFound},
		{"branches", func() int { return api.Get(gi + "/branches?repo=ann/app").Code }, http.StatusNotFound},
		{"install url", func() int { return api.Get(gi + "/install-url").Code }, http.StatusNotFound},
		{"delete", func() int { return api.Delete(gi).Code }, http.StatusNotFound},
		{"detect", func() int {
			return api.Post(base+"/detect-stack", map[string]any{"git_integration_id": annsConn.ID.String(), "repo": "ann/app", "branch": "main"}).Code
		}, http.StatusNotFound},
		{"service in their own project", func() int {
			return api.Post(base+"/projects/"+bo.ID.String()+"/services", map[string]any{
				"name": "borrowed", "git_integration_id": annsConn.ID.String(), "git_repo": "ann/app", "branch": "main"}).Code
		}, http.StatusUnprocessableEntity},
	} {
		if code := c.call(); code != c.want {
			t.Errorf("%s with another project's connection: got %d, want %d", c.name, code, c.want)
		}
	}

	if code := api.Post(base+"/git-integrations/github", map[string]any{"project_id": ann.ID.String()}).Code; code < 400 {
		t.Errorf("reserving one for a project they may not add to: got %d, want refused", code)
	}
	if code := api.Post(base+"/git-integrations/github", map[string]any{}).Code; code != http.StatusForbidden {
		t.Errorf("the organization's own, as a member: got %d, want 403", code)
	}
	mine := db.GitIntegration{OrganizationID: org, Provider: "gitea", AuthMethod: "pat", Name: "bo's", BaseURL: "https://git.example.com", ProjectID: &bo.ID}
	if err := database.Create(&mine).Error; err != nil {
		t.Fatalf("their connection: %v", err)
	}
	if body := api.Get(base + "/git-integrations?project=" + bo.ID.String()).Body.String(); !strings.Contains(body, mine.ID.String()) {
		t.Error("their own connection is not listed for their project")
	}
}
