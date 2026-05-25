import { apiFetch } from "./core"
import type { ApiService } from "./services"

export type StackGitMode = "" | "file" | "repo"

export interface ApiStack {
  id: string
  project_id: string
  name: string
  spec: string
  variables: Record<string, string>
  status: "idle" | "applying" | "failed"
  last_applied_at: string | null
  created_at: string
  updated_at: string
  // Git source
  git_mode: StackGitMode
  git_repo: string
  git_branch: string
  git_path: string
  git_integration_id: string | null
  git_last_synced_at: string | null
  git_last_sync_sha: string
}

export interface ApplyStackResult {
  stack: ApiStack
  created: string[]
  updated: string[]
  deleted: string[]
  errors: string[]
}

export interface SyncStackResult extends ApplyStackResult {
  suggested_mode: StackGitMode
  warning: string
}

export interface CreateStackBody {
  name: string
  spec?: string
  variables?: Record<string, string>
  git_mode?: StackGitMode
  git_repo?: string
  git_branch?: string
  git_path?: string
  git_integration_id?: string | null
}

export interface UpdateStackBody {
  name?: string
  spec?: string
  variables?: Record<string, string>
  git_mode?: StackGitMode
  git_repo?: string
  git_branch?: string
  git_path?: string
  git_integration_id?: string | null
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

  create: (orgId: string, projectId: string, body: CreateStackBody, token: string) =>
    apiFetch<ApiStack>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, stackId: string, body: UpdateStackBody, token: string) =>
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

  apply: (orgId: string, projectId: string, stackId: string, token: string, envOverrides?: Record<string, string>) =>
    apiFetch<ApplyStackResult>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/apply`,
      { method: "POST", body: JSON.stringify({ env_overrides: envOverrides ?? {} }) },
      token
    ),

  sync: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<SyncStackResult>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/sync`,
      { method: "POST" },
      token
    ),
}
