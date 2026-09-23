package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// A git provider calls this gateway at an address it was told once, when the
// integration was set up: a GitHub App's webhook and callbacks, an OAuth app's
// redirect, the push hook on each repository. Those addresses live at the
// provider, not here, so removing the domain they name does not update them -
// pushes simply stop arriving, and connecting a repository fails, with nothing
// said. For a domain that is or was primary, they are a reason it cannot go yet.

// Kinds of provider-side registration.
const (
	RegistrationGitHubApp     = "github_app"
	RegistrationOAuthRedirect = "oauth_redirect"
	RegistrationPushHooks     = "push_hooks"
)

// IntegrationRegistration is one registration at a provider that still points
// at a domain.
type IntegrationRegistration struct {
	IntegrationID uuid.UUID `json:"integration_id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	Kind          string    `json:"kind"`
	// Automatic means Meshploy can change it through the provider's API.
	// Otherwise it is changed at the provider by hand, and then marked.
	Automatic bool `json:"automatic"`
	// Current and New are the addresses as registered now and as they should
	// be, one pair per URL the provider holds.
	URLs []RegistrationURL `json:"urls"`
	// Repos are the repositories whose push hooks are involved.
	Repos []string `json:"repos,omitempty"`
}

// RegistrationURL is one address a provider holds, and what it should become.
type RegistrationURL struct {
	Label   string `json:"label"`
	Current string `json:"current"`
	New     string `json:"new"`
}

// IntegrationsOnDomain lists the provider registrations still pointing at a
// domain. Only a domain that is or was primary serves api. and console., so
// only such a domain can have any.
func (s *DomainService) IntegrationsOnDomain(ctx context.Context, git *GitIntegrationService, orgID uuid.UUID, domain *db.Domain) ([]IntegrationRegistration, error) {
	out := make([]IntegrationRegistration, 0)
	if !domain.IsPrimary && !domain.FormerPrimary {
		return out, nil
	}
	var integrations []db.GitIntegration
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).
		Order("name ASC").Find(&integrations).Error; err != nil {
		return nil, err
	}
	api, console := "api."+domain.BaseDomain, "console."+domain.BaseDomain
	newAPI := git.apiBase(orgID)

	for i := range integrations {
		g := &integrations[i]
		ref := IntegrationRegistration{IntegrationID: g.ID, Name: g.Name, Provider: g.Provider}
		registered := git.registeredBase(g)

		switch {
		case g.Provider == "github" && g.AuthMethod == "app":
			if urlHost(registered) != api {
				continue
			}
			ref.Kind = RegistrationGitHubApp
			for _, u := range []struct{ label, path string }{
				{"Webhook URL", "/api/v1/webhooks/github/" + g.ID.String()},
				{"Callback URL", "/api/v1/github/callback"},
				{"Setup URL", "/api/v1/github/callback"},
			} {
				ref.URLs = append(ref.URLs, RegistrationURL{Label: u.label, Current: registered + u.path, New: newAPI + u.path})
			}
			out = append(out, ref)

		case g.Provider != "github":
			if g.AuthMethod == "oauth" {
				if h := urlHost(g.OAuthRedirectURI); h == api || h == console {
					oauth := ref
					oauth.Kind = RegistrationOAuthRedirect
					oauth.URLs = []RegistrationURL{{Label: "Redirect URI", Current: g.OAuthRedirectURI,
						New: rehost(g.OAuthRedirectURI, api, console, newAPI, git.consoleBase(orgID))}}
					out = append(out, oauth)
				}
			}
			if urlHost(registered) != api {
				continue
			}
			repos, err := s.autoDeployRepos(ctx, g.ID)
			if err != nil {
				return nil, err
			}
			if len(repos) == 0 {
				continue
			}
			hooks := ref
			hooks.Kind = RegistrationPushHooks
			hooks.Automatic = true
			hooks.Repos = repos
			hooks.URLs = []RegistrationURL{{Label: "Push webhook", Current: registered + pushHookPath(g), New: newAPI + pushHookPath(g)}}
			out = append(out, hooks)
		}
	}
	return out, nil
}

// autoDeployRepos are the repositories an integration keeps a push hook on:
// those a service deploys on push from.
func (s *DomainService) autoDeployRepos(ctx context.Context, integrationID uuid.UUID) ([]string, error) {
	var configs []db.BuildConfig
	if err := s.db.WithContext(ctx).
		Where("git_integration_id = ? AND auto_deploy", integrationID).Find(&configs).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var repos []string
	for _, c := range configs {
		key := normalizeRepoPath(c.GitRepo)
		if c.GitRepo == "" || seen[key] {
			continue
		}
		seen[key] = true
		repos = append(repos, c.GitRepo)
	}
	sort.Strings(repos)
	return repos, nil
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// rehost moves a URL on the retiring domain's api. or console. name to the same
// name on the primary, keeping its path.
func rehost(raw, oldAPI, oldConsole, newAPI, newConsole string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base := newAPI
	if strings.EqualFold(u.Hostname(), oldConsole) {
		base = newConsole
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	u.Scheme, u.Host = b.Scheme, b.Host
	return u.String()
}

// HookMoveFailure is a repository whose hook could not be moved.
type HookMoveFailure struct {
	Repo  string `json:"repo"`
	Error string `json:"error"`
}

// HookMoveResult is what moving an integration's push hooks did.
type HookMoveResult struct {
	Moved  []string          `json:"moved"`
	Failed []HookMoveFailure `json:"failed"`
}

// MovePushHooks moves an integration's repository push hooks to the primary's
// API address, through the provider's API.
//
// For each repository: add a hook at the new address unless one is there, then
// delete this integration's hooks at any other address. In that order, so a
// repository is never without one - a push in between is delivered twice at
// worst, and deploys are idempotent to a commit. Best effort per repository:
// a token without the scope to manage hooks is common, and one refusal does not
// stop the rest. Only when every repository moved is the integration recorded
// as registered at the new address.
func (s *GitIntegrationService) MovePushHooks(ctx context.Context, orgID, integrationID uuid.UUID) (*HookMoveResult, error) {
	var g db.GitIntegration
	if err := s.db.WithContext(ctx).First(&g, "id = ? AND organization_id = ?", integrationID, orgID).Error; err != nil {
		return nil, huma.Error404NotFound("git integration not found")
	}
	repos, err := s.domains.autoDeployRepos(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if g.Provider == "github" {
		return nil, huma.Error422UnprocessableEntity("a GitHub App has no repository hooks to move - its webhook is set in the App's settings")
	}
	secret := string(g.WebhookSecret)
	if secret == "" {
		return nil, huma.Error422UnprocessableEntity("this integration has no webhook secret; reconnect it first")
	}
	token, err := s.resolveOAuthToken(ctx, &g, false)
	if err != nil {
		return nil, err
	}

	target := s.PushHookURL(&g)
	res := &HookMoveResult{Moved: []string{}, Failed: []HookMoveFailure{}}
	for _, repo := range repos {
		if err := s.moveRepoHook(&g, token, repo, target, secret); err != nil {
			res.Failed = append(res.Failed, HookMoveFailure{Repo: repo, Error: err.Error()})
			continue
		}
		res.Moved = append(res.Moved, repo)
	}
	if len(res.Failed) == 0 {
		if err := s.db.WithContext(ctx).Model(&g).Updates(db.GitIntegration{RegisteredAPIBase: s.apiBase(orgID)}).Error; err != nil {
			return nil, err
		}
	}
	return res, nil
}

func (s *GitIntegrationService) moveRepoHook(g *db.GitIntegration, token, repo, target, secret string) error {
	hooks, err := s.listPushHooks(g, token, repo)
	if err != nil {
		return err
	}
	have := false
	for _, h := range hooks {
		if h.URL == target {
			have = true
		}
	}
	if !have {
		if err := s.createPushHook(g, token, repo, target, secret); err != nil {
			return err
		}
	}
	for _, h := range hooks {
		if h.URL == target || !isOurHook(g, h.URL) {
			continue
		}
		if err := gitAPI(http.MethodDelete, s.hookURL(g, repo, h.ID), hookAuthScheme(g.Provider), token, nil, nil); err != nil {
			return fmt.Errorf("added the new hook, but could not delete the old one: %w", err)
		}
	}
	return nil
}

// MarkRegistrationUpdated records a change the operator made at the provider.
//
// It records, it does not change the provider, and it cannot check - with one
// real effect for an OAuth redirect: Meshploy starts sending the new redirect
// URI, which the provider rejects unless it was changed there first.
func (s *GitIntegrationService) MarkRegistrationUpdated(ctx context.Context, orgID, integrationID uuid.UUID, kind string, oldDomain string) (*db.GitIntegration, error) {
	var g db.GitIntegration
	if err := s.db.WithContext(ctx).First(&g, "id = ? AND organization_id = ?", integrationID, orgID).Error; err != nil {
		return nil, huma.Error404NotFound("git integration not found")
	}
	switch kind {
	case RegistrationGitHubApp, RegistrationPushHooks:
		g.RegisteredAPIBase = s.apiBase(orgID)
		return &g, s.db.WithContext(ctx).Model(&g).Updates(db.GitIntegration{RegisteredAPIBase: g.RegisteredAPIBase}).Error
	case RegistrationOAuthRedirect:
		if g.AuthMethod != "oauth" {
			return nil, huma.Error422UnprocessableEntity("this integration does not sign in with OAuth")
		}
		g.OAuthRedirectURI = rehost(g.OAuthRedirectURI, "api."+oldDomain, "console."+oldDomain, s.apiBase(orgID), s.consoleBase(orgID))
		return &g, s.db.WithContext(ctx).Model(&g).Updates(db.GitIntegration{OAuthRedirectURI: g.OAuthRedirectURI}).Error
	}
	return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("unknown registration kind %q", kind))
}

// errIntegrationsOnDomain explains why a domain providers still call cannot go.
func errIntegrationsOnDomain(n int, domain string) error {
	return huma.Error422UnprocessableEntity(fmt.Sprintf(
		"%d git integration registration(s) still point at %s - pushes and sign-ins through them would stop when it goes",
		n, domain))
}
