import { apiFetch } from "./core"

export type StorageProvider = "s3" | "r2" | "minio" | "b2"

export interface ApiStorageIntegration {
  id: string
  organization_id: string
  name: string
  provider: StorageProvider
  endpoint: string
  region: string
  bucket: string
  created_at: string
  updated_at: string
}

export interface CreateStorageBody {
  name: string
  provider: StorageProvider
  endpoint?: string
  region?: string
  bucket: string
  access_key_id: string
  secret_access_key: string
}

export const storage = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiStorageIntegration[]>(
      `/api/v1/orgs/${orgId}/storage-integrations`,
      {},
      token
    ),

  create: (orgId: string, body: CreateStorageBody, token: string) =>
    apiFetch<ApiStorageIntegration>(
      `/api/v1/orgs/${orgId}/storage-integrations`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/storage-integrations/${id}`,
      { method: "DELETE" },
      token
    ),
}
