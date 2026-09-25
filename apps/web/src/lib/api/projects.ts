import type { Project } from "@/types"
import { apiFetch } from "./core"

export interface ApiProject {
  id: string
  name: string
  slug: string
  organization_id: string
  created_at: string
  updated_at: string
  // Resource counts — embedded by the list endpoint (single SQL aggregation).
  services_count: number
  databases_count: number
  routes_count: number
  /** Variable groups, as the Variables tab lists them. */
  variables_count: number
  jobs_count: number
  stacks_count: number
  volumes_count: number
  config_files_count: number
  /** Set on an environment level: the project it is a level of. */
  parent_project_id?: string | null
  /** "production" on a project itself. */
  env_name?: string
  /** 0 is production; each level below counts up. */
  env_level?: number
}

/** One level of a project, production first. */
export interface EnvironmentLevel {
  project_id: string
  name: string
  level: number
  namespace: string
  production: boolean
  services_count: number
  databases_count: number
}

function parseTimestamp(s: string | null | undefined): Date | null {
  if (!s) return null
  const d = new Date(s)
  if (d.getFullYear() <= 1) return null
  return d
}

/** One service at one level, as the board shows it. */
export interface BoardCell {
  level_id: string
  lineage_id: string
  service_id: string
  service_name: string
  type: "application" | "database"
  status: string
  image: string
  deployed_at?: string | null
  /** A database whose last backup succeeded, which a level's own copy can be cloned from. */
  has_backup?: boolean
}

export interface BoardGroup {
  id: string
  name: string
  /** Level project IDs, from the level the group enters at up to production. */
  path: string[]
  cells: BoardCell[]
  /** Made by copying one service into a level; another group can absorb it. */
  single?: boolean
}

export interface Board {
  levels: EnvironmentLevel[]
  groups: BoardGroup[]
  /** Services in no group, which do not move between levels. */
  ungrouped: BoardCell[]
}

export interface Promotion {
  service_name: string
  from_service_id: string
  to_service_id: string
  image: string
  created_there: boolean
}

/** Why Promote left a service where it was. */
export type SkipReason = "unchanged" | "older" | "never_built" | "not_here"

export interface PromoteResult {
  promoted: Promotion[]
  skipped: { service_name: string; reason: SkipReason }[]
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
    variablesCount: p.variables_count ?? 0,
    jobsCount: p.jobs_count ?? 0,
    stacksCount: p.stacks_count ?? 0,
    volumesCount: p.volumes_count ?? 0,
    configFilesCount: p.config_files_count ?? 0,
    createdAt: parseTimestamp(p.created_at) ?? new Date(p.created_at),
  }
}

export const projects = {
  list: (orgId: string, token: string, opts: { search?: string; sort?: "recent" | "name" } = {}) => {
    const params = new URLSearchParams()
    if (opts.search) params.set("search", opts.search)
    if (opts.sort) params.set("sort", opts.sort)
    const query = params.toString()
    return apiFetch<ApiProject[]>(`/api/v1/orgs/${orgId}/projects${query ? `?${query}` : ""}`, {}, token)
  },

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

  environments: (orgId: string, projectId: string, token: string) =>
    apiFetch<EnvironmentLevel[]>(`/api/v1/orgs/${orgId}/projects/${projectId}/environments`, {}, token),

  createEnvironment: (
    orgId: string,
    projectId: string,
    body: { name: string; relative_to: string; placement: "above" | "below" },
    token: string
  ) =>
    apiFetch<EnvironmentLevel>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/environments`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  board: (orgId: string, projectId: string, token: string) =>
    apiFetch<Board>(`/api/v1/orgs/${orgId}/projects/${projectId}/board`, {}, token),

  createGroup: (
    orgId: string,
    projectId: string,
    body: { name: string; service_ids: string[]; path: string[] },
    token: string
  ) =>
    apiFetch<{ id: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/promotion-groups`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  deleteGroup: (orgId: string, projectId: string, groupId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/promotion-groups/${groupId}`,
      { method: "DELETE" },
      token
    ),

  renameEnvironment: (orgId: string, levelId: string, name: string, token: string) =>
    apiFetch<{ hostnames_changed: number }>(
      `/api/v1/orgs/${orgId}/projects/${levelId}/environment`,
      { method: "PATCH", body: JSON.stringify({ name }) },
      token
    ),

  renameGroup: (orgId: string, projectId: string, groupId: string, name: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/promotion-groups/${groupId}`,
      { method: "PATCH", body: JSON.stringify({ name }) },
      token
    ),

  addToGroup: (orgId: string, projectId: string, groupId: string, serviceIds: string[], token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/promotion-groups/${groupId}/services`,
      { method: "POST", body: JSON.stringify({ service_ids: serviceIds }) },
      token
    ),

  removeFromGroup: (orgId: string, projectId: string, groupId: string, lineageId: string, token: string) =>
    apiFetch<{ group_deleted: boolean }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/promotion-groups/${groupId}/services/${lineageId}`,
      { method: "DELETE" },
      token
    ),

  /** Copy a service into a lower level as a group of its own. */
  copyToLevel: (orgId: string, serviceLevelId: string, serviceId: string, levelId: string, token: string) =>
    apiFetch<{ id: string }>(
      `/api/v1/orgs/${orgId}/projects/${serviceLevelId}/services/${serviceId}/copy-to-level`,
      { method: "POST", body: JSON.stringify({ level_id: levelId }) },
      token
    ),

  /** Delete a level's copy of a service; production's is untouched. */
  removeFromLevel: (orgId: string, levelId: string, serviceId: string, token: string) =>
    apiFetch<{ routes_removed: number; left_group: boolean; group_deleted: boolean }>(
      `/api/v1/orgs/${orgId}/projects/${levelId}/services/${serviceId}/level-copy`,
      { method: "DELETE" },
      token
    ),

  /** Run a service's current image in a lower level. */
  bringDown: (orgId: string, serviceLevelId: string, serviceId: string, levelId: string, token: string) =>
    apiFetch<{ id: string }>(
      `/api/v1/orgs/${orgId}/projects/${serviceLevelId}/services/${serviceId}/bring-down`,
      { method: "POST", body: JSON.stringify({ level_id: levelId }) },
      token
    ),

  /** Give a level its own copy of a database it uses from above. */
  ownDatabase: (
    orgId: string,
    levelId: string,
    body: { source_service_id: string; mode: "empty" | "clone" },
    token: string
  ) =>
    apiFetch<{ id: string }>(
      `/api/v1/orgs/${orgId}/projects/${levelId}/own-database`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  /** Promote a group's services from this level (projectId) to the next on its path. */
  promote: (orgId: string, levelId: string, groupId: string, token: string) =>
    apiFetch<PromoteResult>(
      `/api/v1/orgs/${orgId}/projects/${levelId}/promotion-groups/${groupId}/promote`,
      { method: "POST" },
      token
    ),

  clearBuildCache: (orgId: string, projectId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/build-cache`,
      { method: "DELETE" },
      token
    ),
}
