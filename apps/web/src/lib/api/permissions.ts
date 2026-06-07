import { apiFetch } from "./core"

export type ResourceType = "project" | "service" | "stack" | "job" | "route"
export type ResourceAction = "view" | "create" | "deploy" | "update" | "delete"

export const RESOURCE_ACTIONS: ResourceAction[] = ["view", "create", "deploy", "update", "delete"]

export interface ApiPermission {
  id: string
  resource_type: ResourceType
  resource_id: string
  action: ResourceAction
  resource_name?: string
  parent_project_id?: string
}

export interface PermissionBody {
  resource_type: ResourceType
  resource_id: string
  action: ResourceAction
}

export interface PermissionsWithUserDTO {
  user_id: string
  user_name: string
  user_email: string
  action: ResourceAction
}

export const permissions = {
  listForMember: (orgId: string, userId: string, token: string) =>
    apiFetch<ApiPermission[]>(`/api/v1/orgs/${orgId}/members/${userId}/permissions`, {}, token),

  listForResource: (orgId: string, resourceType: "project" | "service" | "stack" | "job", resourceId: string, token: string) =>
    apiFetch<PermissionsWithUserDTO[]>(`/api/v1/orgs/${orgId}/${resourceType}s/${resourceId}/permissions`, {}, token),

  grant: (orgId: string, userId: string, body: PermissionBody, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/members/${userId}/permissions`, {
      method: "POST",
      body: JSON.stringify(body),
    }, token),

  revoke: (orgId: string, userId: string, body: PermissionBody, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/members/${userId}/permissions`, {
      method: "DELETE",
      body: JSON.stringify(body),
    }, token),
}
