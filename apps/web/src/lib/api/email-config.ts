import { apiFetch } from "./core"

export interface ApiOrgEmailConfig {
  id: string
  organization_id: string
  host: string
  port: number
  username: string
  from_address: string
  from_name: string
  use_tls: boolean
  created_at: string
  updated_at: string
}

export interface SaveEmailConfigBody {
  host: string
  port: number
  username: string
  password: string      // empty string = keep existing on update
  from_address: string
  from_name?: string
  use_tls: boolean
}

export const emailConfig = {
  get: (orgId: string, token: string) =>
    apiFetch<ApiOrgEmailConfig>(
      `/api/v1/orgs/${orgId}/email-config`,
      {},
      token
    ),

  save: (orgId: string, body: SaveEmailConfigBody, token: string) =>
    apiFetch<ApiOrgEmailConfig>(
      `/api/v1/orgs/${orgId}/email-config`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/email-config`,
      { method: "DELETE" },
      token
    ),
}
