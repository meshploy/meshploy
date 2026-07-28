import { apiFetch } from "./core"

export const cluster = {
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

  createHeadscalePreAuthKey: (orgId: string, token: string) =>
    apiFetch<{ key: string; reusable: boolean; expiration: string; headscale_url: string }>(
      `/api/v1/orgs/${orgId}/cluster/headscale-preauth-key`,
      { method: "POST" },
      token
    ),
}
