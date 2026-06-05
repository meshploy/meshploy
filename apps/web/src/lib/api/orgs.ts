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

export interface ApiOrgInvitation {
  id: string
  org_id: string
  email: string
  role: "admin" | "member"
  expires_at: string
  token?: string
}

export interface ApiInvitationInfo {
  email: string
  org_name: string
  role: string
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

  updateMember: (orgId: string, userId: string, role: "admin" | "member", token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/members/${userId}`, { method: "PATCH", body: JSON.stringify({ role }) }, token),

  removeMember: (orgId: string, userId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/members/${userId}`, { method: "DELETE" }, token),

  createInvitation: (orgId: string, email: string, role: "admin" | "member", token: string) =>
    apiFetch<ApiOrgInvitation>(`/api/v1/orgs/${orgId}/invitations`, { method: "POST", body: JSON.stringify({ email, role }) }, token),

  listInvitations: (orgId: string, token: string) =>
    apiFetch<ApiOrgInvitation[]>(`/api/v1/orgs/${orgId}/invitations`, {}, token),

  getInvitationByToken: (inviteToken: string) =>
    apiFetch<ApiInvitationInfo>(`/api/v1/invitations/${inviteToken}`),

  acceptInvitation: (inviteToken: string, username: string, password: string) =>
    apiFetch<{ message: string }>(`/api/v1/invitations/${inviteToken}/accept`, {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
}
