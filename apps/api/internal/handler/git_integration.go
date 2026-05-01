package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
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

type ListBranchesOutput struct {
	Body []string
}

type CreatePATIntegrationInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Provider string `json:"provider" enum:"gitlab,gitea"`
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
		Provider     string `json:"provider"      enum:"gitlab,gitea"`
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
		requireUser(ctx)
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
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
		requireUser(ctx)
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
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
		Summary:       "Create a GitLab or Gitea integration via personal access token",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreatePATIntegrationInput) (*CreatePATIntegrationOutput, error) {
		requireUser(ctx)
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
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
		Summary:       "Start a GitLab or Gitea OAuth App connection",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *InitOAuthIntegrationInput) (*InitOAuthIntegrationOutput, error) {
		requireUser(ctx)
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
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
		requireUser(ctx)
		integrationID, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
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
		requireUser(ctx)
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
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
		requireUser(ctx)
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
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
		requireUser(ctx)
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
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
		requireUser(ctx)
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid integration ID")
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
	frontendURL := h.cfg.FrontendURL

	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations?github_setup=error&reason=missing_params", http.StatusFound)
		return
	}

	if err := h.svc.GitIntegrations.HandleAppCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations?github_setup=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}

	http.Redirect(w, r, frontendURL+"/integrations?github_setup=done", http.StatusFound)
}

// GitLabOAuthCallback handles the redirect back from GitLab after OAuth authorization.
func (h *Handler) GitLabOAuthCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.cfg.FrontendURL
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations?gitlab=error&reason=missing_params", http.StatusFound)
		return
	}
	if _, err := h.svc.GitIntegrations.HandleGitLabOAuthCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations?gitlab=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}
	http.Redirect(w, r, frontendURL+"/integrations?gitlab=connected", http.StatusFound)
}

// GiteaOAuthCallback handles the redirect back from Gitea after OAuth authorization.
func (h *Handler) GiteaOAuthCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.cfg.FrontendURL
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations?gitea=error&reason=missing_params", http.StatusFound)
		return
	}
	if _, err := h.svc.GitIntegrations.HandleGiteaOAuthCallback(r.Context(), code, state); err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations?gitea=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}
	http.Redirect(w, r, frontendURL+"/integrations?gitea=connected", http.StatusFound)
}

// GitHubCallback handles the redirect back from GitHub after App installation.
func (h *Handler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := h.cfg.FrontendURL

	q := r.URL.Query()
	installationID := q.Get("installation_id")
	setupAction := q.Get("setup_action")
	state := q.Get("state")

	if setupAction != "install" && setupAction != "update" {
		http.Redirect(w, r, frontendURL+"/integrations", http.StatusFound)
		return
	}

	if installationID == "" || state == "" {
		http.Redirect(w, r, frontendURL+"/integrations?github=error&reason=missing_params", http.StatusFound)
		return
	}

	_, err := h.svc.GitIntegrations.HandleGitHubCallback(r.Context(), installationID, state)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("%s/integrations?github=error&reason=internal_error", frontendURL), http.StatusFound)
		return
	}

	http.Redirect(w, r, frontendURL+"/integrations?github=connected", http.StatusFound)
}
