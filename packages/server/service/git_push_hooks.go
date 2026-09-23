package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// Deploy on push, for the providers that keep hooks per repository.
//
// GitHub needs none of this: its App is installed once and delivers every push
// from every repository it can see. GitLab, Gitea/Forgejo and Bitbucket hold a
// hook on each repository, so one has to be created the first time a service
// built from that repository is set to deploy on push.
//
// Best effort, deliberately. A token without the scope to manage hooks is
// common - GitLab needs Maintainer on the project, Bitbucket needs
// write:webhook - and that must not stop anyone saving a build configuration.
// When it fails the console shows the URL and secret to paste in by hand, and
// the receiver treats a hand-made hook exactly like one made here.

// PushHookURL is where this integration's providers should deliver pushes: the
// primary domain's API. New hooks are created there; existing ones keep
// whatever address they were created with until they are moved.
func (s *GitIntegrationService) PushHookURL(integration *db.GitIntegration) string {
	return s.apiBase(integration.OrganizationID) + pushHookPath(integration)
}

// pushHookPath is what identifies one of this integration's hooks, whatever
// host it was created on.
//
// Hooks used to be matched by their whole URL. Once the primary can move, the
// URL a hook should have changes while the hooks already on a repository do
// not - so an exact match would take an existing hook for someone else's,
// create a second one beside it, and later delete neither. The path names the
// integration; the host only says which domain it was made under.
func pushHookPath(integration *db.GitIntegration) string {
	return "/api/v1/webhooks/git/" + integration.Provider + "/" + integration.ID.String()
}

// isOurHook reports whether a hook on a repository is this integration's.
func isOurHook(integration *db.GitIntegration, hookURL string) bool {
	u, err := url.Parse(hookURL)
	if err != nil {
		return false
	}
	return strings.TrimRight(u.Path, "/") == pushHookPath(integration)
}

// errNoHookScope is what a provider refusing to list or create hooks comes back
// as, so the caller can tell "you cannot do this" from "this went wrong".
var errNoHookScope = errors.New("this token cannot manage repository webhooks")

// PushHook is what the console shows about one repository's push webhook: where
// deliveries go, what proves them, and whether the hook is actually there.
type PushHook struct {
	// Provider is what this integration connects to, because what the console
	// can offer differs: a repository webhook to add, or a GitHub App to
	// install on one more repository.
	Provider string `json:"provider"`
	URL      string `json:"url"`
	// Secret is shown because it has to be typed into the provider. It is the
	// integration's, not one repository's.
	Secret string `json:"secret"`
	// Field names where that secret goes, which differ enough between
	// providers to be worth saying.
	SecretField string `json:"secret_field"`
	Event       string `json:"event"`

	// State is what was found on the repository, when one was asked about:
	//
	//	installed   - the hook is there, pushes arrive
	//	missing     - it is not there, and this connection could add it
	//	no_access   - this connection cannot manage the repository's webhooks,
	//	              so it has to be added by hand
	//	unreachable - the provider could not be asked
	//	unknown     - no repository was named
	State string `json:"state"`
	// Reason carries the provider's own words when State is not installed.
	Reason string `json:"reason,omitempty"`
}

// Hook states.
const (
	HookInstalled   = "installed"
	HookMissing     = "missing"
	HookNoAccess    = "no_access"
	HookUnreachable = "unreachable"
	HookUnknown     = "unknown"
)

// PushHookDetails returns them for one integration, and - when a repository is
// named - whether the hook is on it. GitHub has none: its App delivers pushes
// without a per-repository hook.
func (s *GitIntegrationService) PushHookDetails(ctx context.Context, integrationID uuid.UUID, repo string) (*PushHook, error) {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return nil, fmt.Errorf("git integration not found")
	}
	out := &PushHook{
		Provider: integration.Provider,
		URL:      s.PushHookURL(&integration),
		Secret:   string(integration.WebhookSecret),
		State:    HookUnknown,
	}
	switch integration.Provider {
	case "github":
		// A GitHub App needs no per-repository hook, but it has the same
		// failure in a different shape: a repository outside the installation
		// never reports a push, and nothing said so. There is no URL or secret
		// to hand out here - the App holds both - so only the state is filled.
		out.URL, out.Secret = "", ""
		if repo != "" {
			s.fillGitHubInstallState(ctx, out, &integration, repo)
		}
		return out, nil
	case "gitlab":
		out.SecretField, out.Event = "Secret token", "Push events"
	case "gitea":
		out.SecretField, out.Event = "Secret", "Push"
	case "bitbucket":
		out.SecretField, out.Event = "Secret", "Repository push"
	}
	if repo != "" {
		s.fillHookState(ctx, out, &integration, repo)
	}
	return out, nil
}

// fillHookState asks the provider whether the hook is on the repository. Every
// way this can fail is a state rather than an error: the console still has
// something useful to show, which is the URL and secret to add by hand.
func (s *GitIntegrationService) fillHookState(ctx context.Context, out *PushHook, integration *db.GitIntegration, repo string) {
	token, err := s.resolveOAuthToken(ctx, integration, false)
	if err != nil {
		out.State, out.Reason = HookUnreachable, "the connection's access token could not be renewed; reconnect it"
		return
	}
	hooks, err := s.listPushHooks(integration, token, repo)
	switch {
	case errors.Is(err, errNoHookScope):
		out.State, out.Reason = HookNoAccess, "this connection cannot see or manage "+repo+"'s webhooks"
		return
	case errors.Is(err, errUnauthorized):
		out.State, out.Reason = HookUnreachable, "the provider rejected the connection's token; reconnect it"
		return
	case err != nil:
		out.State, out.Reason = HookUnreachable, err.Error()
		return
	}
	for _, h := range hooks {
		if h.URL == out.URL {
			out.State = HookInstalled
			return
		}
	}
	out.State = HookMissing
}

// fillGitHubInstallState asks whether the App can see this repository at all.
func (s *GitIntegrationService) fillGitHubInstallState(ctx context.Context, out *PushHook, integration *db.GitIntegration, repo string) {
	if integration.GHAppID == "" || string(integration.InstallationID) == "" {
		out.State, out.Reason = HookNoAccess, "this GitHub App is not installed yet"
		return
	}
	token, err := getInstallationToken(integration.GHAppID, string(integration.GHPrivateKey), string(integration.InstallationID))
	if err != nil {
		out.State, out.Reason = HookUnreachable, "GitHub would not issue a token for this App: "+err.Error()
		return
	}
	repos, err := fetchAllRepos(token)
	if err != nil {
		out.State, out.Reason = HookUnreachable, err.Error()
		return
	}
	wanted := normalizeRepoPath(repo)
	for _, r := range repos {
		if normalizeRepoPath(r.FullName) == wanted {
			out.State = HookInstalled
			return
		}
	}
	out.State = HookMissing
	out.Reason = repo + " is not one of the repositories this GitHub App is installed on"
}

// InstallPushHook adds the hook on request, and reports what the repository
// looks like afterwards.
func (s *GitIntegrationService) InstallPushHook(ctx context.Context, integrationID uuid.UUID, repo string) (*PushHook, error) {
	if repo == "" {
		return nil, fmt.Errorf("name the repository to add the webhook to")
	}
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return nil, fmt.Errorf("git integration not found")
	}
	if integration.Provider == "github" {
		// Which repositories an App can see is chosen at GitHub, on the
		// installation itself; the console sends people there instead.
		return nil, fmt.Errorf("add the repository to this GitHub App's installation on GitHub, then check again")
	}
	if err := s.EnsurePushHook(ctx, integrationID, repo); err != nil {
		switch {
		case errors.Is(err, errNoHookScope):
			return nil, fmt.Errorf("this connection cannot manage %s's webhooks: on GitLab that means the api scope and Maintainer on the project, on Gitea write:repository and admin, on Bitbucket write:webhook", repo)
		case errors.Is(err, errUnauthorized):
			return nil, fmt.Errorf("the provider rejected the connection's token; reconnect it")
		}
		return nil, err
	}
	return s.PushHookDetails(ctx, integrationID, repo)
}

// EnsurePushHook makes sure repo will tell Meshploy about its pushes. It is
// idempotent: an existing hook pointing at this integration is left alone.
func (s *GitIntegrationService) EnsurePushHook(ctx context.Context, integrationID uuid.UUID, repo string) error {
	if repo == "" {
		return nil
	}
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return fmt.Errorf("git integration not found")
	}
	if integration.Provider == "github" {
		return nil // the App already delivers
	}
	secret := string(integration.WebhookSecret)
	if secret == "" {
		return fmt.Errorf("this integration has no webhook secret; reconnect it to get one")
	}
	token, err := s.resolveOAuthToken(ctx, &integration, false)
	if err != nil {
		return err
	}

	hookURL := s.PushHookURL(&integration)
	existing, err := s.listPushHooks(&integration, token, repo)
	if err != nil {
		return err
	}
	// One of ours on any host counts: a hook made under an earlier primary
	// still delivers while that domain serves, and moving it is its own step.
	for _, h := range existing {
		if isOurHook(&integration, h.URL) {
			return nil
		}
	}
	return s.createPushHook(&integration, token, repo, hookURL, secret)
}

// pushHookRef is one webhook on a repository: what it is called there, and
// where it delivers.
type pushHookRef struct {
	ID  string
	URL string
}

// listPushHooks returns the repository's existing webhooks.
func (s *GitIntegrationService) listPushHooks(integration *db.GitIntegration, token, repo string) ([]pushHookRef, error) {
	switch integration.Provider {
	case "gitlab":
		var hooks []struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		}
		if err := gitAPI(http.MethodGet, gitLabHooksURL(integration.BaseURL, repo), "Bearer", token, nil, &hooks); err != nil {
			return nil, err
		}
		out := make([]pushHookRef, 0, len(hooks))
		for _, h := range hooks {
			out = append(out, pushHookRef{ID: strconv.FormatInt(h.ID, 10), URL: h.URL})
		}
		return out, nil

	case "gitea":
		var hooks []struct {
			ID     int64 `json:"id"`
			Config struct {
				URL string `json:"url"`
			} `json:"config"`
		}
		if err := gitAPI(http.MethodGet, giteaHooksURL(integration.BaseURL, repo), "token", token, nil, &hooks); err != nil {
			return nil, err
		}
		out := make([]pushHookRef, 0, len(hooks))
		for _, h := range hooks {
			out = append(out, pushHookRef{ID: strconv.FormatInt(h.ID, 10), URL: h.Config.URL})
		}
		return out, nil

	case "bitbucket":
		var page struct {
			Values []struct {
				UUID string `json:"uuid"`
				URL  string `json:"url"`
			} `json:"values"`
		}
		if err := gitAPI(http.MethodGet, bitbucketHooksURL(repo), "Bearer", token, nil, &page); err != nil {
			return nil, err
		}
		out := make([]pushHookRef, 0, len(page.Values))
		for _, h := range page.Values {
			out = append(out, pushHookRef{ID: h.UUID, URL: h.URL})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported provider: %s", integration.Provider)
}

// createPushHook adds the hook, asking for push events only.
func (s *GitIntegrationService) createPushHook(integration *db.GitIntegration, token, repo, hookURL, secret string) error {
	switch integration.Provider {
	case "gitlab":
		// GitLab sends the secret back as a header rather than signing the
		// body, which is what the receiver compares.
		return gitAPI(http.MethodPost, gitLabHooksURL(integration.BaseURL, repo), "Bearer", token, map[string]any{
			"url":                     hookURL,
			"push_events":             true,
			"token":                   secret,
			"enable_ssl_verification": true,
		}, nil)

	case "gitea":
		return gitAPI(http.MethodPost, giteaHooksURL(integration.BaseURL, repo), "token", token, map[string]any{
			"type":   "gitea",
			"active": true,
			"events": []string{"push"},
			"config": map[string]string{
				"url":          hookURL,
				"content_type": "json",
				"secret":       secret,
			},
		}, nil)

	case "bitbucket":
		return gitAPI(http.MethodPost, bitbucketHooksURL(repo), "Bearer", token, map[string]any{
			"description": "Meshploy deploy on push",
			"url":         hookURL,
			"active":      true,
			"events":      []string{"repo:push"},
			"secret":      secret,
		}, nil)
	}
	return fmt.Errorf("unsupported provider: %s", integration.Provider)
}

// RemovePushHookIfUnused takes the hook off a repository once nothing wants it.
//
// One hook serves every service built from that repository through this
// integration, so turning deploy-on-push off for one service must not stop the
// others hearing about pushes. It is removed only when no build configuration
// is left asking for it.
//
// Best effort, like adding it: a hook nobody reads is deliveries into a
// endpoint that answers 200 and does nothing, which is untidy rather than
// harmful.
func (s *GitIntegrationService) RemovePushHookIfUnused(ctx context.Context, integrationID uuid.UUID, repo string) error {
	if repo == "" {
		return nil
	}
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return fmt.Errorf("git integration not found")
	}
	if integration.Provider == "github" {
		return nil // nothing was added, so nothing to take away
	}

	var configs []db.BuildConfig
	s.db.WithContext(ctx).
		Where("git_integration_id = ? AND auto_deploy = true", integrationID).
		Find(&configs)
	wanted := normalizeRepoPath(repo)
	for _, bc := range configs {
		if normalizeRepoPath(bc.GitRepo) == wanted {
			return nil // another service still deploys on this repository's pushes
		}
	}

	token, err := s.resolveOAuthToken(ctx, &integration, false)
	if err != nil {
		return err
	}
	hooks, err := s.listPushHooks(&integration, token, repo)
	if err != nil {
		return err
	}
	for _, h := range hooks {
		if !isOurHook(&integration, h.URL) {
			continue
		}
		if err := gitAPI(http.MethodDelete, s.hookURL(&integration, repo, h.ID), hookAuthScheme(integration.Provider), token, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// hookURL addresses one existing hook.
func (s *GitIntegrationService) hookURL(integration *db.GitIntegration, repo, hookID string) string {
	switch integration.Provider {
	case "gitlab":
		return gitLabHooksURL(integration.BaseURL, repo) + "/" + url.PathEscape(hookID)
	case "gitea":
		return giteaHooksURL(integration.BaseURL, repo) + "/" + url.PathEscape(hookID)
	default: // bitbucket, whose id is a braced uuid
		return bitbucketHooksURL(repo) + "/" + url.PathEscape(hookID)
	}
}

// hookAuthScheme is how each provider wants the token presented.
func hookAuthScheme(provider string) string {
	if provider == "gitea" {
		return "token"
	}
	return "Bearer"
}

// removePushHookQuietly is what the workload service calls when deploy-on-push
// is turned off. Nothing it does can fail the save.
func (s *GitIntegrationService) removePushHookQuietly(integrationID uuid.UUID, repo string) {
	go func() {
		if err := s.RemovePushHookIfUnused(context.Background(), integrationID, repo); err != nil {
			log.Printf("git: could not remove the push hook for %s: %v", repo, err)
		}
	}()
}

func gitLabHooksURL(baseURL, repo string) string {
	// A GitLab project is addressed by its full path, which has to arrive
	// encoded slashes and all.
	return fmt.Sprintf("%s/api/v4/projects/%s/hooks", gitLabBase(baseURL), url.PathEscape(strings.Trim(repo, "/")))
}

func giteaHooksURL(baseURL, repo string) string {
	return fmt.Sprintf("%s/api/v1/repos/%s/hooks", strings.TrimRight(baseURL, "/"), strings.Trim(repo, "/"))
}

func bitbucketHooksURL(repo string) string {
	return fmt.Sprintf("%s/repositories/%s/hooks", bitbucketAPI, strings.Trim(repo, "/"))
}

// gitAPI performs one authenticated JSON call. out may be nil when the response
// body is not needed.
func gitAPI(method, apiURL, scheme, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, apiURL, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", scheme+" "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errUnauthorized
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		// Both mean the same thing in practice here: this token cannot see or
		// manage the repository's hooks.
		return errNoHookScope
	case resp.StatusCode >= 300:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("%s returned %d: %s", apiURL, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ensurePushHookQuietly is what the workload service calls when a build
// configuration is saved with deploy-on-push turned on. Nothing it does can
// fail the save.
func (s *GitIntegrationService) ensurePushHookQuietly(integrationID uuid.UUID, repo string) {
	go func() {
		if err := s.EnsurePushHook(context.Background(), integrationID, repo); err != nil {
			log.Printf("git: could not create the push hook for %s: %v", repo, err)
		}
	}()
}
