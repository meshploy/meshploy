import type { Node, Project } from "@/types"

declare global {
  interface Window {
    __MESHPLOY_CONFIG__?: { apiUrl: string }
  }
}

// In production: BASE is "" — all paths (/api/v1/...) are relative, Caddy routes /api/* to port 4000.
// In dev: BASE is "http://localhost:4000" so the full path becomes http://localhost:4000/api/v1/...
const BASE =
  window.__MESHPLOY_CONFIG__?.apiUrl ??
  import.meta.env.VITE_API_URL ??
  "http://localhost:4000"

// ─── Error ────────────────────────────────────────────────────────────────────

export class ApiError extends Error {
  constructor(
    public status: number,
    public detail: string,
    public title?: string
  ) {
    super(detail)
    this.name = "ApiError"
  }
}

// ─── Core fetch ───────────────────────────────────────────────────────────────

async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
  token?: string | null
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  }
  if (token) headers["Authorization"] = `Bearer ${token}`

  const res = await fetch(`${BASE}${path}`, { ...options, headers })

  if (!res.ok) {
    // Huma returns RFC 7807 problem details
    let detail = res.statusText
    let title: string | undefined
    try {
      const body = await res.json()
      detail = body.detail ?? body.message ?? detail
      title = body.title
    } catch {}
    throw new ApiError(res.status, detail, title)
  }

  // 204 No Content
  if (res.status === 204) return undefined as T

  return res.json() as Promise<T>
}

// ─── API response types (snake_case, matches Go JSON tags) ────────────────────

export interface ApiOrg {
  id: string
  name: string
  slug: string
  created_at: string
  updated_at: string
}

export interface ApiNode {
  id: string
  name: string
  tailscale_ip: string
  status: "online" | "offline"
  k3s_role: "server" | "agent"
  k3s_version: string
  k3s_labels: Record<string, string>
  cpu_cores: number
  memory_gb: number
  disk_gb: number
  last_seen_at: string | null
  organization_id: string
  created_at: string
  updated_at: string
  // Headscale peer data (zeroed when Headscale is not configured)
  headscale_id: string
  headscale_online: boolean
  headscale_last_seen: string | null
  headscale_expiry: string | null
  headscale_tags: string[]
  headscale_user: string
  headscale_fqdn: string
  // K8s cluster membership
  k8s_member: boolean
  k8s_ready: boolean
  k8s_node_name: string
  // Active project namespaces on this node
  active_projects: string[]
}

export interface ApiProject {
  id: string
  name: string
  slug: string
  organization_id: string
  created_at: string
  updated_at: string
}

// ─── Adapters (API → frontend types) ─────────────────────────────────────────

// parseTimestamp returns null for missing values and Go's zero time (year 1).
function parseTimestamp(s: string | null | undefined): Date | null {
  if (!s) return null
  const d = new Date(s)
  if (d.getFullYear() <= 1) return null
  return d
}

export function toNode(n: ApiNode): Node {
  return {
    id: n.id,
    name: n.name,
    tailscaleIP: n.tailscale_ip,
    status: n.status,
    k3sRole: n.k3s_role,
    k3sVersion: n.k3s_version,
    os: "",
    cpuCores: n.cpu_cores,
    memoryGB: n.memory_gb,
    diskGB: n.disk_gb,
    lastSeenAt: parseTimestamp(n.last_seen_at),
    organizationId: n.organization_id,
    headscaleId: n.headscale_id,
    headscaleOnline: n.headscale_online,
    headscaleLastSeen: parseTimestamp(n.headscale_last_seen),
    headscaleExpiry: parseTimestamp(n.headscale_expiry),
    headscaleTags: n.headscale_tags ?? [],
    headscaleUser: n.headscale_user,
    headscaleFQDN: n.headscale_fqdn ?? "",
    k8sMember: n.k8s_member,
    k8sReady: n.k8s_ready,
    k8sNodeName: n.k8s_node_name,
    activeProjects: n.active_projects ?? [],
  }
}

export function toProject(p: ApiProject): Project {
  return {
    id: p.id,
    name: p.name,
    slug: p.slug,
    organizationId: p.organization_id,
    servicesCount: 0,
    routesCount: 0,
    createdAt: new Date(p.created_at),
  }
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

export const auth = {
  login: (email: string, password: string) =>
    apiFetch<{ token: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),

  register: (username: string, email: string, password: string) =>
    apiFetch<{ id: string; username: string; email: string }>(
      "/api/v1/auth/register",
      {
        method: "POST",
        body: JSON.stringify({ username, email, password }),
      }
    ),
}

// ─── Orgs ─────────────────────────────────────────────────────────────────────

export const orgs = {
  list: (token: string) =>
    apiFetch<ApiOrg[]>("/api/v1/orgs", {}, token),
}

// ─── Nodes ────────────────────────────────────────────────────────────────────

export const nodes = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiNode[]>(`/api/v1/orgs/${orgId}/nodes`, {}, token),

  get: (orgId: string, nodeId: string, token: string) =>
    apiFetch<ApiNode>(`/api/v1/orgs/${orgId}/nodes/${nodeId}`, {}, token),

  getRegistrationToken: (orgId: string, token: string) =>
    apiFetch<{ token: string }>(`/api/v1/orgs/${orgId}/node-registration-token`, {}, token),

  generateRegistrationToken: (orgId: string, token: string) =>
    apiFetch<{ token: string }>(
      `/api/v1/orgs/${orgId}/node-registration-token`,
      { method: "POST" },
      token
    ),

  delete: (orgId: string, nodeId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/nodes/${nodeId}`, { method: "DELETE" }, token),
}

export interface ApiService {
  id: string
  name: string
  project_id: string
  node_id: string | null
  type: "application" | "database"
  status: "running" | "stopped" | "deploying" | "failed"
  image: string
  replicas: number
  cpu_request: string
  cpu_limit: string
  memory_request: string
  memory_limit: string
  created_at: string
  updated_at: string
}

export interface ApiDbRoute {
  id: string
  organization_id: string
  project_id: string
  service_id: string | null
  domain_id: string | null
  zone: "public" | "internal" | "preview"
  subdomain: string
  hostname: string
  target_ip: string
  target_port: number
  created_at: string
  updated_at: string
}

export interface ApiDomain {
  id: string
  organization_id: string
  base_domain: string
  internal_subdomain: string
  preview_subdomain: string
  verified: boolean
  created_at: string
  updated_at: string
}

// ─── Cluster ──────────────────────────────────────────────────────────────────

export const cluster = {
  getJoinToken: (token: string) =>
    apiFetch<{ token: string; server_url: string }>("/api/v1/cluster/join-token", {}, token),

  getHeadscalePreAuthKey: (token: string) =>
    apiFetch<{ has_active_key: boolean; key?: string; headscale_url: string }>(
      "/api/v1/cluster/headscale-preauth-key",
      {},
      token
    ),

  createHeadscalePreAuthKey: (token: string) =>
    apiFetch<{ key: string; reusable: boolean; expiration: string; headscale_url: string }>(
      "/api/v1/cluster/headscale-preauth-key",
      { method: "POST" },
      token
    ),
}

// ─── Projects ─────────────────────────────────────────────────────────────────

export const projects = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiProject[]>(`/api/v1/orgs/${orgId}/projects`, {}, token),

  get: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiProject>(`/api/v1/orgs/${orgId}/projects/${projectId}`, {}, token),

  create: (orgId: string, name: string, slug: string, token: string) =>
    apiFetch<ApiProject>(
      `/api/v1/orgs/${orgId}/projects`,
      { method: "POST", body: JSON.stringify({ name, slug }) },
      token
    ),
}

// ─── Services ─────────────────────────────────────────────────────────────────

export const services = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiService[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services`,
      {},
      token
    ),
}

// ─── Routes ───────────────────────────────────────────────────────────────────

export const routes = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiDbRoute[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes`,
      {},
      token
    ),

  create: (
    orgId: string,
    projectId: string,
    body: {
      domain_id?: string
      zone: string
      subdomain: string
      hostname?: string
      target_ip: string
      target_port: number
      service_id?: string
    },
    token: string
  ) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}`,
      { method: "DELETE" },
      token
    ),
}

// ─── GitHub App (platform-wide setup) ────────────────────────────────────────

export const gitHubApp = {
  status: () =>
    apiFetch<{ configured: boolean; app_slug: string }>("/api/v1/github/app-status"),

  manifestSetup: () =>
    apiFetch<{ github_url: string; manifest: string }>("/api/v1/github/manifest-setup"),
}

// ─── Git Integrations ─────────────────────────────────────────────────────────

export interface ApiGitIntegration {
  id: string
  organization_id: string
  provider: string
  name: string
  base_url: string
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

  installUrl: (orgId: string, token: string) =>
    apiFetch<{ url: string }>(`/api/v1/orgs/${orgId}/git-integrations/github/install-url`, {}, token),

  repos: (orgId: string, id: string, token: string) =>
    apiFetch<GitRepo[]>(`/api/v1/orgs/${orgId}/git-integrations/${id}/repos`, {}, token),

  branches: (orgId: string, id: string, repo: string, token: string) =>
    apiFetch<string[]>(`/api/v1/orgs/${orgId}/git-integrations/${id}/branches?repo=${encodeURIComponent(repo)}`, {}, token),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/git-integrations/${id}`, { method: "DELETE" }, token),
}

// ─── Domains ──────────────────────────────────────────────────────────────────

export const domains = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiDomain[]>(`/api/v1/orgs/${orgId}/domains`, {}, token),

  create: (
    orgId: string,
    body: { base_domain: string; internal_subdomain?: string; preview_subdomain?: string },
    token: string
  ) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  get: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(`/api/v1/orgs/${orgId}/domains/${domainId}`, {}, token),

  update: (
    orgId: string,
    domainId: string,
    body: { internal_subdomain?: string; preview_subdomain?: string },
    token: string
  ) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains/${domainId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, domainId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/domains/${domainId}`,
      { method: "DELETE" },
      token
    ),

  verify: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains/${domainId}/verify`,
      { method: "POST" },
      token
    ),
}
