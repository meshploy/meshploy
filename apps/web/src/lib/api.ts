import type { Node, Project } from "@/types"

const BASE = "http://localhost:4000"

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
    lastSeenAt: n.last_seen_at ? new Date(n.last_seen_at) : new Date(0),
    organizationId: n.organization_id,
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
  hostname: string
  target_ip: string
  target_port: number
  created_at: string
  updated_at: string
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
}
