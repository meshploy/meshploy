import { apiFetch } from "./core"

export interface ApiOrg {
  id: string
  name: string
  slug: string
  /** Each project takes traffic only from its own services, its levels, the projects that use its published variables, and the machines. */
  network_isolation?: boolean
  created_at: string
  updated_at: string
}

export interface ApiSandboxedProject {
  id: string
  /** The project it belongs to: itself for a project, its project for a level. */
  project_id: string
  name: string
  /** The level's name; absent for production. */
  level?: string
  required: boolean
}

export interface ApiOrgSandbox {
  /** Nodes prepared to run sandboxed pods. */
  nodes: string[]
  /** Whether a project created now starts sandboxed. */
  new_projects: boolean
  /** A rule requires every new project sandboxed; new_projects_why says which. */
  new_projects_required: boolean
  new_projects_why?: string
  projects: ApiSandboxedProject[]
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
  /** The address already has an account, which signs in and joins rather than creating another. */
  account_exists?: boolean
}

export const orgs = {
  list: (token: string) =>
    apiFetch<ApiOrg[]>("/api/v1/orgs", {}, token),

  get: (orgId: string, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}`, {}, token),

  update: (orgId: string, name: string, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}`, { method: "PATCH", body: JSON.stringify({ name }) }, token),

  setNetworkIsolation: (orgId: string, enabled: boolean, token: string) =>
    apiFetch<ApiOrg>(`/api/v1/orgs/${orgId}/network-isolation`, { method: "PUT", body: JSON.stringify({ enabled }) }, token),

  /** Sandboxed projects and levels this member can see, and the nodes that can run them. */
  sandbox: (orgId: string, token: string) =>
    apiFetch<ApiOrgSandbox>(`/api/v1/orgs/${orgId}/sandbox`, {}, token),

  /** Whether new projects start sandboxed; existing ones keep their own. */
  setSandboxNewProjects: (orgId: string, newProjects: boolean, token: string) =>
    apiFetch<ApiOrgSandbox>(
      `/api/v1/orgs/${orgId}/sandbox`,
      { method: "PUT", body: JSON.stringify({ new_projects: newProjects }) },
      token
    ),

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

  /** Accept an invitation as the account you are signed in as; its address must be the invited one. */
  joinInvitation: (inviteToken: string, token: string) =>
    apiFetch<void>(`/api/v1/invitations/${inviteToken}/join`, { method: "POST" }, token),
}
