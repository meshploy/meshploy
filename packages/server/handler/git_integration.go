package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

// ─── I/O types ────────────────────────────────────────────────────────────────

type ListGitIntegrationsOutput struct {
	Body []db.GitIntegration
}

type GitIntegrationPathInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
}

type GitHubInstallURLOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

type ListReposOutput struct {
	Body []RepoItem
}

type RepoItem struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type DetectStackOutput struct {
	Body *service.Detection
}

type ListBranchesOutput struct {
	Body []string
}

type GetPushHookOutput struct {
	Body service.PushHook
}

type PushHookInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
	// Repo asks whether the hook is on that repository. Without it the answer
	// is only where deliveries go and what signs them.
	Repo string `query:"repo"`
}

type InstallPushHookInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
	Body  struct {
		Repo string `json:"repo" minLength:"1"`
	}
}

type CreatePATIntegrationInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Provider string `json:"provider" enum:"gitlab,gitea,bitbucket"`
		Name     string `json:"name"     minLength:"1" maxLength:"100"`
		BaseURL  string `json:"base_url,omitempty"`
		Groups   string `json:"groups,omitempty"`
		Token    string `json:"token"    minLength:"1"`
	}
}

type CreatePATIntegrationOutput struct {
	Body *db.GitIntegration
}

type InitOAuthIntegrationInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Provider     string `json:"provider"      enum:"gitlab,gitea,bitbucket"`
		Name         string `json:"name"          minLength:"1" maxLength:"100"`
		BaseURL      string `json:"base_url,omitempty"`
		Groups       string `json:"groups,omitempty"`
		RedirectURI  string `json:"redirect_uri"  minLength:"1"`
		ClientID     string `json:"client_id"     minLength:"1"`
		ClientSecret string `json:"client_secret" minLength:"1"`
	}
}

type InitOAuthIntegrationOutput struct {
	Body struct {
		AuthURL     string `json:"auth_url"`
		RedirectURI string `json:"redirect_uri"`
	}
}

type InitGitHubIntegrationInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		GithubOrg string `json:"github_org,omitempty"`
	}
}

type InitGitHubIntegrationOutput struct {
	Body struct {
		Integration *db.GitIntegration `json:"integration"`
		GithubURL   string             `json:"github_url"`
		Manifest    string             `json:"manifest"`
	}
}

type OAuthReconnectOutput struct {
	Body struct {
		AuthURL string `json:"auth_url"`
	}
}

// ─── Routes ───────────────────────────────────────────────────────────────────

// gitIntegrationInOrg refuses an integration that is not the org's, as not
// found. The org and role checks alone say nothing about the id in the path
// or the body, which could name another org's integration and so reach its
// repositories, its push-hook secret, or delete it.
func (h *Handler) gitIntegrationInOrg(ctx context.Context, orgIDStr string, id uuid.UUID) error {
	orgID, err := parseUUID(orgIDStr)
	if err != nil {
		return err
	}
	if err := h.svc.GitIntegrations.InOrg(ctx, orgID, id); err != nil {
		if errors.Is(err, service.ErrGitIntegrationNotFound) {
			return huma.Error404NotFound("git integration not found")
		}
		return huma.Error500InternalServerError("check git integration", err)
	}
	return nil
}

func (h *Handler) registerGitIntegrationRoutes(api huma.API) {
	const tag = "Git Integrations"

	// List
	huma.Register(api, huma.Operation{
		OperationID: "list-git-integrations",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/git-integrations",
		Summary:     "List git integrations",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*ListGitIntegrationsOutput, error) {
		_, orgID, _, err := h.checkOrgMemberAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		rows, err := h.svc.GitIntegrations.List(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &ListGitIntegrationsOutput{Body: rows}, nil
	})

	// Init GitHub integration (create pending row + return manifest setup data)
	huma.Register(api, huma.Operation{
		OperationID:   "init-github-git-integration",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/git-integrations/github",
		Summary:       "Start a GitHub App integration (manifest flow)",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *InitGitHubIntegrationInput) (*InitGitHubIntegrationOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		row, githubURL, manifest, err := h.svc.GitIntegrations.InitGitHubIntegration(ctx, orgID, in.Body.GithubOrg)
		if err != nil {
			return nil, err
		}
		out := &InitGitHubIntegrationOutput{}
		out.Body.Integration = row
		out.Body.GithubURL = githubURL
		out.Body.Manifest = manifest
		return out, nil
	})

	// Create PAT-based integration (GitLab / Gitea)
	huma.Register(api, huma.Operation{
		OperationID:   "create-pat-git-integration",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/git-integrations",
		Summary:       "Create a GitLab, Gitea or Bitbucket integration from a token",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreatePATIntegrationInput) (*CreatePATIntegrationOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		row, err := h.svc.GitIntegrations.CreatePATIntegration(ctx, orgID, in.Body.Provider, in.Body.Name, in.Body.BaseURL, in.Body.Groups, in.Body.Token)
		if err != nil {
			return nil, err
		}
		return &CreatePATIntegrationOutput{Body: row}, nil
	})

	// Init OAuth integration (GitLab / Gitea)
	huma.Register(api, huma.Operation{
		OperationID:   "init-oauth-git-integration",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/git-integrations/oauth",
		Summary:       "Start a GitLab, Gitea or Bitbucket OAuth App connection",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *InitOAuthIntegrationInput) (*InitOAuthIntegrationOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		_, authURL, err := h.svc.GitIntegrations.InitOAuthIntegration(
			ctx, orgID, in.Body.Provider, in.Body.Name,
			in.Body.BaseURL, in.Body.Groups, in.Body.RedirectURI, in.Body.ClientID, in.Body.ClientSecret,
		)
		if err != nil {
			return nil, err
		}
		out := &InitOAuthIntegrationOutput{}
		out.Body.AuthURL = authURL
		out.Body.RedirectURI = in.Body.RedirectURI
		return out, nil
	})

	// GitHub install URL (per-integration)
	huma.Register(api, huma.Operation{
		OperationID: "github-integration-install-url",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/install-url",
		Summary:     "Get GitHub App install URL for a specific integration",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID     string `path:"orgId"`
		ID        string `path:"id"`
		GithubOrg string `query:"github_org"`
	}) (*GitHubInstallURLOutput, error) {
		_, _, integrationID, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, integrationID); err != nil {
			return nil, err
		}
		installURL, err := h.svc.GitIntegrations.GitHubInstallURL(ctx, integrationID, in.GithubOrg)
		if err != nil {
			return nil, err
		}
		out := &GitHubInstallURLOutput{}
		out.Body.URL = installURL
		return out, nil
	})

	// OAuth reconnect (GitLab / Gitea)
	huma.Register(api, huma.Operation{
		OperationID: "oauth-reconnect-git-integration",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/oauth-reconnect",
		Summary:     "Re-generate OAuth authorization URL for a pending integration",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *GitIntegrationPathInput) (*OAuthReconnectOutput, error) {
		_, _, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, id); err != nil {
			return nil, err
		}
		authURL, err := h.svc.GitIntegrations.OAuthReconnect(ctx, id)
		if err != nil {
			return nil, err
		}
		out := &OAuthReconnectOutput{}
		out.Body.AuthURL = authURL
		return out, nil
	})

	// List repos
	huma.Register(api, huma.Operation{
		OperationID: "list-git-repos",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/repos",
		Summary:     "List repositories for a git integration",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *GitIntegrationPathInput) (*ListReposOutput, error) {
		_, _, id, err := h.checkOrgMemberAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, id); err != nil {
			return nil, err
		}
		repos, err := h.svc.GitIntegrations.ListRepos(ctx, id)
		if err != nil {
			return nil, err
		}
		items := make([]RepoItem, len(repos))
		for i, r := range repos {
			items[i] = RepoItem{
				FullName:      r.FullName,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
			}
		}
		return &ListReposOutput{Body: items}, nil
	})

	// Push hook details, for setting one up by hand
	huma.Register(api, huma.Operation{
		OperationID: "get-git-push-hook",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/push-hook",
		Summary:     "Where a provider should deliver pushes for this integration, and the secret to sign them with",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *PushHookInput) (*GetPushHookOutput, error) {
		// Admin, not member: the response carries the secret that makes a
		// delivery trusted.
		_, _, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, id); err != nil {
			return nil, err
		}
		hook, err := h.svc.GitIntegrations.PushHookDetails(ctx, id, in.Repo)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &GetPushHookOutput{Body: *hook}, nil
	})

	// Add the push hook to a repository on request
	huma.Register(api, huma.Operation{
		OperationID:   "install-git-push-hook",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/git-integrations/{id}/push-hook",
		Summary:       "Add this integration's push webhook to a repository",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *InstallPushHookInput) (*GetPushHookOutput, error) {
		_, _, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, id); err != nil {
			return nil, err
		}
		hook, err := h.svc.GitIntegrations.InstallPushHook(ctx, id, in.Body.Repo)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &GetPushHookOutput{Body: *hook}, nil
	})

	// List branches
	huma.Register(api, huma.Operation{
		OperationID: "list-git-branches",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/branches",
		Summary:     "List branches for a repository",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		ID    string `path:"id"`
		Repo  string `query:"repo"`
	}) (*ListBranchesOutput, error) {
		_, _, id, err := h.checkOrgMemberAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, id); err != nil {
			return nil, err
		}
		if in.Repo == "" {
			return nil, huma.Error400BadRequest("repo query param is required")
		}
		branches, err := h.svc.GitIntegrations.ListBranches(ctx, id, in.Repo)
		if err != nil {
			return nil, err
		}
		return &ListBranchesOutput{Body: branches}, nil
	})

	// Detect: what a repository's branch is, to fill in a new service.
	huma.Register(api, huma.Operation{
		OperationID: "detect-repository-stack",
		Method:      http.MethodPost,
		Path:        "/api/v1/orgs/{orgId}/detect-stack",
		Summary:     "Look at a repository's branch and suggest how to build and run it: builder, port, start command, memory",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		Body  struct {
			// GitIntegrationID is empty for a public repository.
			GitIntegrationID string `json:"git_integration_id,omitempty"`
			Repo             string `json:"repo" minLength:"1"`
			Branch           string `json:"branch" minLength:"1"`
			DockerfilePath   string `json:"dockerfile_path,omitempty"`
		}
	}) (*DetectStackOutput, error) {
		_, orgID, integrationID, err := h.checkOrgMemberAccess(ctx, in.OrgID, in.Body.GitIntegrationID)
		if err != nil {
			return nil, err
		}
		var idp *uuid.UUID
		if in.Body.GitIntegrationID != "" {
			idp = &integrationID
		}
		d, err := h.svc.GitIntegrations.DetectStack(ctx, orgID, idp, in.Body.Repo, in.Body.Branch, in.Body.DockerfilePath)
		if errors.Is(err, service.ErrGitIntegrationNotFound) {
			return nil, huma.Error404NotFound("git integration not found")
		}
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return &DetectStackOutput{Body: d}, nil
	})

	// Delete
	huma.Register(api, huma.Operation{
		OperationID:   "delete-git-integration",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/git-integrations/{id}",
		Summary:       "Delete a git integration",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *GitIntegrationPathInput) (*struct{}, error) {
		_, _, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		if err := h.gitIntegrationInOrg(ctx, in.OrgID, id); err != nil {
			return nil, err
		}
		if err := h.svc.GitIntegrations.Delete(ctx, id); err != nil {
			return nil, err
		}
		return &struct{}{}, nil
	})
}

// GitHubAppCallback handles the redirect back from GitHub after manifest app creation.
// GitHub sends ?code=&state= — we exchange code for credentials and store them on the
// GitIntegration row identified by the state token.
func (h *Handler) GitHubAppCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.consoleAfterCallback(r)

	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations/git?github_setup=error&reason=missing_params", http.StatusFound)
		return
	}

	if err := h.svc.GitIntegrations.HandleAppCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations/git?github_setup=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}

	http.Redirect(w, r, frontendURL+"/integrations/git?github_setup=done", http.StatusFound)
}

// GitLabOAuthCallback handles the redirect back from GitLab after OAuth authorization.
func (h *Handler) GitLabOAuthCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.consoleAfterCallback(r)
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations/git?gitlab=error&reason=missing_params", http.StatusFound)
		return
	}
	if _, err := h.svc.GitIntegrations.HandleGitLabOAuthCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations/git?gitlab=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}
	http.Redirect(w, r, frontendURL+"/integrations/git?gitlab=connected", http.StatusFound)
}

// GiteaOAuthCallback handles the redirect back from Gitea after OAuth authorization.
func (h *Handler) GiteaOAuthCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.consoleAfterCallback(r)
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations/git?gitea=error&reason=missing_params", http.StatusFound)
		return
	}
	if _, err := h.svc.GitIntegrations.HandleGiteaOAuthCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations/git?gitea=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}
	http.Redirect(w, r, frontendURL+"/integrations/git?gitea=connected", http.StatusFound)
}

// BitbucketOAuthCallback handles the redirect back from Bitbucket after OAuth
// authorization.
func (h *Handler) BitbucketOAuthCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.consoleAfterCallback(r)
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations/git?bitbucket=error&reason=missing_params", http.StatusFound)
		return
	}
	if _, err := h.svc.GitIntegrations.HandleBitbucketOAuthCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations/git?bitbucket=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}
	http.Redirect(w, r, frontendURL+"/integrations/git?bitbucket=connected", http.StatusFound)
}

// GitHubCallback handles the redirect back from GitHub after App installation.
func (h *Handler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.consoleAfterCallback(r)

	q := r.URL.Query()
	installationID := q.Get("installation_id")
	setupAction := q.Get("setup_action")
	state := q.Get("state")

	if setupAction != "install" && setupAction != "update" {
		http.Redirect(w, r, frontendURL+"/integrations/git", http.StatusFound)
		return
	}

	if installationID == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations/git?github=error&reason=missing_params", http.StatusFound)
		return
	}

	_, err := h.svc.GitIntegrations.HandleGitHubCallback(r.Context(), installationID, state)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations/git?github=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}

	http.Redirect(w, r, frontendURL+"/integrations/git?github=connected", http.StatusFound)
}

// consoleAfterCallback is where a provider's redirect back sends the browser:
// the console on the domain the callback arrived through, so the person lands
// on the console they left - and stays signed in, since a session belongs to
// one console's address. FRONTEND_URL is written once at install and would send
// them to the old primary after it moved.
//
// The Host header is the caller's to write, so it is honoured only for a domain
// this gateway serves the platform on. Anything else is an open redirect waiting
// to be used, and falls back to the primary's console.
func (h *Handler) consoleAfterCallback(r *http.Request) string {
	host := strings.ToLower(r.Host)
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if h.svc != nil && h.svc.Domains != nil {
		for _, prefix := range []string{"api.", "console."} {
			if base, ok := strings.CutPrefix(host, prefix); ok && h.svc.Domains.ServesPlatform(r.Context(), base) {
				return "https://console." + base
			}
		}
		if u := h.svc.Domains.GatewayPlatformURL(r.Context(), "console"); u != "" {
			return u
		}
	}
	if h.cfg != nil {
		return strings.TrimRight(h.cfg.FrontendURL, "/")
	}
	return ""
}
