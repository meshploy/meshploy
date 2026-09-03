import { apiFetch } from "./core"

/**
 * A config file's `content` is never returned by the API — it is stored
 * encrypted and write-only, the same as a variable group's values. `size` is
 * carried instead so the UI can show that something is there without carrying
 * an htpasswd hash or a TLS key into the browser.
 */
export interface ApiConfigFile {
  id: string
  name: string
  path: string
  stack_id: string | null
  size: number
  /** Names of the services mounting this file. */
  services: string[]
  created_at: string
}

/** What the detail endpoint adds: service ids to link to, and a modified time. */
export interface ApiConfigFileDetail extends ApiConfigFile {
  attached_services: { id: string; name: string }[]
  updated_at: string
}

export interface ConfigFileBody {
  name: string
  path: string
  content: string
}

export const configFiles = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<{ files: ApiConfigFile[] }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files`,
      {},
      token
    ),

  get: (orgId: string, projectId: string, fileId: string, token: string) =>
    apiFetch<ApiConfigFileDetail>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files/${fileId}`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, body: ConfigFileBody, token: string) =>
    apiFetch<{ id: string; name: string; path: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, projectId: string, fileId: string, body: ConfigFileBody, token: string) =>
    apiFetch<{ id: string; name: string; path: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files/${fileId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, fileId: string, token: string) =>
    apiFetch<{ message: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files/${fileId}`,
      { method: "DELETE" },
      token
    ),

  attach: (orgId: string, projectId: string, fileId: string, serviceId: string, token: string) =>
    apiFetch<{ message: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files/${fileId}/attach/${serviceId}`,
      { method: "POST" },
      token
    ),

  detach: (orgId: string, projectId: string, fileId: string, serviceId: string, token: string) =>
    apiFetch<{ message: string }>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/config-files/${fileId}/attach/${serviceId}`,
      { method: "DELETE" },
      token
    ),
}
