import { apiFetch } from "./core"

export type RegistryProvider = "ghcr" | "dockerhub" | "ecr" | "gcr" | "custom" | "builtin"

export interface ApiRegistryIntegration {
  id: string
  organization_id: string
  name: string
  provider: RegistryProvider
  endpoint: string
  namespace: string
  created_at: string
  updated_at: string
}

export interface CreateRegistryBody {
  name: string
  provider: RegistryProvider
  endpoint?: string
  namespace?: string
  username: string
  password: string
}

export const registries = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiRegistryIntegration[]>(
      `/api/v1/orgs/${orgId}/registry-integrations`,
      {},
      token
    ),

  create: (orgId: string, body: CreateRegistryBody, token: string) =>
    apiFetch<ApiRegistryIntegration>(
      `/api/v1/orgs/${orgId}/registry-integrations`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/registry-integrations/${id}`,
      { method: "DELETE" },
      token
    ),
}
