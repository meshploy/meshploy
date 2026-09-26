package handler

import (
	"context"
	"net/http"
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
