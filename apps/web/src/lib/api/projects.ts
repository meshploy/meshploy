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
  secrets_count: number
  jobs_count: number
  stacks_count: number
  volumes_count: number
}

function parseTimestamp(s: string | null | undefined): Date | null {
  if (!s) return null
  const d = new Date(s)
  if (d.getFullYear() <= 1) return null
  return d
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
    createdAt: parseTimestamp(p.created_at) ?? new Date(p.created_at),
  }
}

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
