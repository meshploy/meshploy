import { apiFetch } from "./core"

export const cluster = {
  getJoinToken: (token: string) =>
    apiFetch<{ token: string; server_url: string }>("/api/v1/cluster/join-token", {}, token),

  getHeadscalePreAuthKey: (token: string) =>
    apiFetch<{ has_active_key: boolean; key?: string; headscale_url: string }>(
      "/api/v1/cluster/headscale-preauth-key",
      {},
      token
    ),

  createHeadscalePreAuthKey: (token: string) =>
    apiFetch<{ key: string; reusable: boolean; expiration: string; headscale_url: string }>(
      "/api/v1/cluster/headscale-preauth-key",
      { method: "POST" },
      token
    ),
}
