import { apiFetch } from "./core"

export interface VersionInfo {
  current: string
  /** "stable" (cut from a release tag), "edge" (built from main), or "dev". */
  channel: string
  latest: string
  update_available: boolean
  release_url: string
  /** When GitHub was last asked. */
  checked_at?: string
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
  /** How this gateway gets certificates: "delegation" when it runs its own
   *  authoritative DNS and holds a wildcard, "ondemand" when DNS stays with
   *  the operator's provider. An internal route differs between the two. */
  dns_mode?: "delegation" | "ondemand"
}

/** A stage of the migration, in the order they run. */
export type MigrationStage =
  | "detect" | "plan" | "credential" | "prepare" | "move" | "cutover" | "rollback" | "finish"

/** One group of the plan and where it has got to. */
export interface GroupProgress {
  id: string
  name: string
  members?: string[]
  moved: boolean
  moved_at?: string
  /** Why the last attempt stopped. The group put itself back, so this is a
   *  reason to read rather than damage to repair. */
  error?: string
  can_move: boolean
  blockers?: string[]
  /** The hostnames this group serves. A group that cannot move loses them when
   *  the ports change hands. */
  domains?: string[]
  data?: string[]
  downtime?: string
}

/** Where the migration has got to, summarised on the host from its journal. */
export interface MigrationStatus {
  updated_at: string
  prepared: boolean
  cut_over: boolean
  finished: boolean
  groups: GroupProgress[]
}

/** A request the console queued for the host agent. */
export interface HostRequestState {
  id: string
  state: "queued" | "running" | "succeeded" | "failed"
  requested_at: string
  finished_at?: string
  error?: string
}

export interface MigrationState {
  agent_reporting: boolean
  detect: unknown | null
  detect_at?: string
  plan: unknown | null
  plan_at?: string
  prepare: unknown | null
  prepare_at?: string
  move: unknown | null
  move_at?: string
  cutover: unknown | null
  cutover_at?: string
  rollback: unknown | null
  rollback_at?: string
  finish: unknown | null
  finish_at?: string
  status: MigrationStatus | null
  status_at?: string
  requests: Record<string, HostRequestState>
}

/** Whether the gateway's host agent is reporting. */
export interface HostAgentStatus {
  reporting: boolean
  version?: string
  started_at?: string
  heartbeat_at?: string
  tasks?: Record<string, { ok: boolean; error?: string; at: string }>
  /** ufw, firewalld, iptables or none. */
  firewall?: string
}

/** Stable slug for the host-exposure advisory. */
export const NOTICE_HOST_EXPOSURE = "host-exposure"

/** The overview's "start here" panel. It hides itself once a service has a
 *  route, so dismissing it is only for someone who would rather not read it. */
export const NOTICE_GETTING_STARTED = "getting-started"

/**
 * Upgrading the server from the console. The API only queues a request; a
 * systemd unit on the gateway runs the upgrade and reports progress back.
 */
export interface UpgradeStatus {
  /** The updater is on (`sudo meshploy updater start`) and the host agent is reporting. */
  enabled: boolean
  /** The updater is on, but the host agent that runs upgrades is not reporting. */
  agent_stopped?: boolean
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
  /** "enterprise" when the run switches to the Enterprise images. */
  edition?: string
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

  /** Ask GitHub now, past the API's caches. At most one real check every 30 seconds. */
  checkForUpdates: (token: string) =>
    apiFetch<VersionInfo>("/api/v1/system/check-updates", { method: "POST" }, token),

  exposure: (token: string) =>
    apiFetch<Exposure>("/api/v1/system/exposure", {}, token),

  /** Every advisory the current user has dismissed. One call for all of them. */
  dismissedNotices: (token: string) =>
    apiFetch<{ dismissed: string[] }>("/api/v1/system/notices", {}, token),

  dismissNotice: (key: string, token: string) =>
    apiFetch<void>(`/api/v1/system/notices/${key}/dismiss`, { method: "POST" }, token),

  upgradeStatus: (token: string) =>
    apiFetch<UpgradeStatus>("/api/v1/system/upgrade", {}, token),

  /**
   * Upgrade on the server's own channel and edition, or switch channel, or
   * switch to the Enterprise images the active licence grants. The image comes
   * from the licence on the server, never from here.
   */
  requestUpgrade: (token: string, opts?: { channel?: ReleaseChannel; edition?: "enterprise" }) =>
    apiFetch<UpgradeStatus>(
      "/api/v1/system/upgrade",
      opts && (opts.channel || opts.edition)
        ? { method: "POST", body: JSON.stringify(opts) }
        : { method: "POST" },
      token
    ),

  hostAgent: (token: string) =>
    apiFetch<HostAgentStatus>("/api/v1/system/host-agent", {}, token),

  /** Where migrating this server off another platform has got to. */
  migration: (token: string) =>
    apiFetch<MigrationState>("/api/v1/system/migrate/dokploy", {}, token),

  /**
   * Ask the host agent to run a stage. It is queued, not run here: the API has
   * no access to the host, and the agent on the gateway does the work.
   */
  requestMigration: (token: string, kind: MigrationStage, body?: { group?: string; volumes?: boolean }) =>
    apiFetch<HostRequestState>(
      `/api/v1/system/migrate/dokploy/${kind}`,
      { method: "POST", body: JSON.stringify(body ?? {}) },
      token
    ),

  channels: (token: string) =>
    apiFetch<Channels>("/api/v1/system/channels", {}, token),
}
