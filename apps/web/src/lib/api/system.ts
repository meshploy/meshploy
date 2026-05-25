import { apiFetch } from "./core"

export interface VersionInfo {
  current: string
  latest: string
  update_available: boolean
  release_url: string
}

export const system = {
  versionInfo: (token: string) =>
    apiFetch<VersionInfo>("/api/v1/system/version", {}, token),
}
