import { apiFetch } from "./core"

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
  env_vars: string
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

  deleteRun: (orgId: string, projectId: string, jobId: string, runId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/jobs/${jobId}/runs/${runId}`,
      { method: "DELETE" },
      token
    ),
}
