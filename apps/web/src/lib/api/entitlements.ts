import { apiFetch } from "./core"

export interface ApiEntitlements {
  licensed: boolean
  tier?: string
  customer?: string
  features: string[]
  expires_at?: string
  expired: boolean
  node_limit?: number // 0 or absent = unlimited
  node_count: number
  over_limit: boolean
  /** Why an installed licence is not active. Empty when it is. */
  problem?: string
}

export const entitlements = {
  /**
   * Readable by any authenticated user — the UI needs it to decide what to
   * render. It never returns the licence token itself, only the resulting tier
   * and feature list.
   */
  get: (token: string) => apiFetch<ApiEntitlements>("/api/v1/entitlements", {}, token),

  /**
   * Install a licence token. Admin only.
   *
   * The open-source build trusts no signing key, so this fails there by design
   * with "this build trusts no license signing key" — upgrading means running
   * the EE image, not entering a key into a CE install.
   */
  activate: (licenseToken: string, token: string) =>
    apiFetch<ApiEntitlements>(
      "/api/v1/entitlements/license",
      { method: "POST", body: JSON.stringify({ token: licenseToken }) },
      token
    ),
}
