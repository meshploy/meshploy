import { apiFetch } from "./core"

export type AgentRole = "admin" | "member"

export interface AgentTokenDTO {
  id: string
  name: string
  token_prefix: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
  created_at: string
}

export interface AgentDTO {
  id: string
  name: string
  role: AgentRole
  created_at: string
  tokens: AgentTokenDTO[]
}

export interface CreateAgentBody {
  name: string
  role: AgentRole
  token_name?: string
  expires_at?: string
}

export interface CreateAgentResult {
  agent: AgentDTO
  token: string
}

export interface CreateTokenBody {
  name?: string
  expires_at?: string
}

export interface CreateTokenResult {
  token: string
  metadata: AgentTokenDTO
}

// A token is active unless it's been revoked or has expired.
export function isTokenActive(t: AgentTokenDTO): boolean {
  if (t.revoked_at) return false
  if (t.expires_at && new Date(t.expires_at).getTime() <= Date.now()) return false
  return true
}

export type TokenState = "active" | "revoked" | "expired"

export function tokenState(t: AgentTokenDTO): TokenState {
  if (t.revoked_at) return "revoked"
  if (t.expires_at && new Date(t.expires_at).getTime() <= Date.now()) return "expired"
  return "active"
}

export const agents = {
  list: (orgId: string, token: string) =>
    apiFetch<AgentDTO[]>(`/api/v1/orgs/${orgId}/agents`, {}, token),

  create: (orgId: string, body: CreateAgentBody, token: string) =>
    apiFetch<CreateAgentResult>(`/api/v1/orgs/${orgId}/agents`, {
      method: "POST",
      body: JSON.stringify(body),
    }, token),

  createToken: (orgId: string, agentId: string, body: CreateTokenBody, token: string) =>
    apiFetch<CreateTokenResult>(`/api/v1/orgs/${orgId}/agents/${agentId}/tokens`, {
      method: "POST",
      body: JSON.stringify(body),
    }, token),

  revokeToken: (orgId: string, agentId: string, tokenId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/agents/${agentId}/tokens/${tokenId}`, {
      method: "DELETE",
    }, token),

  remove: (orgId: string, agentId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/agents/${agentId}`, {
      method: "DELETE",
    }, token),
}
