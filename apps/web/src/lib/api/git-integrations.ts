import { apiFetch } from "./core"

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
    body: { provider: "gitlab" | "gitea"; name: string; base_url?: string; groups?: string; token: string },
    authToken: string
  ) =>
    apiFetch<ApiGitIntegration>(
      `/api/v1/orgs/${orgId}/git-integrations`,
      { method: "POST", body: JSON.stringify(body) },
      authToken
    ),

  initOAuth: (
    orgId: string,
    body: { provider: "gitlab" | "gitea"; name: string; base_url?: string; groups?: string; redirect_uri: string; client_id: string; client_secret: string },
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

  branches: (orgId: string, id: string, repo: string, token: string) =>
    apiFetch<string[]>(`/api/v1/orgs/${orgId}/git-integrations/${id}/branches?repo=${encodeURIComponent(repo)}`, {}, token),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/git-integrations/${id}`, { method: "DELETE" }, token),
}
