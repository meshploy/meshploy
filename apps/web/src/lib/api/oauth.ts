import { apiFetch } from "./core"

/** What an MCP client asking to connect is, for the approval page. */
export interface OAuthRequestInfo {
  client_name: string
  /** Where the browser is sent back to, so a person sees which application they let in. */
  redirect_host: string
}

/** The client's request, as /oauth/authorize received it. */
export interface OAuthRequest {
  client_id: string
  redirect_uri: string
  code_challenge: string
  code_challenge_method: string
  state?: string
}

/** An MCP client connected through OAuth. */
export interface OAuthConnection {
  id: string
  client_name: string
  /** The agent it acts as; absent when it acts as the person who connected it. */
  as_agent?: string
  agent_id?: string
  approved_by: string
  approved_by_name: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

/** A member's signed-in CLI, as an admin sees it. */
export interface OrgCliSession {
  id: string
  user_id: string
  user_name: string
  host: string
  created_at: string
  last_used_at?: string
  /** Its person also belongs to another organisation, where it acts as them too: only they sign it out. */
  elsewhere: boolean
}

/** A member's sign-in to the console, as an owner or admin sees it. */
export interface OrgConsoleSession {
  id: string
  user_id: string
  user_name: string
  user_agent: string
  ip: string
  created_at: string
  last_seen_at: string
  /** Its person also belongs to another organisation, where it signs them in too: only they end it. */
  elsewhere: boolean
  /** The sign-in this request was made with: this browser. */
  current?: boolean
}

const conns = (orgId: string) => `/api/v1/orgs/${orgId}/oauth/connections`

export const oauth = {
  info: (req: OAuthRequest, token: string) => {
    const q = new URLSearchParams({ client_id: req.client_id, redirect_uri: req.redirect_uri,
      code_challenge: req.code_challenge, code_challenge_method: req.code_challenge_method })
    return apiFetch<OAuthRequestInfo>(`/api/v1/oauth/authorize?${q}`, {}, token)
  },
  /** Approve (as yourself, or as agentId) or deny; answers where to send the browser. */
  decide: (req: OAuthRequest, body: { approve: boolean; org_id?: string; agent_id?: string }, token: string) =>
    apiFetch<{ redirect: string }>("/api/v1/oauth/authorize", { method: "POST", body: JSON.stringify({ ...req, ...body }) }, token),
  /** Your own connections, or (owners and admins) a person's, an agent's, or every one. */
  connections: (orgId: string, token: string, by?: { userId?: string; agentId?: string; all?: boolean }) => {
    const q = by?.agentId ? `?agent_id=${by.agentId}` : by?.userId ? `?user_id=${by.userId}` : by?.all ? "?all=true" : ""
    return apiFetch<OAuthConnection[]>(`${conns(orgId)}${q}`, {}, token)
  },
  /** Connected sessions per member (assistants connected here, and signed-in CLIs). Owners and admins. */
  sessionCounts: (orgId: string, token: string) => apiFetch<Record<string, number>>(`/api/v1/orgs/${orgId}/session-counts`, {}, token),
  /** CLIs signed in as the organisation's members, or one of them. Owners and admins; read-only. */
  orgCliSessions: (orgId: string, token: string, userId?: string) =>
    apiFetch<OrgCliSession[]>(`/api/v1/orgs/${orgId}/cli-sessions${userId ? `?user_id=${userId}` : ""}`, {}, token),
  /** Sign out a member's CLI (owners and admins), while they belong to this organisation alone. */
  logOutOrgCli: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/cli-sessions/${encodeURIComponent(id)}`, { method: "DELETE" }, token),
  revoke: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`${conns(orgId)}/${encodeURIComponent(id)}`, { method: "DELETE" }, token),
  /** Console sign-ins of the organisation's members, or one of them. Owners and admins. */
  orgConsoleSessions: (orgId: string, token: string, userId?: string) =>
    apiFetch<OrgConsoleSession[]>(`/api/v1/orgs/${orgId}/console-sessions${userId ? `?user_id=${userId}` : ""}`, {}, token),
  /** Sign a member out of the console (owners and admins), while they belong to this organisation alone. */
  endOrgConsoleSession: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/console-sessions/${encodeURIComponent(id)}`, { method: "DELETE" }, token),
}
