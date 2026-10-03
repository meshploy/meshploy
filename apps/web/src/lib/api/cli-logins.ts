import { apiFetch } from "./core"

/** A CLI waiting for someone to approve its login, by the code its terminal shows. */
export interface CliLogin {
  user_code: string
  host: string
  expires_at: string
}

/** A logged-in CLI. `current` marks the one making the request. */
export interface CliSession {
  id: string
  host: string
  token_prefix: string
  created_at: string
  last_used_at?: string
  current: boolean
}

const login = (code: string) => `/api/v1/cli/logins/${encodeURIComponent(code)}`

export const cliLogins = {
  get: (code: string, token: string) => apiFetch<CliLogin>(login(code), {}, token),
  approve: (code: string, token: string) => apiFetch<void>(`${login(code)}/approve`, { method: "POST" }, token),
  deny: (code: string, token: string) => apiFetch<void>(`${login(code)}/deny`, { method: "POST" }, token),
  sessions: (token: string) => apiFetch<CliSession[]>("/api/v1/me/cli-sessions", {}, token),
  revoke: (id: string, token: string) =>
    apiFetch<void>(`/api/v1/me/cli-sessions/${encodeURIComponent(id)}`, { method: "DELETE" }, token),
}
