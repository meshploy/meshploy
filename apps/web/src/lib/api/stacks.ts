import { apiFetch } from "./core"
import type { ApiService } from "./services"

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
