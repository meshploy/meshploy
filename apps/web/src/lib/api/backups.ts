import { apiFetch } from "./core"

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

export interface BackupObject {
  key: string
  size: number
  last_modified: string
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

  trigger: (orgId: string, projectId: string, serviceId: string, id: string, token: string) =>
    apiFetch<ApiBackupConfig>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups/${id}/trigger`,
      { method: "POST" },
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

  triggerSystem: (orgId: string, token: string) =>
    apiFetch<ApiSystemBackupConfig>(
      `/api/v1/orgs/${orgId}/system-backup/trigger`,
      { method: "POST" },
      token
    ),

  listObjects: (orgId: string, projectId: string, serviceId: string, id: string, token: string) =>
    apiFetch<BackupObject[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups/${id}/objects`,
      {},
      token
    ),

  restore: (orgId: string, projectId: string, serviceId: string, id: string, key: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/backups/${id}/restore`,
      { method: "POST", body: JSON.stringify({ key }) },
      token
    ),

  listSystemObjects: (orgId: string, token: string) =>
    apiFetch<BackupObject[]>(
      `/api/v1/orgs/${orgId}/system-backup/objects`,
      {},
      token
    ),

  restoreSystem: (orgId: string, key: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/system-backup/restore`,
      { method: "POST", body: JSON.stringify({ key }) },
      token
    ),
}
