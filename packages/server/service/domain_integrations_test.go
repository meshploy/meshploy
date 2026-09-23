package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGitLabHooks keeps repository hooks the way GitLab's API does: listed,
// created and deleted per project, addressed by the full project path.
type fakeGitLabHooks struct {
	mu    sync.Mutex
	next  int
	hooks map[string]map[string]string // project -> id -> url
	deny  map[string]bool              // projects whose hooks cannot be managed
}

func newFakeGitLab(t *testing.T) (*fakeGitLabHooks, *httptest.Server) {
	t.Helper()
	f := &fakeGitLabHooks{hooks: map[string]map[string]string{}, deny: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		path := r.URL.EscapedPath() // keeps the project's encoded slashes
		rest, ok := strings.CutPrefix(path, "/api/v4/projects/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		parts := strings.SplitN(rest, "/hooks", 2)
		project := strings.ReplaceAll(parts[0], "%2F", "/")
		if f.deny[project] {
			http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
			return
		}
		if f.hooks[project] == nil {
			f.hooks[project] = map[string]string{}
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			list := []map[string]any{}
			for id, u := range f.hooks[project] {
				n, _ := strconv.Atoi(id)
				list = append(list, map[string]any{"id": n, "url": u})
			}
			_ = json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.next++
			id := strconv.Itoa(f.next)
			f.hooks[project][id] = body["url"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": f.next})
		case r.Method == http.MethodDelete:
			delete(f.hooks[project], strings.TrimPrefix(parts[1], "/"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeGitLabHooks) urls(project string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, u := range f.hooks[project] {
		out = append(out, u)
	}
	return out
}

// gitlabOn makes a GitLab integration that registered its hooks on the given
// API base, with a service deploying on push from each repository.
func (e primaryEnv) gitlabOn(t *testing.T, baseURL, registered string, repos ...string) *meshdb.GitIntegration {
	t.Helper()
	g := &meshdb.GitIntegration{
		OrganizationID: e.org, Provider: "gitlab", AuthMethod: "pat", Name: "gitlab",
		BaseURL: baseURL, InstallationID: "glpat-test", WebhookSecret: "hook-secret",
		RegisteredAPIBase: registered,
	}
	require.NoError(t, e.gdb.Create(g).Error)
	project := &meshdb.Project{OrganizationID: e.org, Name: "p", Slug: "p-" + uuid.NewString()[:6]}
	require.NoError(t, e.gdb.Create(project).Error)
	for _, repo := range repos {
		svc := &meshdb.Service{ProjectID: project.ID, Name: strings.ReplaceAll(repo, "/", "-")}
		require.NoError(t, e.gdb.Create(svc).Error)
		require.NoError(t, e.gdb.Create(&meshdb.BuildConfig{
			ServiceID: svc.ID, GitIntegrationID: &g.ID, GitRepo: repo, AutoDeploy: true,
		}).Error)
	}
	return g
}

// A git provider calls the address it was given once. Remove the domain it
// names and pushes stop arriving with nothing said, so it holds the domain.
func TestAFormerPrimaryStaysWhileAProviderStillCallsIt(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	gl, srv := newFakeGitLab(t)
	g := e.gitlabOn(t, srv.URL, "https://api.old.test", "acme/shop", "acme/blog")
	gl.hooks["acme/shop"] = map[string]string{"101": "https://api.old.test/api/v1/webhooks/git/gitlab/" + g.ID.String()}
	gl.hooks["acme/blog"] = map[string]string{"102": "https://api.old.test/api/v1/webhooks/git/gitlab/" + g.ID.String()}

	_, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)
	old, _ := e.svc.Domains.Get(ctx, e.old.ID)

	regs, err := e.svc.Domains.IntegrationsOnDomain(ctx, e.svc.GitIntegrations, e.org, old)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	assert.Equal(t, service.RegistrationPushHooks, regs[0].Kind)
	assert.True(t, regs[0].Automatic)
	assert.Equal(t, []string{"acme/blog", "acme/shop"}, regs[0].Repos)
	assert.Equal(t, "https://api.new.test/api/v1/webhooks/git/gitlab/"+g.ID.String(), regs[0].URLs[0].New)

	_, err = e.svc.Domains.StartRetiring(ctx, e.old.ID)
	require.NoError(t, err)
	assert.ErrorContains(t, e.svc.Domains.Delete(ctx, e.old.ID), "git integration registration(s) still point at old.test")

	// Moved through the provider's API: a hook at the new address, none left at
	// the old, and the integration recorded as registered there.
	res, err := e.svc.GitIntegrations.MovePushHooks(ctx, e.org, g.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"acme/shop", "acme/blog"}, res.Moved)
	assert.Empty(t, res.Failed)
	for _, repo := range []string{"acme/shop", "acme/blog"} {
		assert.Equal(t, []string{"https://api.new.test/api/v1/webhooks/git/gitlab/" + g.ID.String()}, gl.urls(repo))
	}
	regs, err = e.svc.Domains.IntegrationsOnDomain(ctx, e.svc.GitIntegrations, e.org, old)
	require.NoError(t, err)
	assert.Empty(t, regs)
	require.NoError(t, e.svc.Domains.Delete(ctx, e.old.ID))
}

// One repository refusing does not stop the rest, and the integration is not
// recorded as moved until every one has.
func TestMovingHooksIsPerRepositoryAndOnlyCompleteMovesCount(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	gl, srv := newFakeGitLab(t)
	g := e.gitlabOn(t, srv.URL, "https://api.old.test", "acme/shop", "acme/locked")
	gl.deny["acme/locked"] = true
	_, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)

	res, err := e.svc.GitIntegrations.MovePushHooks(ctx, e.org, g.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme/shop"}, res.Moved)
	require.Len(t, res.Failed, 1)
	assert.Equal(t, "acme/locked", res.Failed[0].Repo)

	old, _ := e.svc.Domains.Get(ctx, e.old.ID)
	regs, err := e.svc.Domains.IntegrationsOnDomain(ctx, e.svc.GitIntegrations, e.org, old)
	require.NoError(t, err)
	assert.Len(t, regs, 1, "a partial move still holds the domain")

	// The operator fixes the last one by hand and says so.
	_, err = e.svc.GitIntegrations.MarkRegistrationUpdated(ctx, e.org, g.ID, service.RegistrationPushHooks, "old.test")
	require.NoError(t, err)
	regs, _ = e.svc.Domains.IntegrationsOnDomain(ctx, e.svc.GitIntegrations, e.org, old)
	assert.Empty(t, regs)
}

// A GitHub App's callbacks can only be changed in the App's settings, and an
// OAuth app's redirect only at the provider. Both are listed with the exact new
// values, and marking the OAuth one changes what Meshploy sends.
func TestManualRegistrationsAreListedWithTheirNewValues(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	app := &meshdb.GitIntegration{OrganizationID: e.org, Provider: "github", AuthMethod: "app", Name: "gh"}
	require.NoError(t, e.gdb.Create(app).Error) // before the field: registered the install's API_BASE_URL
	oauth := &meshdb.GitIntegration{OrganizationID: e.org, Provider: "gitea", AuthMethod: "oauth", Name: "gitea",
		OAuthRedirectURI: "https://api.old.test/api/v1/gitea/callback", RegisteredAPIBase: "https://api.old.test"}
	require.NoError(t, e.gdb.Create(oauth).Error)

	_, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)
	old, _ := e.svc.Domains.Get(ctx, e.old.ID)
	regs, err := e.svc.Domains.IntegrationsOnDomain(ctx, e.svc.GitIntegrations, e.org, old)
	require.NoError(t, err)

	kinds := map[string]service.IntegrationRegistration{}
	for _, r := range regs {
		kinds[r.Kind] = r
	}
	require.Contains(t, kinds, service.RegistrationGitHubApp)
	require.Contains(t, kinds, service.RegistrationOAuthRedirect)
	assert.False(t, kinds[service.RegistrationGitHubApp].Automatic)
	assert.Equal(t, "https://api.new.test/api/v1/webhooks/github/"+app.ID.String(), kinds[service.RegistrationGitHubApp].URLs[0].New)
	assert.Equal(t, "https://api.new.test/api/v1/gitea/callback", kinds[service.RegistrationOAuthRedirect].URLs[0].New)

	_, err = e.svc.GitIntegrations.MarkRegistrationUpdated(ctx, e.org, oauth.ID, service.RegistrationOAuthRedirect, "old.test")
	require.NoError(t, err)
	var stored meshdb.GitIntegration
	require.NoError(t, e.gdb.First(&stored, "id = ?", oauth.ID).Error)
	assert.Equal(t, "https://api.new.test/api/v1/gitea/callback", stored.OAuthRedirectURI,
		"Meshploy now sends the new redirect, which is why this is only marked once the provider has it")
}

// A hook made under an earlier primary is still ours. Seeing only the new
// address as ours would add a second hook beside it, and deleting a service
// would then remove neither.
func TestAnExistingHookUnderAnOldAddressIsNotDuplicated(t *testing.T) {
	ctx := context.Background()
	e := newPrimaryEnv(t)
	gl, srv := newFakeGitLab(t)
	g := e.gitlabOn(t, srv.URL, "https://api.old.test", "acme/shop")
	gl.hooks["acme/shop"] = map[string]string{"7": "https://api.old.test/api/v1/webhooks/git/gitlab/" + g.ID.String()}
	_, err := e.svc.Domains.SetPrimary(ctx, e.new.ID)
	require.NoError(t, err)

	require.NoError(t, e.svc.GitIntegrations.EnsurePushHook(ctx, g.ID, "acme/shop"))
	assert.Len(t, gl.urls("acme/shop"), 1, "the existing hook still delivers; it is moved as its own step")
}
