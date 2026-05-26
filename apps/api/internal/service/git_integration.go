package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// GitIntegrationService handles GitHub App setup, installation flows, and repo listing.
type GitIntegrationService struct {
	db  *gorm.DB
	cfg *config.Config
}

// GitRepo is a minimal repo descriptor returned to callers.
type GitRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// ─── GitHub App per-integration flow ─────────────────────────────────────────

// InitGitHubIntegration creates a pending GitIntegration row (no credentials yet),
// then returns the GitHub manifest-setup URL and manifest JSON the frontend must
// POST to GitHub to create the GitHub App.
// Each call creates a separate GitHub App — multiple pending integrations are allowed.
func (s *GitIntegrationService) InitGitHubIntegration(
	ctx context.Context,
	orgID uuid.UUID,
	githubOrg string,
) (row *db.GitIntegration, githubURL, manifest string, err error) {
	autoName := "github-" + time.Now().UTC().Format("20060102-150405")
	row = &db.GitIntegration{
		OrganizationID: orgID,
		Provider:       "github",
		AuthMethod:     "app",
		Name:           autoName,
	}
	if err = s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, "", "", huma.Error500InternalServerError("failed to create git integration")
	}

	state := buildState(row.ID.String(), s.cfg.JWTSecret)

	base := s.cfg.FrontendURL
	apiBase := s.cfg.APIBaseURL
	m := map[string]any{
		"name":         "Meshploy",
		"url":          base,
		"redirect_url": apiBase + "/api/v1/github/app-callback",
		"callback_urls": []string{apiBase + "/api/v1/github/callback"},
		"setup_url":    apiBase + "/api/v1/github/callback",
		"public":       false,
		"default_permissions": map[string]string{
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "read",
		},
		"default_events": []string{"push"},
		"hook_attributes": map[string]any{
			"url":    apiBase + "/api/v1/webhooks/github/" + row.ID.String(),
			"active": true,
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(row)
		return nil, "", "", fmt.Errorf("marshal manifest: %w", err)
	}
	manifest = string(b)

	if githubOrg != "" {
		githubURL = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new?state=%s", githubOrg, state)
	} else {
		githubURL = fmt.Sprintf("https://github.com/settings/apps/new?state=%s", state)
	}
	return row, githubURL, manifest, nil
}

// HandleAppCallback is called by the GitHub redirect after manifest app creation.
// It exchanges the code for GitHub App credentials and stores them on the
// specific GitIntegration row identified by the state token.
func (s *GitIntegrationService) HandleAppCallback(ctx context.Context, code, state string) error {
	integrationIDStr, err := validateState(state, s.cfg.JWTSecret)
	if err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}
	integrationID, err := uuid.Parse(integrationIDStr)
	if err != nil {
		return fmt.Errorf("invalid state payload: not a valid integration ID")
	}

	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return fmt.Errorf("integration not found")
	}
	if integration.Provider != "github" {
		return fmt.Errorf("integration is not a GitHub integration")
	}

	// Exchange code → app credentials.
	exchangeURL := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	req, _ := http.NewRequest(http.MethodPost, exchangeURL, bytes.NewReader(nil))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	updates := map[string]any{
		"gh_app_id":         fmt.Sprintf("%d", result.ID),
		"gh_app_slug":       result.Slug,
		"gh_client_id":      result.ClientID,
		"gh_client_secret":  db.EncryptedString(result.ClientSecret),
		"gh_private_key":    db.EncryptedString(result.PEM),
		"gh_webhook_secret": db.EncryptedString(result.WebhookSecret),
		"name":              result.Slug,
	}
	return s.db.WithContext(ctx).Model(&integration).Updates(updates).Error
}

// GitHubInstallURL returns the GitHub App installation URL for a specific integration.
// The state token encodes the integration ID so the install callback knows which row to update.
func (s *GitIntegrationService) GitHubInstallURL(ctx context.Context, integrationID uuid.UUID, githubOrg string) (string, error) {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return "", huma.Error404NotFound("git integration not found")
	}
	if integration.GHAppSlug == "" {
		return "", huma.Error400BadRequest("GitHub App credentials not yet set — complete the manifest setup first")
	}

	state := buildState(integrationID.String(), s.cfg.JWTSecret)
	installURL := fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", integration.GHAppSlug, state)
	if githubOrg != "" {
		targetID, err := fetchGitHubOrgID(githubOrg)
		if err != nil {
			return "", huma.Error400BadRequest("could not find GitHub org: " + err.Error())
		}
		installURL += fmt.Sprintf("&suggested_target_id=%d", targetID)
	}
	return installURL, nil
}

// HandleGitHubCallback validates the install state, sets InstallationID on the
// specific GitIntegration row, and updates the name to the installed account login.
func (s *GitIntegrationService) HandleGitHubCallback(ctx context.Context, installationID, state string) (*db.GitIntegration, error) {
	integrationIDStr, err := validateState(state, s.cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid state: %w", err)
	}
	integrationID, err := uuid.Parse(integrationIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid state payload: not a valid integration ID")
	}

	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return nil, fmt.Errorf("integration not found")
	}

	appJWT, err := generateAppJWT(integration.GHAppID, string(integration.GHPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to generate GitHub App JWT: %w", err)
	}
	accountLogin, err := fetchInstallationLogin(installationID, appJWT)
	if err != nil {
		accountLogin = integration.Name // keep existing name if fetch failed
	}

	updates := map[string]any{
		"installation_id": db.EncryptedString(installationID),
		"name":            accountLogin,
	}
	if err := s.db.WithContext(ctx).Model(&integration).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to save installation: %w", err)
	}
	integration.InstallationID = db.EncryptedString(installationID)
	integration.Name = accountLogin
	integration.Connected = true
	return &integration, nil
}

// ─── Org-level integration management ────────────────────────────────────────

// GetByID returns a single git integration by its ID (no org scoping — used internally).
func (s *GitIntegrationService) GetByID(ctx context.Context, id uuid.UUID) (*db.GitIntegration, error) {
	var row db.GitIntegration
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List returns all git integrations for an org with Connected computed.
func (s *GitIntegrationService) List(ctx context.Context, orgID uuid.UUID) ([]db.GitIntegration, error) {
	rows := make([]db.GitIntegration, 0)
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&rows).Error; err != nil {
		return nil, huma.Error500InternalServerError("failed to list git integrations")
	}
	for i := range rows {
		rows[i].Connected = string(rows[i].InstallationID) != ""
	}
	return rows, nil
}

// Delete removes an integration by ID.
func (s *GitIntegrationService) Delete(ctx context.Context, id uuid.UUID) error {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, id).Error; err != nil {
		return huma.Error404NotFound("git integration not found")
	}
	if err := s.db.WithContext(ctx).Delete(&db.GitIntegration{}, id).Error; err != nil {
		return huma.Error500InternalServerError("failed to delete git integration")
	}
	return nil
}

// OAuthReconnect resets the OAuth access token on a pending GitLab/Gitea OAuth
// integration and returns a fresh authorization URL using the stored client credentials.
func (s *GitIntegrationService) OAuthReconnect(ctx context.Context, integrationID uuid.UUID) (string, error) {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return "", huma.Error404NotFound("git integration not found")
	}
	if integration.AuthMethod != "oauth" {
		return "", huma.Error400BadRequest("integration is not OAuth-based")
	}

	// Clear the stale tokens so the row returns to pending state.
	if err := s.db.WithContext(ctx).Model(&integration).Updates(map[string]any{
		"installation_id":      db.EncryptedString(""),
		"o_auth_refresh_token": db.EncryptedString(""),
		"o_auth_token_expiry":  nil,
	}).Error; err != nil {
		return "", huma.Error500InternalServerError("failed to reset integration")
	}

	state := buildState(integrationID.String(), s.cfg.JWTSecret)
	clientID := integration.OAuthClientID
	redirectURI := integration.OAuthRedirectURI

	var authURL string
	switch integration.Provider {
	case "gitlab":
		base := gitLabBase(integration.BaseURL)
		authURL = fmt.Sprintf("%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
			base, url.QueryEscape(clientID), url.QueryEscape(redirectURI),
			url.QueryEscape("api read_user read_repository"), state)
	case "gitea":
		base := strings.TrimRight(integration.BaseURL, "/")
		authURL = fmt.Sprintf("%s/login/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
			base, url.QueryEscape(clientID), url.QueryEscape(redirectURI), state)
	default:
		return "", huma.Error400BadRequest("unsupported provider: " + integration.Provider)
	}
	return authURL, nil
}

// ListBranches returns branch names for a specific repo in a git integration.
func (s *GitIntegrationService) ListBranches(ctx context.Context, integrationID uuid.UUID, repo string) ([]string, error) {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return nil, huma.Error404NotFound("git integration not found")
	}

	switch integration.Provider {
	case "gitlab":
		token, err := s.resolveOAuthToken(ctx, &integration, false)
		if err == errUnauthorized {
			return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		branches, err := listGitLabBranches(integration.BaseURL, token, repo)
		if err == errUnauthorized {
			token, err = s.resolveOAuthToken(ctx, &integration, true)
			if err == errUnauthorized {
				return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
			}
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			branches, err = listGitLabBranches(integration.BaseURL, token, repo)
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list GitLab branches: " + err.Error())
		}
		return branches, nil
	case "gitea":
		token, err := s.resolveOAuthToken(ctx, &integration, false)
		if err == errUnauthorized {
			return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		branches, err := listGiteaBranches(integration.BaseURL, token, repo)
		if err == errUnauthorized {
			token, err = s.resolveOAuthToken(ctx, &integration, true)
			if err == errUnauthorized {
				return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
			}
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			branches, err = listGiteaBranches(integration.BaseURL, token, repo)
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list Gitea branches: " + err.Error())
		}
		return branches, nil
	}

	// GitHub App flow — credentials are on the integration row itself.
	if integration.GHAppID == "" || string(integration.InstallationID) == "" {
		return nil, huma.Error400BadRequest("GitHub App is not fully configured — complete setup and installation first")
	}
	token, err := getInstallationToken(integration.GHAppID, string(integration.GHPrivateKey), string(integration.InstallationID))
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get GitHub installation token: " + err.Error())
	}

	var all []string
	page := 1
	for {
		reqURL := fmt.Sprintf("https://api.github.com/repos/%s/branches?per_page=100&page=%d", repo, page)
		req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, huma.Error500InternalServerError("GitHub API request failed")
		}
		defer resp.Body.Close()

		var result []struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, huma.Error500InternalServerError("failed to decode GitHub response")
		}
		for _, b := range result {
			all = append(all, b.Name)
		}
		if len(result) < 100 {
			break
		}
		page++
	}
	return all, nil
}

// CreatePATIntegration creates a GitLab or Gitea integration using a personal access token.
// The token is validated by hitting the provider's /user endpoint before persisting.
func (s *GitIntegrationService) CreatePATIntegration(ctx context.Context, orgID uuid.UUID, provider, name, baseURL, groups, pat string) (*db.GitIntegration, error) {
	switch provider {
	case "gitlab":
		if err := validateGitToken(gitLabBase(baseURL)+"/api/v4/user", "Bearer", pat); err != nil {
			return nil, huma.Error400BadRequest("invalid GitLab token: " + err.Error())
		}
	case "gitea":
		if baseURL == "" {
			return nil, huma.Error400BadRequest("instance URL is required for Gitea")
		}
		if err := validateGitToken(strings.TrimRight(baseURL, "/")+"/api/v1/user", "token", pat); err != nil {
			return nil, huma.Error400BadRequest("invalid Gitea token: " + err.Error())
		}
	default:
		return nil, huma.Error400BadRequest("unsupported provider: " + provider)
	}

	row := db.GitIntegration{
		OrganizationID: orgID,
		Provider:       provider,
		AuthMethod:     "pat",
		Name:           name,
		InstallationID: db.EncryptedString(pat),
		BaseURL:        baseURL,
		Groups:         groups,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, huma.Error500InternalServerError("failed to save git integration")
	}
	row.Connected = true
	return &row, nil
}

// InitOAuthIntegration creates a pending GitLab/Gitea integration record and returns
// the OAuth authorization URL the user should be redirected to.
func (s *GitIntegrationService) InitOAuthIntegration(ctx context.Context, orgID uuid.UUID, provider, name, baseURL, groups, redirectURI, clientID, clientSecret string) (*db.GitIntegration, string, error) {
	row := db.GitIntegration{
		OrganizationID:    orgID,
		Provider:          provider,
		AuthMethod:        "oauth",
		Name:              name,
		BaseURL:           baseURL,
		Groups:            groups,
		OAuthClientID:     clientID,
		OAuthClientSecret: db.EncryptedString(clientSecret),
		OAuthRedirectURI:  redirectURI,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, "", huma.Error500InternalServerError("failed to save git integration")
	}

	state := buildState(row.ID.String(), s.cfg.JWTSecret)
	var authURL string
	switch provider {
	case "gitlab":
		base := gitLabBase(baseURL)
		authURL = fmt.Sprintf("%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
			base, url.QueryEscape(clientID), url.QueryEscape(redirectURI),
			url.QueryEscape("api read_user read_repository"), state)
	case "gitea":
		base := strings.TrimRight(baseURL, "/")
		authURL = fmt.Sprintf("%s/login/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
			base, url.QueryEscape(clientID), url.QueryEscape(redirectURI), state)
	default:
		_ = s.db.WithContext(ctx).Delete(&row)
		return nil, "", huma.Error400BadRequest("unsupported provider: " + provider)
	}

	return &row, authURL, nil
}

// HandleGitLabOAuthCallback exchanges the authorization code for an access token
// and stores it on the integration record.
func (s *GitIntegrationService) HandleGitLabOAuthCallback(ctx context.Context, code, state string) (*db.GitIntegration, error) {
	integrationID, err := validateState(state, s.cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid state: %w", err)
	}
	id, err := uuid.Parse(integrationID)
	if err != nil {
		return nil, fmt.Errorf("malformed integration ID in state")
	}

	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, id).Error; err != nil {
		return nil, fmt.Errorf("integration not found")
	}

	base := gitLabBase(integration.BaseURL)
	redirectURI := integration.OAuthRedirectURI
	tok, err := exchangeOAuthCode(base+"/oauth/token",
		integration.OAuthClientID, string(integration.OAuthClientSecret), code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	updates := map[string]any{
		"installation_id":    db.EncryptedString(tok.AccessToken),
		"o_auth_refresh_token": db.EncryptedString(tok.RefreshToken),
		"o_auth_token_expiry":  tok.Expiry,
	}
	if err := s.db.WithContext(ctx).Model(&integration).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to persist access token")
	}
	integration.InstallationID = db.EncryptedString(tok.AccessToken)
	return &integration, nil
}

// HandleGiteaOAuthCallback does the same for Gitea.
func (s *GitIntegrationService) HandleGiteaOAuthCallback(ctx context.Context, code, state string) (*db.GitIntegration, error) {
	integrationID, err := validateState(state, s.cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid state: %w", err)
	}
	id, err := uuid.Parse(integrationID)
	if err != nil {
		return nil, fmt.Errorf("malformed integration ID in state")
	}

	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, id).Error; err != nil {
		return nil, fmt.Errorf("integration not found")
	}

	base := strings.TrimRight(integration.BaseURL, "/")
	redirectURI := integration.OAuthRedirectURI
	tok, err := exchangeOAuthCode(base+"/login/oauth/access_token",
		integration.OAuthClientID, string(integration.OAuthClientSecret), code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	updates := map[string]any{
		"installation_id":      db.EncryptedString(tok.AccessToken),
		"o_auth_refresh_token": db.EncryptedString(tok.RefreshToken),
		"o_auth_token_expiry":  tok.Expiry,
	}
	if err := s.db.WithContext(ctx).Model(&integration).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to persist access token")
	}
	integration.InstallationID = db.EncryptedString(tok.AccessToken)
	return &integration, nil
}

// resolveOAuthToken returns a valid access token for an OAuth integration.
// If force is false: only refreshes when the token is within 5 minutes of expiry.
// If force is true: always refreshes using the refresh token (called after a 401 response).
// Returns errUnauthorized if refresh is impossible (no refresh token) so the
// caller can surface a reconnect prompt to the user.
func (s *GitIntegrationService) resolveOAuthToken(ctx context.Context, integration *db.GitIntegration, force bool) (string, error) {
	if integration.AuthMethod != "oauth" {
		return string(integration.InstallationID), nil
	}

	needsRefresh := force
	if !needsRefresh {
		// Proactively refresh when within 5 minutes of expiry (or already expired).
		if integration.OAuthTokenExpiry != nil && time.Until(*integration.OAuthTokenExpiry) <= 5*time.Minute {
			needsRefresh = true
		}
	}

	if !needsRefresh {
		return string(integration.InstallationID), nil
	}

	if string(integration.OAuthRefreshToken) == "" {
		return "", errUnauthorized
	}

	var tokenURL string
	switch integration.Provider {
	case "gitlab":
		tokenURL = gitLabBase(integration.BaseURL) + "/oauth/token"
	case "gitea":
		tokenURL = strings.TrimRight(integration.BaseURL, "/") + "/login/oauth/access_token"
	default:
		return string(integration.InstallationID), nil
	}

	tok, err := refreshOAuthToken(tokenURL, integration.OAuthClientID, string(integration.OAuthClientSecret), string(integration.OAuthRefreshToken))
	if err != nil {
		return "", errUnauthorized
	}

	updates := map[string]any{
		"installation_id":      db.EncryptedString(tok.AccessToken),
		"o_auth_refresh_token": db.EncryptedString(tok.RefreshToken),
		"o_auth_token_expiry":  tok.Expiry,
	}
	if err := s.db.WithContext(ctx).Model(integration).Updates(updates).Error; err != nil {
		return "", fmt.Errorf("failed to persist refreshed token")
	}
	integration.InstallationID = db.EncryptedString(tok.AccessToken)
	integration.OAuthRefreshToken = db.EncryptedString(tok.RefreshToken)
	integration.OAuthTokenExpiry = tok.Expiry
	return tok.AccessToken, nil
}

// ListRepos returns all repositories accessible via a GitHub App installation,
// GitLab PAT, or Gitea PAT depending on the integration provider.
func (s *GitIntegrationService) ListRepos(ctx context.Context, integrationID uuid.UUID) ([]GitRepo, error) {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return nil, huma.Error404NotFound("git integration not found")
	}

	switch integration.Provider {
	case "gitlab":
		token, err := s.resolveOAuthToken(ctx, &integration, false)
		if err == errUnauthorized {
			return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		repos, err := listGitLabRepos(integration.BaseURL, token, integration.Groups)
		if err == errUnauthorized {
			// Token was rejected despite looking valid — force refresh and retry once.
			token, err = s.resolveOAuthToken(ctx, &integration, true)
			if err == errUnauthorized {
				return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
			}
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			repos, err = listGitLabRepos(integration.BaseURL, token, integration.Groups)
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list GitLab repositories: " + err.Error())
		}
		return repos, nil
	case "gitea":
		token, err := s.resolveOAuthToken(ctx, &integration, false)
		if err == errUnauthorized {
			return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		repos, err := listGiteaRepos(integration.BaseURL, token, integration.Groups)
		if err == errUnauthorized {
			token, err = s.resolveOAuthToken(ctx, &integration, true)
			if err == errUnauthorized {
				return nil, huma.Error401Unauthorized("access token expired — please reconnect the integration")
			}
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			repos, err = listGiteaRepos(integration.BaseURL, token, integration.Groups)
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list Gitea repositories: " + err.Error())
		}
		return repos, nil
	}

	// GitHub App flow — credentials are on the integration row itself.
	if integration.GHAppID == "" || string(integration.InstallationID) == "" {
		return nil, huma.Error400BadRequest("GitHub App is not fully configured — complete setup and installation first")
	}
	token, err := getInstallationToken(integration.GHAppID, string(integration.GHPrivateKey), string(integration.InstallationID))
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get GitHub installation token: " + err.Error())
	}

	repos, err := fetchAllRepos(token)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list GitHub repositories: " + err.Error())
	}
	return repos, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildState creates a URL-safe state token: base64url("{id}:{hmac8hex}")
func buildState(id, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	sum := mac.Sum(nil)
	payload := fmt.Sprintf("%s:%x", id, sum[:8])
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// validateState decodes and verifies the state token, returns the id segment.
func validateState(state, secret string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", fmt.Errorf("malformed state")
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("malformed state format")
	}
	id := parts[0]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	expected := fmt.Sprintf("%x", mac.Sum(nil)[:8])
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", fmt.Errorf("state signature mismatch")
	}
	return id, nil
}

// fetchGitHubOrgID returns the numeric GitHub ID for a given org login name.
func fetchGitHubOrgID(orgName string) (int64, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/orgs/"+url.PathEscape(orgName), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("org %q not found on GitHub", orgName)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.ID, nil
}

// generateAppJWT creates a short-lived RS256 JWT signed with the GitHub App private key.
func generateAppJWT(appID, privateKeyPEM string) (string, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

// getInstallationToken exchanges the App JWT for a short-lived installation access token.
func getInstallationToken(appID, privateKeyPEM, installationID string) (string, error) {
	appJWT, err := generateAppJWT(appID, privateKeyPEM)
	if err != nil {
		return "", err
	}
	reqURL := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installationID)
	req, _ := http.NewRequest(http.MethodPost, reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub returned %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.Token, nil
}

// fetchInstallationLogin calls the GitHub API to get the account login name for an installation.
func fetchInstallationLogin(installationID, appJWT string) (string, error) {
	reqURL := fmt.Sprintf("https://api.github.com/app/installations/%s", installationID)
	req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Account.Login, nil
}

// fetchAllRepos paginates GET /installation/repositories and returns all repos.
func fetchAllRepos(token string) ([]GitRepo, error) {
	var all []GitRepo
	page := 1
	for {
		reqURL := fmt.Sprintf("https://api.github.com/installation/repositories?per_page=100&page=%d", page)
		req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var result struct {
			Repositories []struct {
				FullName      string `json:"full_name"`
				DefaultBranch string `json:"default_branch"`
				Private       bool   `json:"private"`
			} `json:"repositories"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		for _, r := range result.Repositories {
			all = append(all, GitRepo{
				FullName:      r.FullName,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
			})
		}
		if len(result.Repositories) < 100 {
			break
		}
		page++
	}
	return all, nil
}

// ─── Shared auth helpers ──────────────────────────────────────────────────────

// validateGitToken sends a GET to userURL with "Authorization: {scheme} {token}"
// and returns an error if the response is not 200.
func validateGitToken(userURL, scheme, token string) error {
	req, _ := http.NewRequest(http.MethodGet, userURL, nil)
	req.Header.Set("Authorization", scheme+" "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("unauthorized — check the token and its scopes")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

type oauthTokenResult struct {
	AccessToken  string
	RefreshToken string
	Expiry       *time.Time // nil if provider doesn't send expires_in
}

// exchangeOAuthCode exchanges an OAuth2 authorization code for an access token.
// Works for both GitLab (/oauth/token) and Gitea (/login/oauth/access_token).
func exchangeOAuthCode(tokenURL, clientID, clientSecret, code, redirectURI string) (oauthTokenResult, error) {
	body := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	return doTokenRequest(tokenURL, body)
}

// refreshOAuthToken uses a refresh token to obtain a new access token.
func refreshOAuthToken(tokenURL, clientID, clientSecret, refreshToken string) (oauthTokenResult, error) {
	body := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	return doTokenRequest(tokenURL, body)
}

func doTokenRequest(tokenURL string, body url.Values) (oauthTokenResult, error) {
	req, _ := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthTokenResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return oauthTokenResult{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"` // seconds; 0 = not provided
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return oauthTokenResult{}, fmt.Errorf("decode token response: %w", err)
	}
	if result.Error != "" {
		return oauthTokenResult{}, fmt.Errorf("OAuth error: %s", result.Error)
	}
	if result.AccessToken == "" {
		return oauthTokenResult{}, fmt.Errorf("empty access token in response")
	}
	var expiry *time.Time
	if result.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
		expiry = &t
	}
	return oauthTokenResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Expiry:       expiry,
	}, nil
}

// errUnauthorized is returned by provider helpers when the API responds with
// 401/403. The caller can detect this and attempt a token refresh + retry.
var errUnauthorized = fmt.Errorf("unauthorized")

// ─── GitLab/Gitea provider helpers ───────────────────────────────────────────

func gitLabBase(baseURL string) string {
	if baseURL == "" {
		return "https://gitlab.com"
	}
	return strings.TrimRight(baseURL, "/")
}

func listGitLabRepos(baseURL, token, group string) ([]GitRepo, error) {
	base := gitLabBase(baseURL)
	var all []GitRepo
	page := 1
	for {
		var apiURL string
		if group != "" {
			apiURL = fmt.Sprintf("%s/api/v4/groups/%s/projects?per_page=100&page=%d&include_subgroups=true",
				base, url.PathEscape(group), page)
		} else {
			apiURL = fmt.Sprintf("%s/api/v4/projects?membership=true&per_page=100&page=%d", base, page)
		}
		req, _ := http.NewRequest(http.MethodGet, apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, errUnauthorized
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitLab API returned %d: %s", resp.StatusCode, string(raw))
		}
		var result []struct {
			PathWithNamespace string `json:"path_with_namespace"`
			DefaultBranch     string `json:"default_branch"`
			Visibility        string `json:"visibility"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		for _, r := range result {
			all = append(all, GitRepo{
				FullName:      r.PathWithNamespace,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Visibility != "public",
			})
		}
		if len(result) < 100 {
			break
		}
		page++
	}
	return all, nil
}

func listGitLabBranches(baseURL, token, projectPath string) ([]string, error) {
	base := gitLabBase(baseURL)
	encoded := strings.ReplaceAll(projectPath, "/", "%2F")
	var all []string
	page := 1
	for {
		reqURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/branches?per_page=100&page=%d", base, encoded, page)
		req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, errUnauthorized
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitLab API returned %d: %s", resp.StatusCode, string(raw))
		}
		var result []struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		for _, b := range result {
			all = append(all, b.Name)
		}
		if len(result) < 100 {
			break
		}
		page++
	}
	return all, nil
}

func listGiteaRepos(baseURL, token, org string) ([]GitRepo, error) {
	base := strings.TrimRight(baseURL, "/")
	var all []GitRepo
	page := 1
	for {
		var apiURL string
		if org != "" {
			apiURL = fmt.Sprintf("%s/api/v1/orgs/%s/repos?limit=50&page=%d", base, url.PathEscape(org), page)
		} else {
			apiURL = fmt.Sprintf("%s/api/v1/repos/search?limit=50&page=%d", base, page)
		}
		req, _ := http.NewRequest(http.MethodGet, apiURL, nil)
		req.Header.Set("Authorization", "token "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, errUnauthorized
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Gitea API returned %d: %s", resp.StatusCode, string(raw))
		}
		var result struct {
			Data []struct {
				FullName      string `json:"full_name"`
				DefaultBranch string `json:"default_branch"`
				Private       bool   `json:"private"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		for _, r := range result.Data {
			all = append(all, GitRepo{
				FullName:      r.FullName,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
			})
		}
		if len(result.Data) < 50 {
			break
		}
		page++
	}
	return all, nil
}

func listGiteaBranches(baseURL, pat, repo string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	var all []string
	page := 1
	for {
		reqURL := fmt.Sprintf("%s/api/v1/repos/%s/branches?limit=50&page=%d", base, repo, page)
		req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "token "+pat)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, errUnauthorized
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Gitea API returned %d: %s", resp.StatusCode, string(raw))
		}
		var result []struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		for _, b := range result {
			all = append(all, b.Name)
		}
		if len(result) < 50 {
			break
		}
		page++
	}
	return all, nil
}

// parseRSAPrivateKey parses a PKCS1 or PKCS8 PEM-encoded RSA private key.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
}
