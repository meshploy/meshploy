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

/**
 * Upgrading the server from the console. The API only queues a request; a
 * systemd unit on the gateway runs the upgrade and reports progress back.
 */
export interface UpgradeStatus {
  /** The updater is on (`sudo meshploy updater start`). */
  enabled: boolean
  /** The current user may start an upgrade: the server's owner, while enabled. */
  can_upgrade: boolean
  /** A request is queued and the server has not picked it up yet. */
  pending: boolean
  pending_id?: string
  /** The current or last run. Empty when there has never been one. */
  id: string
  /** running, succeeded, failed, interrupted, or empty. */
  state: string
  step: string
  channel: string
  cli_from: string
  cli_to: string
  started_at: string
  finished_at: string
  error: string
  /** The end of the run's log. Only the server's owner receives it. */
  log_tail: string[]
}

export const system = {
  versionInfo: (token: string) =>
    apiFetch<VersionInfo>("/api/v1/system/version", {}, token),

  exposure: (token: string) =>
    apiFetch<Exposure>("/api/v1/system/exposure", {}, token),

  dismissNotice: (key: string, token: string) =>
    apiFetch<void>(`/api/v1/system/notices/${key}/dismiss`, { method: "POST" }, token),

  upgradeStatus: (token: string) =>
    apiFetch<UpgradeStatus>("/api/v1/system/upgrade", {}, token),

  requestUpgrade: (token: string) =>
    apiFetch<UpgradeStatus>("/api/v1/system/upgrade", { method: "POST" }, token),
}
