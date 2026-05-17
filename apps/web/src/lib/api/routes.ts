import { apiFetch } from "./core"

export interface ApiRouteTarget {
  id: string
  route_id: string
  path: string
  strip_path: boolean
  service_id: string | null
  node_id: string | null
  target_ip: string
  target_port: number
  redirect_route_id: string | null
  redirect_code: number
  created_at: string
  updated_at: string
}

export interface ApiDbRoute {
  id: string
  organization_id: string
  project_id: string
  domain_id: string | null
  zone: "public" | "internal" | "preview"
  subdomain: string
  hostname: string
  custom_domain_verified: boolean
  targets: ApiRouteTarget[]
  created_at: string
  updated_at: string
}

export type TargetBody = {
  path: string
  strip_path: boolean
  service_id?: string
  node_id?: string
  port?: number
  redirect_route_id?: string
  redirect_code?: number
}

export const routes = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiDbRoute[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes`,
      {},
      token
    ),

  create: (
    orgId: string,
    projectId: string,
    body: {
      domain_id?: string
      zone: string
      subdomain?: string
      hostname?: string
      targets: TargetBody[]
    },
    token: string
  ) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  get: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiDbRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}`,
      {},
      token
    ),

  delete: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}`,
      { method: "DELETE" },
      token
    ),

  addTarget: (orgId: string, projectId: string, routeId: string, body: TargetBody, token: string) =>
    apiFetch<ApiRouteTarget>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/targets`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  updateTarget: (orgId: string, projectId: string, routeId: string, targetId: string, body: TargetBody, token: string) =>
    apiFetch<ApiRouteTarget>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/targets/${targetId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  deleteTarget: (orgId: string, projectId: string, routeId: string, targetId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/targets/${targetId}`,
      { method: "DELETE" },
      token
    ),

}
