import { apiFetch } from "./core"

export interface VersionInfo {
  current: string
  /** "stable" (cut from a release tag), "edge" (built from main), or "dev". */
  channel: string
  latest: string
  update_available: boolean
  release_url: string
}

export interface ExposedPort {
  port: number
  service: string
  /** Why this port matters, where the service name alone does not say. */
  note?: string
}

export interface Exposure {
  /** What install.sh saw on the host: none, ufw, firewalld, or unknown. */
  firewall_state: string
  /** When the installer looked (RFC3339). Absent on older installs. */
  checked_at?: string
  /** Populated only when firewall_state is "none". */
  ports: ExposedPort[]
  dismissed: boolean
}

/** Stable slug for the host-exposure advisory. */
export const NOTICE_HOST_EXPOSURE = "host-exposure"

export const system = {
  versionInfo: (token: string) =>
    apiFetch<VersionInfo>("/api/v1/system/version", {}, token),

  exposure: (token: string) =>
    apiFetch<Exposure>("/api/v1/system/exposure", {}, token),

  dismissNotice: (key: string, token: string) =>
    apiFetch<void>(`/api/v1/system/notices/${key}/dismiss`, { method: "POST" }, token),
}
