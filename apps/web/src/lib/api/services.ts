import { apiFetch } from "./core"

export interface ApiServicePort {
  id: string
  service_id: string
  name: string
  port: number
  is_http: boolean
  is_primary: boolean
  is_public: boolean
  node_port: number
}

export interface ApiService {
  id: string
  name: string
  project_id: string
  node_id: string | null
  stack_id: string | null
  type: "application" | "database"
  status: "running" | "stopped" | "deploying" | "failed"
  image: string
  pull_registry_integration_id: string | null
  ports: ApiServicePort[]
  replicas: number
  cpu_request: string
  cpu_limit: string
  memory_request: string
  memory_limit: string
  created_at: string
  updated_at: string
}

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

export interface ApiPodInfo {
  name: string
  phase: string
  ready: boolean
  restarts: number
  node_name: string
  started_at: string
}

export interface ApiPodMetrics {
  pod_name: string
  cpu_millis: number
  memory_mib: number
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

export interface ApiBuildConfig {
  id: string
  service_id: string
  builder: "railpack" | "dockerfile" | "image"
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

export interface PortBody {
  name: string
  port: number
  is_http: boolean
  is_primary: boolean
  is_public: boolean
}

export interface CreateServiceBody {
  ports?: PortBody[]
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
  builder?: "railpack" | "dockerfile"
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
  pull_registry_integration_id?: string
}

export interface UpdateServiceBody {
  name?: string
  image?: string
  node_id?: string     // "" = auto-schedule, UUID = pin to node
  replicas?: number
  cpu_request?: string
  cpu_limit?: string
  memory_request?: string
  memory_limit?: string
  env_vars?: string
  ports?: PortBody[]   // replaces all ports when set
  pull_registry_integration_id?: string  // "" = clear (public image), UUID = set
}

export interface UpdateBuildConfigBody {
  git_repo?: string
  branch?: string
  builder?: "railpack" | "dockerfile"
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

  listPods: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiPodInfo[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/pods`,
      {},
      token
    ),

  getPodMetrics: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiPodMetrics[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/pods/metrics`,
      {},
      token
    ),
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
