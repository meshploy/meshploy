import { apiFetch } from "./core"
import type { ApiService } from "./services"

export type StackGitMode = "" | "file" | "repo"

export interface ApiStack {
  id: string
  project_id: string
  name: string
  spec: string
  variables: Record<string, string>
  status: "idle" | "applying" | "running" | "failed" | "destroyed"
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
  /** Images its built services keep for rollback: true keeps each one's last image_retention, false every image. */
  rollback_enabled?: boolean
  image_retention?: number
}

export interface ApplyStackResult {
  stack: ApiStack
  created: string[]
  updated: string[]
  deleted: string[]
  /** Services this apply rolled out. */
  deployed?: string[]
  /** Services that roll out once what they depend on is up. */
  queued?: string[]
  /** The record of this apply's rollout, under the stack's runs. */
  run_id?: string
  errors: string[]
  warnings?: string[]
}

export type StackRunStatus = "running" | "succeeded" | "failed" | "stopped" | "interrupted"
export type RolloutStepStatus = "waiting" | "started" | "succeeded" | "failed" | "not_started"

/** One service of a run's rollout, in its depends_on layer. */
export interface RolloutStep {
  name: string
  service_id: string
  layer: number
  deployment_id?: string
  status: RolloutStepStatus
  error?: string
}

/** One Sync or Apply of a stack, and how its rollout went. */
export interface StackRun {
  id: string
  stack_id: string
  kind: "sync" | "apply"
  triggered_by?: string
  commit?: string
  status: StackRunStatus
  created_at: string
  updated_at: string
  finished_at?: string
  result: { created: string[] | null; updated: string[] | null; deleted: string[] | null; errors: string[] | null; warnings: string[] | null }
  rollout: RolloutStep[]
}

export interface DestroyStackResult {
  stack: ApiStack
  destroyed: string[]
  volumes: string[]
  routes: string[]
  errors: string[]
}

export interface DestroyStackBody {
  delete_volumes: boolean
  delete_routes: boolean
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
  /** Images the stack's built services keep: set on them at once. */
  rollback_enabled?: boolean
  image_retention?: number
}

export const stacks = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiStack[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks`,
      {},
      token
    ),

  // The spec with every x-meshploy setting an apply would default written out,
  // by the same code apply uses.
  meshployConfig: (orgId: string, projectId: string, spec: string, token: string) =>
    apiFetch<{ spec: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/meshploy-config`,
      { method: "POST", body: JSON.stringify({ spec }) },
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

  destroy: (orgId: string, projectId: string, stackId: string, token: string, body: DestroyStackBody) =>
    apiFetch<DestroyStackResult>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/destroy`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  runs: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<StackRun[]>(`/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/runs`, {}, token),

  run: (orgId: string, projectId: string, stackId: string, runId: string, token: string) =>
    apiFetch<StackRun>(`/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/runs/${runId}`, {}, token),

  sync: (orgId: string, projectId: string, stackId: string, token: string) =>
    apiFetch<SyncStackResult>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/stacks/${stackId}/sync`,
      { method: "POST" },
      token
    ),
}
