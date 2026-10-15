import { API_BASE, apiFetch } from "./core"

/** How an organisation names and shows itself. Community always answers Meshploy's. */
export interface ApiBrand {
  name: string
  /** A CSS colour for the primary tokens, and the text colour drawn on it. */
  accent?: string
  accent_foreground?: string
  /** The wordmark and the square mark, served from the console's own origin. */
  logo_url?: string
  mark_url?: string
  /** False for Meshploy's own brand. */
  custom: boolean
  /** Show "Powered by Meshploy" beside a custom brand. */
  powered_by: boolean
}

export const DEFAULT_BRAND: ApiBrand = { name: "Meshploy", custom: false, powered_by: false }

/** A logo's address as the browser fetches it: the API's own paths are relative to it. */
export function brandAssetURL(url?: string): string | undefined {
  if (!url) return undefined
  return url.startsWith("/") ? `${API_BASE}${url}` : url
}

export const brand = {
  /** The brand of the organisation this host belongs to, before signing in. */
  forHost: () => apiFetch<ApiBrand>("/api/v1/brand"),
  forOrg: (orgId: string, token: string) => apiFetch<ApiBrand>(`/api/v1/orgs/${orgId}/brand`, {}, token),
}
