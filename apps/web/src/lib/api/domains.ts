import { apiFetch } from "./core"

export interface ApiDomain {
  id: string
  organization_id: string
  base_domain: string
  internal_subdomain: string
  preview_subdomain: string
  verified: boolean
  created_at: string
  updated_at: string
}

export const domains = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiDomain[]>(`/api/v1/orgs/${orgId}/domains`, {}, token),

  get: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(`/api/v1/orgs/${orgId}/domains/${domainId}`, {}, token),
}
