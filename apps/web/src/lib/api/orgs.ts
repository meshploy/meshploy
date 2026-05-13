import { apiFetch } from "./core"

export interface ApiOrg {
  id: string
  name: string
  slug: string
  created_at: string
  updated_at: string
}

export interface ApiOrgMember {
  id: string
  user_id: string
  role: "owner" | "admin" | "member"
  user_name: string
  user_email: string
}

export const orgs = {
  list: (token: string) =>
    apiFetch<ApiOrg[]>("/api/v1/orgs", {}, token),

  get: (orgId: string, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}`, {}, token),

  update: (orgId: string, name: string, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}`, { method: "PATCH", body: JSON.stringify({ name }) }, token),

  listMembers: (orgId: string, token: string) =>
    apiFetch<ApiOrgMember[]>(`/api/v1/orgs/${orgId}/members`, {}, token),

  addMember: (orgId: string, email: string, role: "admin" | "member", token: string) =>
    apiFetch<ApiOrgMember>(`/api/v1/orgs/${orgId}/members`, { method: "POST", body: JSON.stringify({ email, role }) }, token),

  removeMember: (orgId: string, userId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/members/${userId}`, { method: "DELETE" }, token),
}
