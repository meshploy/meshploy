package client

// ── Git integrations ──────────────────────────────────────────────────────────

type GitIntegration struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	AuthMethod string `json:"auth_method"`
	Connected  bool   `json:"connected"`
	GHAppSlug  string `json:"gh_app_slug,omitempty"`
}

type createPATBody struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url,omitempty"`
	Groups   string `json:"groups,omitempty"`
	Token    string `json:"token"`
}

type initOAuthBody struct {
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url,omitempty"`
	Groups       string `json:"groups,omitempty"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type initGitHubBody struct {
	GithubOrg string `json:"github_org,omitempty"`
}

type initGitHubOutput struct {
	Integration *GitIntegration `json:"integration"`
	GithubURL   string          `json:"github_url"`
	Manifest    string          `json:"manifest"`
}

type initOAuthOutput struct {
	AuthURL     string `json:"auth_url"`
	RedirectURI string `json:"redirect_uri"`
}

func (c *Client) ListGitIntegrations(orgID string) ([]GitIntegration, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/git-integrations", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]GitIntegration](resp)
}

func (c *Client) CreatePATIntegration(orgID, provider, name, baseURL, groups, token string) (*GitIntegration, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/git-integrations", createPATBody{
		Provider: provider, Name: name, BaseURL: baseURL, Groups: groups, Token: token,
	})
	if err != nil {
		return nil, err
	}
	return decodePtr[GitIntegration](resp)
}

func (c *Client) InitGitHubIntegration(orgID, githubOrg string) (initGitHubOutput, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/git-integrations/github", initGitHubBody{GithubOrg: githubOrg})
	if err != nil {
		return initGitHubOutput{}, err
	}
	return decode[initGitHubOutput](resp)
}

func (c *Client) InitOAuthIntegration(orgID, provider, name, baseURL, groups, redirectURI, clientID, clientSecret string) (string, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/git-integrations/oauth", initOAuthBody{
		Provider: provider, Name: name, BaseURL: baseURL, Groups: groups,
		RedirectURI: redirectURI, ClientID: clientID, ClientSecret: clientSecret,
	})
	if err != nil {
		return "", err
	}
	out, err := decode[initOAuthOutput](resp)
	return out.AuthURL, err
}

func (c *Client) DeleteGitIntegration(orgID, id string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/git-integrations/"+id)
}

// ── Registry integrations ─────────────────────────────────────────────────────

type RegistryIntegration struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Endpoint  string `json:"endpoint"`
}

type CreateRegistryBody struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

func (c *Client) ListRegistryIntegrations(orgID string) ([]RegistryIntegration, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/registry-integrations", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]RegistryIntegration](resp)
}

func (c *Client) CreateRegistryIntegration(orgID string, body CreateRegistryBody) (*RegistryIntegration, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/registry-integrations", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[RegistryIntegration](resp)
}

func (c *Client) DeleteRegistryIntegration(orgID, id string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/registry-integrations/"+id)
}

// ── Storage integrations ──────────────────────────────────────────────────────

type StorageIntegration struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	Bucket   string `json:"bucket"`
}

type CreateStorageBody struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint,omitempty"`
	Region          string `json:"region,omitempty"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

func (c *Client) ListStorageIntegrations(orgID string) ([]StorageIntegration, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/storage-integrations", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]StorageIntegration](resp)
}

func (c *Client) CreateStorageIntegration(orgID string, body CreateStorageBody) (*StorageIntegration, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/storage-integrations", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[StorageIntegration](resp)
}

func (c *Client) DeleteStorageIntegration(orgID, id string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/storage-integrations/"+id)
}

// ── Repo/branch helpers ───────────────────────────────────────────────────────

type RepoItem struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

func (c *Client) ListRepos(orgID, integrationID string) ([]RepoItem, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/git-integrations/"+integrationID+"/repos", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]RepoItem](resp)
}

func (c *Client) ListBranches(orgID, integrationID, repoURL string) ([]string, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/git-integrations/"+integrationID+"/branches?repo_url="+repoURL, nil)
	if err != nil {
		return nil, err
	}
	return decode[[]string](resp)
}
