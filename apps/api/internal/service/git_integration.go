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
	"gorm.io/gorm/clause"
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

// ─── Platform GitHub App config (manifest flow) ───────────────────────────────

// GetAppConfig loads the platform-wide GitHub App config from DB.
// Returns nil, nil if the manifest flow has not been completed yet.
func (s *GitIntegrationService) GetAppConfig(ctx context.Context) (*db.GitHubAppConfig, error) {
	var cfg db.GitHubAppConfig
	err := s.db.WithContext(ctx).First(&cfg).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load GitHub App config")
	}
	return &cfg, nil
}

// BuildManifestSetup returns the GitHub URL and manifest JSON the frontend needs
// to auto-submit the manifest form that creates the GitHub App.
func (s *GitIntegrationService) BuildManifestSetup(ctx context.Context, orgName string) (githubURL, manifest, state string, err error) {
	state = buildState("setup", s.cfg.JWTSecret)

	// Callbacks must go through the public-facing domain (Caddy terminates TLS
	// there and proxies /api/* to the API). Using APIBaseURL would point GitHub
	// at the raw API port which has no TLS.
	base := s.cfg.FrontendURL
	m := map[string]any{
		"name":          "Meshploy",
		"url":           base,
		"redirect_url":  base + "/api/v1/github/app-callback",
		"callback_urls": []string{base + "/api/v1/github/callback"},
		"setup_url":     base + "/api/v1/github/callback",
		"public":        false,
		"default_permissions": map[string]string{
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "read",
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal manifest: %w", err)
	}
	manifest = string(b)
	if orgName != "" {
		githubURL = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new?state=%s", orgName, state)
	} else {
		githubURL = fmt.Sprintf("https://github.com/settings/apps/new?state=%s", state)
	}
	return githubURL, manifest, state, nil
}

// HandleAppCallback exchanges the one-time code GitHub sends after manifest app creation,
// stores the resulting credentials in the DB (replacing any prior config).
func (s *GitIntegrationService) HandleAppCallback(ctx context.Context, code, state string) error {
	orgIDStr, err := validateState(state, s.cfg.JWTSecret)
	if err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}
	if orgIDStr != "setup" {
		return fmt.Errorf("unexpected state payload")
	}

	// Exchange code → app credentials.
	url := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(nil))
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

	row := db.GitHubAppConfig{
		AppID:         fmt.Sprintf("%d", result.ID),
		AppSlug:       result.Slug,
		ClientID:      result.ClientID,
		ClientSecret:  db.EncryptedString(result.ClientSecret),
		PrivateKey:    db.EncryptedString(result.PEM),
		WebhookSecret: db.EncryptedString(result.WebhookSecret),
	}

	// Singleton: delete any prior row, then insert fresh.
	if err := s.db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).
		Delete(&db.GitHubAppConfig{}).Error; err != nil {
		return fmt.Errorf("clear old config: %w", err)
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("save app config: %w", err)
	}
	return nil
}

// ─── Org-level installation ───────────────────────────────────────────────────

// List returns all git integrations for an org.
func (s *GitIntegrationService) List(ctx context.Context, orgID uuid.UUID) ([]db.GitIntegration, error) {
	rows := make([]db.GitIntegration, 0)
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&rows).Error; err != nil {
		return nil, huma.Error500InternalServerError("failed to list git integrations")
	}
	return rows, nil
}

// Delete removes an integration by ID.
func (s *GitIntegrationService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.db.WithContext(ctx).Delete(&db.GitIntegration{}, id).Error; err != nil {
		return huma.Error500InternalServerError("failed to delete git integration")
	}
	return nil
}

// GitHubInstallURL returns the GitHub App installation URL with a signed state parameter.
// If githubOrg is provided, fetches the org's numeric GitHub ID and appends suggested_target_id
// so GitHub pre-selects that org in the installation account picker.
// Returns 501 if the manifest flow has not been completed yet.
func (s *GitIntegrationService) GitHubInstallURL(ctx context.Context, orgID uuid.UUID, githubOrg string) (string, error) {
	appCfg, err := s.GetAppConfig(ctx)
	if err != nil {
		return "", err
	}
	if appCfg == nil {
		return "", huma.Error501NotImplemented("GitHub App is not set up — visit /integrations to complete setup")
	}
	state := buildState(orgID.String(), s.cfg.JWTSecret)
	installURL := fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", appCfg.AppSlug, state)
	if githubOrg != "" {
		targetID, err := fetchGitHubOrgID(githubOrg)
		if err != nil {
			return "", huma.Error400BadRequest("could not find GitHub org: " + err.Error())
		}
		installURL += fmt.Sprintf("&suggested_target_id=%d", targetID)
	}
	return installURL, nil
}

// HandleGitHubCallback validates the OAuth state, fetches installation metadata from GitHub,
// and upserts a GitIntegration row for the org.
func (s *GitIntegrationService) HandleGitHubCallback(ctx context.Context, installationID, state string) (*db.GitIntegration, error) {
	appCfg, err := s.GetAppConfig(ctx)
	if err != nil {
		return nil, err
	}
	if appCfg == nil {
		return nil, fmt.Errorf("GitHub App is not configured")
	}

	orgIDStr, err := validateState(state, s.cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid state: %w", err)
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid org ID in state")
	}

	appJWT, err := generateAppJWT(appCfg.AppID, string(appCfg.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to generate GitHub App JWT: %w", err)
	}
	accountLogin, err := fetchInstallationLogin(installationID, appJWT)
	if err != nil {
		accountLogin = "github-" + installationID
	}

	row := db.GitIntegration{
		OrganizationID: orgID,
		Provider:       "github",
		Name:           accountLogin,
		InstallationID: db.EncryptedString(installationID),
		BaseURL:        "",
	}

	result := s.db.WithContext(ctx).
		Where("organization_id = ? AND provider = ? AND installation_id = ?",
			orgID, "github", db.EncryptedString(installationID)).
		Assign(db.GitIntegration{Name: accountLogin}).
		FirstOrCreate(&row)
	if result.Error != nil {
		row.Base = db.Base{}
		if err2 := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&row).Error; err2 != nil {
			return nil, fmt.Errorf("failed to save git integration: %w", err2)
		}
	}
	return &row, nil
}

// ListBranches returns branch names for a specific repo in a git integration.
func (s *GitIntegrationService) ListBranches(ctx context.Context, integrationID uuid.UUID, repo string) ([]string, error) {
	var integration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&integration, integrationID).Error; err != nil {
		return nil, huma.Error404NotFound("git integration not found")
	}

	switch integration.Provider {
	case "gitlab":
		branches, err := listGitLabBranches(integration.BaseURL, string(integration.InstallationID), repo)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list GitLab branches: " + err.Error())
		}
		return branches, nil
	case "gitea":
		branches, err := listGiteaBranches(integration.BaseURL, string(integration.InstallationID), repo)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list Gitea branches: " + err.Error())
		}
		return branches, nil
	}

	// Default: GitHub App flow.
	appCfg, err := s.GetAppConfig(ctx)
	if err != nil {
		return nil, err
	}
	if appCfg == nil {
		return nil, huma.Error501NotImplemented("GitHub App is not configured")
	}
	token, err := getInstallationToken(appCfg.AppID, string(appCfg.PrivateKey), string(integration.InstallationID))
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get GitHub installation token: " + err.Error())
	}

	var all []string
	page := 1
	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/branches?per_page=100&page=%d", repo, page)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
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
	return &row, nil
}

// InitOAuthIntegration creates a pending GitLab/Gitea integration record and returns
// the OAuth authorization URL the user should be redirected to.
// InitOAuthIntegration creates a pending GitLab/Gitea integration record and returns
// the OAuth authorization URL the user should be redirected to.
// redirectURI must be the full public URL the provider will redirect back to — it is
// computed by the frontend (window.location.origin + /api/v1/{provider}/callback) so
// it always matches what the user registered in their GitLab/Gitea application.
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
	token, err := exchangeOAuthCode(base+"/oauth/token",
		integration.OAuthClientID, string(integration.OAuthClientSecret), code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&integration).
		Update("installation_id", db.EncryptedString(token)).Error; err != nil {
		return nil, fmt.Errorf("failed to persist access token")
	}
	integration.InstallationID = db.EncryptedString(token)
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
	token, err := exchangeOAuthCode(base+"/login/oauth/access_token",
		integration.OAuthClientID, string(integration.OAuthClientSecret), code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&integration).
		Update("installation_id", db.EncryptedString(token)).Error; err != nil {
		return nil, fmt.Errorf("failed to persist access token")
	}
	integration.InstallationID = db.EncryptedString(token)
	return &integration, nil
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
		repos, err := listGitLabRepos(integration.BaseURL, string(integration.InstallationID), integration.Groups)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list GitLab repositories: " + err.Error())
		}
		return repos, nil
	case "gitea":
		repos, err := listGiteaRepos(integration.BaseURL, string(integration.InstallationID), integration.Groups)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list Gitea repositories: " + err.Error())
		}
		return repos, nil
	}

	// Default: GitHub App flow.
	appCfg, err := s.GetAppConfig(ctx)
	if err != nil {
		return nil, err
	}
	if appCfg == nil {
		return nil, huma.Error501NotImplemented("GitHub App is not configured")
	}

	token, err := getInstallationToken(appCfg.AppID, string(appCfg.PrivateKey), string(integration.InstallationID))
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
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installationID)
	req, _ := http.NewRequest(http.MethodPost, url, nil)
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
	url := fmt.Sprintf("https://api.github.com/app/installations/%s", installationID)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
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
		url := fmt.Sprintf("https://api.github.com/installation/repositories?per_page=100&page=%d", page)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
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

// exchangeOAuthCode exchanges an OAuth2 authorization code for an access token.
// Works for both GitLab (/oauth/token) and Gitea (/login/oauth/access_token).
func exchangeOAuthCode(tokenURL, clientID, clientSecret, code, redirectURI string) (string, error) {
	body := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	req, _ := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("OAuth error: %s", result.Error)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}
	return result.AccessToken, nil
}

// ─── GitLab helpers ───────────────────────────────────────────────────────────

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
			// Scope to a specific group (includes subgroups via with_shared=false&include_subgroups=true)
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
		url := fmt.Sprintf("%s/api/v4/projects/%s/repository/branches?per_page=100&page=%d", base, encoded, page)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
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

// ─── Gitea helpers ────────────────────────────────────────────────────────────

func listGiteaRepos(baseURL, token, org string) ([]GitRepo, error) {
	base := strings.TrimRight(baseURL, "/")
	var all []GitRepo
	page := 1
	for {
		var apiURL string
		if org != "" {
			// Scope to a specific organization
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
		url := fmt.Sprintf("%s/api/v1/repos/%s/branches?limit=50&page=%d", base, repo, page)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "token "+pat)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
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
