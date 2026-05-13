import { apiFetch } from "./core"

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
