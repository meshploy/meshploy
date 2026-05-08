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

export interface ApiOrgMember {
  id: string
  user_id: string
  role: "owner" | "admin" | "member"
  user_name: string
  user_email: string
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
  // Public internet IP (gateway/server nodes only)
  public_ip: string
}

export interface ApiProject {
  id: string
  name: string
  slug: string
  organization_id: string
  created_at: string
  updated_at: string
  // Resource counts — embedded by the list endpoint (single SQL aggregation).
  // Extend ProjectCounts in the Go service layer when adding new resource types.
  services_count: number
  databases_count: number
  routes_count: number
  secrets_count: number
  jobs_count: number
  stacks_count: number
  volumes_count: number
}

export interface ApiStack {
  id: string
  project_id: string
  name: string
  spec: string
  status: "idle" | "applying" | "failed"
  last_applied_at: string | null
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
    servicesCount: p.services_count ?? 0,
    databasesCount: p.databases_count ?? 0,
    routesCount: p.routes_count ?? 0,
    secretsCount: p.secrets_count ?? 0,
    jobsCount: p.jobs_count ?? 0,
    stacksCount: p.stacks_count ?? 0,
    volumesCount: p.volumes_count ?? 0,
    createdAt: new Date(p.created_at),
  }
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

export const auth = {
  login: (email: string, password: string) =>
    apiFetch<{ token?: string; totp_required?: boolean; mfa_token?: string }>(
      "/api/v1/auth/login",
      { method: "POST", body: JSON.stringify({ email, password }) }
    ),

  completeTOTPLogin: (mfaToken: string, code: string) =>
    apiFetch<{ token: string }>("/api/v1/auth/totp", {
      method: "POST",
      body: JSON.stringify({ mfa_token: mfaToken, code }),
    }),

  register: (username: string, email: string, password: string) =>
    apiFetch<{ id: string; username: string; email: string }>(
      "/api/v1/auth/register",
      { method: "POST", body: JSON.stringify({ username, email, password }) }
    ),

  getMe: (token: string) =>
    apiFetch<{ id: string; username: string; email: string; totp_enabled: boolean }>(
      "/api/v1/me", {}, token
    ),

  setupTOTP: (token: string) =>
    apiFetch<{ otp_url: string; secret: string }>(
      "/api/v1/me/totp/setup", { method: "POST" }, token
    ),

  enableTOTP: (code: string, token: string) =>
    apiFetch<void>("/api/v1/me/totp/enable", {
      method: "POST",
      body: JSON.stringify({ code }),
    }, token),

  disableTOTP: (code: string, token: string) =>
    apiFetch<void>("/api/v1/me/totp", {
      method: "DELETE",
      body: JSON.stringify({ code }),
    }, token),
}

// ─── Orgs ─────────────────────────────────────────────────────────────────────

export const orgs = {
  list: (token: string) =>
    apiFetch<ApiOrg[]>("/api/v1/orgs", {}, token),

  get: (orgId: string, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}`, {}, token),

  update: (orgId: string, name: string, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}`, { method: "PATCH", body: JSON.stringify({ name }) }, token),

  listMembers: (orgId: string, token: string) =>
    apiFetch<ApiOrgMember[]>(`/api/v1/orgs/${orgId}/members`, {}, token),

  addMember: (orgId: string, email: string, role: "admin" | "member", token: string) =>
    apiFetch<ApiOrgMember>(`/api/v1/orgs/${orgId}/members`, { method: "POST", body: JSON.stringify({ email, role }) }, token),

  removeMember: (orgId: string, userId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/members/${userId}`, { method: "DELETE" }, token),
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
  port: number
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

// ─── Registry Integrations ────────────────────────────────────────────────────

export type RegistryProvider = "ghcr" | "dockerhub" | "ecr" | "gcr" | "custom" | "builtin"

export interface ApiRegistryIntegration {
  id: string
  organization_id: string
  name: string
  provider: RegistryProvider
  endpoint: string
  namespace: string
  created_at: string
  updated_at: string
}

export interface CreateRegistryBody {
  name: string
  provider: RegistryProvider
  endpoint?: string
  namespace?: string
  username: string
  password: string
}

export const registries = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiRegistryIntegration[]>(
      `/api/v1/orgs/${orgId}/registry-integrations`,
      {},
      token
    ),

  create: (orgId: string, body: CreateRegistryBody, token: string) =>
    apiFetch<ApiRegistryIntegration>(
      `/api/v1/orgs/${orgId}/registry-integrations`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/registry-integrations/${id}`,
      { method: "DELETE" },
      token
    ),
}

// ─── Storage integrations ─────────────────────────────────────────────────────

export type StorageProvider = "s3" | "r2" | "minio" | "b2"

export interface ApiStorageIntegration {
  id: string
  organization_id: string
  name: string
  provider: StorageProvider
  endpoint: string
  region: string
  bucket: string
  created_at: string
  updated_at: string
}

export interface CreateStorageBody {
  name: string
  provider: StorageProvider
  endpoint?: string
  region?: string
  bucket: string
  access_key_id: string
  secret_access_key: string
}

export const storage = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiStorageIntegration[]>(
      `/api/v1/orgs/${orgId}/storage-integrations`,
      {},
      token
    ),

  create: (orgId: string, body: CreateStorageBody, token: string) =>
    apiFetch<ApiStorageIntegration>(
      `/api/v1/orgs/${orgId}/storage-integrations`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/storage-integrations/${id}`,
      { method: "DELETE" },
      token
    ),
}

// ─── Backup configs ───────────────────────────────────────────────────────────

export interface ApiBackupConfig {
  id: string
  service_id: string
  storage_integration_id: string
  schedule: string
  retention_days: number
  path_prefix: string
  enabled: boolean
  last_backup_at: string | null
  last_backup_status: "pending" | "running" | "success" | "failed" | null
  created_at: string
  updated_at: string
}

export interface ApiSystemBackupConfig {
  id: string
  organization_id: string
  storage_integration_id: string
  schedule: string
  retention_days: number
  path_prefix: string
  enabled: boolean
  last_backup_at: string | null
  last_backup_status: "pending" | "running" | "success" | "failed" | null
  created_at: string
  updated_at: string
}

export interface CreateBackupBody {
  storage_integration_id: string
  schedule: string
  retention_days?: number
  path_prefix?: string
}

export interface UpdateBackupBody {
  schedule?: string
  retention_days?: number
  path_prefix?: string
  enabled?: boolean
}

export const backups = {
  list: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiBackupConfig[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, serviceId: string, body: CreateBackupBody, token: string) =>
    apiFetch<ApiBackupConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, serviceId: string, id: string, body: UpdateBackupBody, token: string) =>
    apiFetch<ApiBackupConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups/${id}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, serviceId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups/${id}`,
      { method: "DELETE" },
      token
    ),

  getSystem: (orgId: string, token: string) =>
    apiFetch<ApiSystemBackupConfig | null>(
      `/api/v1/orgs/${orgId}/system-backup`,
      {},
      token
    ),

  upsertSystem: (orgId: string, body: { storage_integration_id: string; schedule: string; retention_days?: number; path_prefix?: string; enabled: boolean }, token: string) =>
    apiFetch<ApiSystemBackupConfig>(
      `/api/v1/orgs/${orgId}/system-backup`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  deleteSystem: (orgId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/system-backup`,
      { method: "DELETE" },
      token
    ),
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

  update: (orgId: string, projectId: string, name: string, token: string) =>
    apiFetch<ApiProject>(
      `/api/v1/orgs/${orgId}/projects/${projectId}`,
      { method: "PATCH", body: JSON.stringify({ name }) },
      token
    ),

  delete: (orgId: string, projectId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}`,
      { method: "DELETE" },
      token
    ),

  clearBuildCache: (orgId: string, projectId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/build-cache`,
      { method: "DELETE" },
      token
    ),
}

// ─── Services ─────────────────────────────────────────────────────────────────

export interface ApiDeployment {
  id: string
  service_id: string
  status: "pending" | "building" | "deploying" | "running" | "success" | "failed"
  image: string
  build_job_name: string
  log: string
  deployed_at: string | null
  created_at: string
  updated_at: string
}

export interface CreateServiceBody {
  port?: number
  name: string
  image?: string
  node_id?: string
  replicas?: number
  cpu_request?: string
  cpu_limit?: string
  memory_request?: string
  memory_limit?: string
  env_vars?: string
  // Build config — a BuildConfig row is created server-side when git_repo is set
  git_integration_id?: string
  git_repo?: string
  branch?: string
  builder?: "nixpacks" | "railpack" | "dockerfile"
  dockerfile_path?: string
  registry_integration_id?: string
  builder_node?: string          // "" = auto-schedule
  builder_cpu_request?: string   // "" = default (1000m)
  builder_memory_request?: string // "" = default (1Gi)
  // Database-specific fields
  type?: "application" | "database"
  engine?: "postgres" | "mysql" | "redis" | "mongodb" | "dragonfly" | "clickhouse"
  version?: string
  storage_gb?: number
  db_name?: string
  db_user?: string
  db_password?: string
}

export const services = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiService[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services`,
      {},
      token
    ),

  get: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiService>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, body: CreateServiceBody, token: string) =>
    apiFetch<ApiService>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, serviceId: string, body: UpdateServiceBody, token: string) =>
    apiFetch<ApiService>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  getEnvVars: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<{ env_vars: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/env-vars`,
      {},
      token
    ),

  delete: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}`,
      { method: "DELETE" },
      token
    ),

  start: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiService>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/start`,
      { method: "POST" },
      token
    ),

  stop: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiService>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/stop`,
      { method: "POST" },
      token
    ),

  getDatabaseConfig: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiDatabaseConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/database-config`,
      {},
      token
    ),

  reset: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiDeployment>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/reset`,
      { method: "POST" },
      token
    ),

  dbSchema: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiSchemaTable[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/db/schema`,
      {},
      token
    ),

  dbQuery: (orgId: string, projectId: string, serviceId: string, query: string, readOnly: boolean, token: string) =>
    apiFetch<ApiQueryResult>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/db/query`,
      { method: "POST", body: JSON.stringify({ query, read_only: readOnly }) },
      token
    ),
}

export interface ApiSchemaColumn {
  name: string
  data_type: string
  nullable: boolean
}

export interface ApiSchemaTable {
  name: string
  columns: ApiSchemaColumn[]
}

export interface ApiQueryResult {
  columns: string[]
  rows: string[][]
  count: number
}

export interface ApiDatabaseConfig {
  id: string
  service_id: string
  engine: "postgres" | "mysql" | "redis" | "mongodb" | "dragonfly" | "clickhouse"
  version: string
  storage_gb: number
  slug: string
  db_name: string
  db_user: string
  db_password: string
  node_port: number
}

export interface ApiBuildConfig {
  id: string
  service_id: string
  builder: "nixpacks" | "railpack" | "dockerfile" | "image"
  git_repo: string
  branch: string
  dockerfile_path: string
  registry_integration_id: string | null
  git_integration_id: string | null
  builder_node: string
  builder_cpu_request: string
  builder_memory_request: string
  last_built_image: string
  last_built_at: string | null
  rollback_enabled: boolean
  image_retention: number
  created_at: string
  updated_at: string
}

export interface UpdateServiceBody {
  name?: string
  image?: string
  node_id?: string     // "" = auto-schedule, UUID = pin to node
  replicas?: number
  port?: number
  cpu_request?: string
  cpu_limit?: string
  memory_request?: string
  memory_limit?: string
  env_vars?: string
}

export interface UpdateBuildConfigBody {
  git_repo?: string
  branch?: string
  builder?: "nixpacks" | "railpack" | "dockerfile"
  dockerfile_path?: string
  registry_integration_id?: string  // "" = clear
  build_env_vars?: string           // nil = no change; "" = clear
  git_integration_id?: string
  builder_node?: string             // "" = auto-schedule, node name = pin
  builder_cpu_request?: string      // "" = default (1000m)
  builder_memory_request?: string   // "" = default (1Gi)
  rollback_enabled?: boolean
  image_retention?: number
}

export const buildConfigs = {
  get: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiBuildConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/build-config`,
      {},
      token
    ),

  update: (orgId: string, projectId: string, serviceId: string, body: UpdateBuildConfigBody, token: string) =>
    apiFetch<ApiBuildConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/build-config`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  getBuildEnvVars: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<{ build_env_vars: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/build-config/env-vars`,
      {},
      token
    ),

  putBuildEnvVars: (orgId: string, projectId: string, serviceId: string, buildEnvVars: string, token: string) =>
    apiFetch<{ build_env_vars: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/build-config/env-vars`,
      { method: "PUT", body: JSON.stringify({ build_env_vars: buildEnvVars }) },
      token
    ),
}

export const deployments = {
  list: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiDeployment[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments`,
      {},
      token
    ),

  trigger: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiDeployment>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments`,
      { method: "POST" },
      token
    ),

  get: (orgId: string, projectId: string, serviceId: string, deploymentId: string, token: string) =>
    apiFetch<ApiDeployment>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments/${deploymentId}`,
      {},
      token
    ),

  cancel: (orgId: string, projectId: string, serviceId: string, deploymentId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments/${deploymentId}`,
      { method: "DELETE" },
      token
    ),

  deleteRecord: (orgId: string, projectId: string, serviceId: string, deploymentId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments/${deploymentId}/record`,
      { method: "DELETE" },
      token
    ),

  rollback: (orgId: string, projectId: string, serviceId: string, deploymentId: string, token: string) =>
    apiFetch<ApiDeployment>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments/${deploymentId}/rollback`,
      { method: "POST" },
      token
    ),

  retry: (orgId: string, projectId: string, serviceId: string, deploymentId: string, token: string) =>
    apiFetch<ApiDeployment>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments/${deploymentId}/retry`,
      { method: "POST" },
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
      subdomain?: string
      hostname?: string
      service_id?: string
      node_id?: string
      port?: number
      target_ip?: string
      target_port?: number
    },
    token: string
  ) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  get: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}`,
      {},
      token
    ),

  update: (
    orgId: string,
    projectId: string,
    routeId: string,
    body: { service_id?: string | null; target_ip: string; target_port: number },
    token: string
  ) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}`,
      { method: "DELETE" },
      token
    ),

  syncIP: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/sync`,
      { method: "POST" },
      token
    ),
}

// ─── Git Integrations ─────────────────────────────────────────────────────────

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

  verify: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains/${domainId}/verify`,
      { method: "POST" },
      token
    ),
}

// ─── Secrets ──────────────────────────────────────────────────────────────────

export interface ApiSecret {
  id: string
  project_id: string
  name: string
  has_value: boolean
  created_at: string
  updated_at: string
}

export interface ApiSecretAttachment {
  id: string
  service_id: string
  secret_id: string
  secret_name: string
  env_key: string
}

export const secrets = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiSecret[]>(`/api/v1/orgs/${orgId}/projects/${projectId}/secrets`, {}, token),

  create: (orgId: string, projectId: string, body: { name: string; value: string }, token: string) =>
    apiFetch<ApiSecret>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/secrets`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, secretId: string, value: string, token: string) =>
    apiFetch<ApiSecret>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/secrets/${secretId}`,
      { method: "PATCH", body: JSON.stringify({ value }) },
      token
    ),

  delete: (orgId: string, projectId: string, secretId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/secrets/${secretId}`,
      { method: "DELETE" },
      token
    ),

  listAttachments: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiSecretAttachment[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/secret-attachments`,
      {},
      token
    ),

  attach: (orgId: string, projectId: string, serviceId: string, body: { secret_id: string; env_key: string }, token: string) =>
    apiFetch<ApiSecretAttachment>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/secret-attachments`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  detach: (orgId: string, projectId: string, serviceId: string, attachmentId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/secret-attachments/${attachmentId}`,
      { method: "DELETE" },
      token
    ),
}

// ─── Jobs ─────────────────────────────────────────────────────────────────────

export interface ApiJob {
  id: string
  project_id: string
  node_id: string | null
  name: string
  is_cron: boolean
  image: string
  command: string
  schedule: string
  concurrency_policy: string
  history_limit: number
  cpu_request: string
  cpu_limit: string
  memory_request: string
  memory_limit: string
  status: string
  last_run_at: string | null
  k8s_name: string
  created_at: string
  updated_at: string
}

export interface ApiJobRun {
  id: string
  job_id: string
  status: string
  started_at: string | null
  finished_at: string | null
  log: string
  k8s_job_name: string
  created_at: string
}

export interface CreateJobBody {
  name: string
  is_cron: boolean
  image: string
  command?: string
  schedule?: string
  concurrency_policy?: string
  history_limit?: number
  cpu_request?: string
  cpu_limit?: string
  memory_request?: string
  memory_limit?: string
  env_vars?: string
  node_id?: string
}

export const jobs = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiJob[]>(`/api/v1/orgs/${orgId}/projects/${projectId}/jobs`, {}, token),

  get: (orgId: string, projectId: string, jobId: string, token: string) =>
    apiFetch<ApiJob>(`/api/v1/orgs/${orgId}/projects/${projectId}/jobs/${jobId}`, {}, token),

  create: (orgId: string, projectId: string, body: CreateJobBody, token: string) =>
    apiFetch<ApiJob>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/jobs`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, jobId: string, body: Partial<CreateJobBody>, token: string) =>
    apiFetch<ApiJob>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/jobs/${jobId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, jobId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/jobs/${jobId}`,
      { method: "DELETE" },
      token
    ),

  listRuns: (orgId: string, projectId: string, jobId: string, token: string) =>
    apiFetch<ApiJobRun[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/jobs/${jobId}/runs`,
      {},
      token
    ),

  trigger: (orgId: string, projectId: string, jobId: string, token: string) =>
    apiFetch<ApiJobRun>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/jobs/${jobId}/trigger`,
      { method: "POST" },
      token
    ),
}

// ─── Notification channels ────────────────────────────────────────────────────

export type NotificationChannelType = "email" | "webhook"

export interface ApiNotificationChannel {
  id: string
  name: string
  type: NotificationChannelType
  config: Record<string, string>
  events: string[]
  enabled: boolean
  created_at: string
}

export interface CreateNotificationBody {
  name: string
  type: NotificationChannelType
  config: Record<string, string>
  events: string[]
}

export const notifications = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiNotificationChannel[]>(
      `/api/v1/orgs/${orgId}/notification-channels`,
      {},
      token
    ),

  create: (orgId: string, body: CreateNotificationBody, token: string) =>
    apiFetch<ApiNotificationChannel>(
      `/api/v1/orgs/${orgId}/notification-channels`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, id: string, body: Partial<CreateNotificationBody> & { enabled?: boolean }, token: string) =>
    apiFetch<ApiNotificationChannel>(
      `/api/v1/orgs/${orgId}/notification-channels/${id}`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/notification-channels/${id}`,
      { method: "DELETE" },
      token
    ),
}

// ─── Stacks ───────────────────────────────────────────────────────────────────

export interface ApplyStackResult {
  stack: ApiStack
  created: string[]
  updated: string[]
  deleted: string[]
  errors: string[]
}

export const stacks = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiStack[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks`,
      {},
      token
    ),

  get: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<ApiStack>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, body: { name: string; spec: string }, token: string) =>
    apiFetch<ApiStack>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, stackId: string, body: { name?: string; spec: string }, token: string) =>
    apiFetch<ApiStack>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}`,
      { method: "DELETE" },
      token
    ),

  listServices: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<ApiService[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/services`,
      {},
      token
    ),

  apply: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<ApplyStackResult>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/apply`,
      { method: "POST" },
      token
    ),
}

// ─── Org email config (SMTP provider) ─────────────────────────────────────────

export interface ApiOrgEmailConfig {
  id: string
  organization_id: string
  host: string
  port: number
  username: string
  from_address: string
  from_name: string
  use_tls: boolean
  created_at: string
  updated_at: string
}

export interface SaveEmailConfigBody {
  host: string
  port: number
  username: string
  password: string      // empty string = keep existing on update
  from_address: string
  from_name?: string
  use_tls: boolean
}

export const emailConfig = {
  get: (orgId: string, token: string) =>
    apiFetch<ApiOrgEmailConfig>(
      `/api/v1/orgs/${orgId}/email-config`,
      {},
      token
    ),

  save: (orgId: string, body: SaveEmailConfigBody, token: string) =>
    apiFetch<ApiOrgEmailConfig>(
      `/api/v1/orgs/${orgId}/email-config`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/email-config`,
      { method: "DELETE" },
      token
    ),
}

// ─── Volumes ──────────────────────────────────────────────────────────────────

export interface ApiVolume {
  id: string
  project_id: string
  name: string
  slug: string
  storage_gb: number
  status: "pending" | "ready"
  mounts?: ApiVolumeMount[]
  created_at: string
  updated_at: string
}

export interface ApiVolumeMount {
  id: string
  volume_id: string
  service_id: string
  mount_path: string
  volume?: ApiVolume
  created_at: string
  updated_at: string
}

export const volumes = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiVolume[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes`,
      {},
      token
    ),

  get: (orgId: string, projectId: string, volumeId: string, token: string) =>
    apiFetch<ApiVolume>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, body: { name: string; storage_gb?: number }, token: string) =>
    apiFetch<ApiVolume>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, volumeId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}`,
      { method: "DELETE" },
      token
    ),

  attach: (
    orgId: string,
    projectId: string,
    volumeId: string,
    body: { service_id: string; mount_path: string },
    token: string,
  ) =>
    apiFetch<ApiVolumeMount>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}/mounts`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  detach: (orgId: string, projectId: string, volumeId: string, mountId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}/mounts/${mountId}`,
      { method: "DELETE" },
      token
    ),

  listServiceMounts: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiVolumeMount[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/mounts`,
      {},
      token
    ),

  getBackup: (orgId: string, projectId: string, volumeId: string, token: string) =>
    apiFetch<ApiVolumeBackupConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}/backup`,
      {},
      token
    ),

  upsertBackup: (
    orgId: string,
    projectId: string,
    volumeId: string,
    body: {
      storage_integration_id: string
      schedule: string
      retention_days: number
      path_prefix?: string
      enabled: boolean
    },
    token: string,
  ) =>
    apiFetch<ApiVolumeBackupConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}/backup`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  deleteBackup: (orgId: string, projectId: string, volumeId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/volumes/${volumeId}/backup`,
      { method: "DELETE" },
      token
    ),
}

// ─── Volume backup config ──────────────────────────────────────────────────────

export interface ApiVolumeBackupConfig {
  id: string
  volume_id: string
  storage_integration_id: string
  schedule: string
  retention_days: number
  path_prefix: string
  enabled: boolean
  last_backup_at: string | null
  last_backup_status: "pending" | "running" | "success" | "failed" | null
  created_at: string
  updated_at: string
}

