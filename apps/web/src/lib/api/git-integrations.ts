import { apiFetch } from "./core"

// The providers connected with a token or an OAuth app. GitHub is not one of
// them: it is a GitHub App, with its own flow.
export type TokenGitProvider = "gitlab" | "gitea" | "bitbucket"

// PushHook is where a provider delivers pushes for an integration, and the
// secret it signs them with, for setting a hook up by hand.
export interface PushHook {
  // github: the App delivers, and the question is whether it covers the repo.
  provider: string
  url: string
  secret: string
  secret_field: string
  event: string
  // What was found on the repository: installed, missing, no_access,
  // unreachable, or unknown when no repository was asked about.
  state: "installed" | "missing" | "no_access" | "unreachable" | "unknown"
  reason?: string
}

export interface ApiGitIntegration {
  id: string
  organization_id: string
  provider: string
  auth_method: string
  name: string
  base_url: string
  gh_app_slug?: string
  groups?: string
  connected: boolean
  created_at: string
  updated_at: string
}

export interface GitRepo {
  full_name: string
  default_branch: string
  private: boolean
}

export const gitIntegrations = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiGitIntegration[]>(`/api/v1/orgs/${orgId}/git-integrations`, {}, token),

  initGitHub: (orgId: string, body: { github_org?: string }, token: string) =>
    apiFetch<{ integration: ApiGitIntegration; github_url: string; manifest: string }>(
      `/api/v1/orgs/${orgId}/git-integrations/github`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  createPAT: (
    orgId: string,
    body: { provider: TokenGitProvider; name: string; base_url?: string; groups?: string; token: string },
    authToken: string
  ) =>
    apiFetch<ApiGitIntegration>(
      `/api/v1/orgs/${orgId}/git-integrations`,
      { method: "POST", body: JSON.stringify(body) },
      authToken
    ),

  initOAuth: (
    orgId: string,
    body: { provider: TokenGitProvider; name: string; base_url?: string; groups?: string; redirect_uri: string; client_id: string; client_secret: string },
    authToken: string
  ) =>
    apiFetch<{ auth_url: string; redirect_uri: string }>(
      `/api/v1/orgs/${orgId}/git-integrations/oauth`,
      { method: "POST", body: JSON.stringify(body) },
      authToken
    ),

  installUrl: (orgId: string, integrationId: string, token: string, githubOrg?: string) =>
    apiFetch<{ url: string }>(
      `/api/v1/orgs/${orgId}/git-integrations/${integrationId}/install-url${githubOrg ? `?github_org=${encodeURIComponent(githubOrg)}` : ""}`,
      {},
      token
    ),

  oauthReconnect: (orgId: string, id: string, token: string) =>
    apiFetch<{ auth_url: string }>(
      `/api/v1/orgs/${orgId}/git-integrations/${id}/oauth-reconnect`,
      {},
      token
    ),

  repos: (orgId: string, id: string, token: string) =>
    apiFetch<GitRepo[]>(`/api/v1/orgs/${orgId}/git-integrations/${id}/repos`, {}, token),

  // Where the provider should deliver pushes, for adding a hook by hand when
  // the token cannot create one. Admin only: it carries the secret.
  pushHook: (orgId: string, id: string, token: string, repo?: string) =>
    apiFetch<PushHook>(
      `/api/v1/orgs/${orgId}/git-integrations/${id}/push-hook${repo ? `?repo=${encodeURIComponent(repo)}` : ""}`,
      {},
      token
    ),

  // Ask the provider to add the hook, for when the connection can manage them.
  installPushHook: (orgId: string, id: string, repo: string, token: string) =>
    apiFetch<PushHook>(
      `/api/v1/orgs/${orgId}/git-integrations/${id}/push-hook`,
      { method: "POST", body: JSON.stringify({ repo }) },
      token
    ),

  branches: (orgId: string, id: string, repo: string, token: string) =>
    apiFetch<string[]>(`/api/v1/orgs/${orgId}/git-integrations/${id}/branches?repo=${encodeURIComponent(repo)}`, {}, token),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/git-integrations/${id}`, { method: "DELETE" }, token),
}
