import { apiFetch } from "./core"

export interface ApiVariableGroupItem {
  id: string
  group_id: string
  key: string
  value?: string   // omitted for secrets in list responses
  is_secret: boolean
}

export interface ApiVariableGroup {
  id: string
  project_id: string
  service_id?: string  // set for system-managed groups
  name: string
  description: string
  system_managed: boolean
  items: ApiVariableGroupItem[]
  created_at: string
  updated_at: string
}

export const variableGroups = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiVariableGroup[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, body: { name: string; description?: string }, token: string) =>
    apiFetch<ApiVariableGroup>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  get: (orgId: string, projectId: string, groupId: string, token: string) =>
    apiFetch<ApiVariableGroup>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups/${groupId}`,
      {},
      token
    ),

  update: (orgId: string, projectId: string, groupId: string, body: { name?: string; description?: string }, token: string) =>
    apiFetch<ApiVariableGroup>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups/${groupId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, projectId: string, groupId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups/${groupId}`,
      { method: "DELETE" },
      token
    ),

  upsertItem: (orgId: string, projectId: string, groupId: string, body: { key: string; value: string; is_secret: boolean }, token: string) =>
    apiFetch<ApiVariableGroupItem>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups/${groupId}/items`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  deleteItem: (orgId: string, projectId: string, groupId: string, itemId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/variable-groups/${groupId}/items/${itemId}`,
      { method: "DELETE" },
      token
    ),

  listForService: (orgId: string, projectId: string, serviceId: string, token: string) =>
    apiFetch<ApiVariableGroup[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/variable-groups`,
      {},
      token
    ),

  attach: (orgId: string, projectId: string, serviceId: string, groupId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/variable-groups`,
      { method: "POST", body: JSON.stringify({ group_id: groupId }) },
      token
    ),

  detach: (orgId: string, projectId: string, serviceId: string, groupId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/variable-groups/${groupId}`,
      { method: "DELETE" },
      token
    ),
}
