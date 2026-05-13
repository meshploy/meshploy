import { apiFetch } from "./core"

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
