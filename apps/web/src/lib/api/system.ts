import { apiFetch } from "./core"

export interface VersionInfo {
  current: string
  /** "stable" (cut from a release tag), "edge" (built from main), or "dev". */
  channel: string
  latest: string
  update_available: boolean
  release_url: string
}

export const system = {
  versionInfo: (token: string) =>
    apiFetch<VersionInfo>("/api/v1/system/version", {}, token),
}
