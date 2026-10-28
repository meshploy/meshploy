import { apiFetch } from "./core"

export type DNSProviderKind = "cloudflare"

export interface ApiDNSProvider {
  id: string
  organization_id: string
  name: string
  kind: DNSProviderKind
  // The zones the token may write records in, as of checked_at.
  zones: string[] | null
  checked_at?: string
  created_at: string
  updated_at: string
}

export interface ApiDNSZone {
  id: string
  name: string
  can_edit: boolean
}

export interface ApiDNSProviderCheck {
  provider: ApiDNSProvider
  zones: ApiDNSZone[]
}

export interface CreateDNSProviderBody {
  name: string
  kind: DNSProviderKind
  token: string
}

export const dnsProviders = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiDNSProvider[]>(`/api/v1/orgs/${orgId}/dns-providers`, {}, token),

  create: (orgId: string, body: CreateDNSProviderBody, token: string) =>
    apiFetch<ApiDNSProviderCheck>(
      `/api/v1/orgs/${orgId}/dns-providers`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  check: (orgId: string, id: string, token: string) =>
    apiFetch<ApiDNSProviderCheck>(`/api/v1/orgs/${orgId}/dns-providers/${id}/check`, { method: "POST" }, token),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/dns-providers/${id}`, { method: "DELETE" }, token),
}
