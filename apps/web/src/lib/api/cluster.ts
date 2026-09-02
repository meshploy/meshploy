import { apiFetch } from "./core"

export interface MeshHealth {
  configured: boolean
  checked: boolean
  healthy: boolean
  unauthorized: boolean
  last_error?: string
  last_error_at?: string | null
  last_success_at?: string | null
}

export interface OrphanWorkload {
  name: string
  project: string
  namespace: string
  replicas: number
  ready: number
  age_days: number
  has_pvc: boolean
}

export const cluster = {
  listOrphans: (orgId: string, token: string) =>
    apiFetch<{ orphans: OrphanWorkload[] }>(
      `/api/v1/orgs/${orgId}/cluster/orphans`,
      {},
      token
    ),

  getJoinToken: (orgId: string, token: string) =>
    apiFetch<{ token: string; server_url: string }>(
      `/api/v1/orgs/${orgId}/cluster/join-token`,
      {},
      token
    ),

  getHeadscalePreAuthKey: (orgId: string, token: string) =>
    apiFetch<{ has_active_key: boolean; key?: string; headscale_url: string }>(
      `/api/v1/orgs/${orgId}/cluster/headscale-preauth-key`,
      {},
      token
    ),

  getMeshHealth: (orgId: string, token: string) =>
    apiFetch<MeshHealth>(`/api/v1/orgs/${orgId}/cluster/mesh-health`, {}, token),

  createHeadscalePreAuthKey: (orgId: string, token: string) =>
    apiFetch<{ key: string; reusable: boolean; expiration: string; headscale_url: string }>(
      `/api/v1/orgs/${orgId}/cluster/headscale-preauth-key`,
      { method: "POST" },
      token
    ),
}
