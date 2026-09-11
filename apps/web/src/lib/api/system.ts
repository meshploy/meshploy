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

export type ReleaseChannel = "stable" | "edge"

export interface ChannelCommit {
  /** Short commit. */
  sha: string
  /** First line of the commit message. Absent when only the commit is known. */
  subject: string
  date: string
}

/**
 * Both release channels, where this server is on them, and whether it may move
 * to the other one. Built from GitHub; `unavailable` says what is missing when
 * it could not be reached.
 */
export interface Channels {
  current: {
    version: string
    /** stable, edge, or empty for a development build. */
    channel: string
    /** The commit an edge build was cut from. */
    commit: string
  }
  stable: { releases: { tag: string; published_at: string; url: string }[] }
  edge: {
    /** The newest commit an edge server can pull. */
    head?: ChannelCommit
    /** Commits on main since the latest release. */
    ahead_by: number
    /** Those commits, newest first, at most 30. */
    commits: ChannelCommit[]
  }
  /** Absent on a development build. */
  switch?: {
    to: ReleaseChannel
    /** Only forward: edge to stable waits until a release includes the build. */
    allowed: boolean
    reason?: string
    /** What the switch brings, newest first, at most 30 of `total`. */
    changes: ChannelCommit[]
    total: number
  }
  unavailable?: string
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

  /** Upgrade on the server's own channel, or switch to `channel`. */
  requestUpgrade: (token: string, channel?: ReleaseChannel) =>
    apiFetch<UpgradeStatus>(
      "/api/v1/system/upgrade",
      channel ? { method: "POST", body: JSON.stringify({ channel }) } : { method: "POST" },
      token
    ),

  channels: (token: string) =>
    apiFetch<Channels>("/api/v1/system/channels", {}, token),
}
