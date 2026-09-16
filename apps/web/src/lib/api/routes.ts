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
  /** Stack that created this route. null = created directly. */
  stack_id: string | null
  targets: ApiRouteTarget[]
  created_at: string
  updated_at: string
}

export type TargetBody = {
  path: string
  strip_path: boolean
  service_id?: string
  service_port_id?: string
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

/** A TCP route publishes one port on the gateway and forwards it over the mesh. */
export interface ApiTCPRoute {
  id: string
  organization_id: string
  project_id: string
  gateway_port: number
  service_id: string | null
  service_port: number
  node_id: string | null
  target_ip: string
  target_port: number
  /** Empty means anyone who can reach the gateway. */
  allowed_cidrs: string[]
  status: "pending" | "open" | "failed"
  last_error: string
  created_at: string
  updated_at: string
}

export type TCPRouteBody = {
  gateway_port: number
  service_id?: string
  service_port?: number
  node_id?: string
  node_port?: number
  allowed_cidrs?: string[]
}

export const tcpRoutes = {
  list: (orgId: string, projectId: string, token: string) =>
    apiFetch<ApiTCPRoute[]>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes`,
      {},
      token
    ),

  create: (orgId: string, projectId: string, body: TCPRouteBody, token: string) =>
    apiFetch<ApiTCPRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (
    orgId: string,
    projectId: string,
    routeId: string,
    body: { gateway_port?: number; allowed_cidrs?: string[] },
    token: string
  ) =>
    apiFetch<ApiTCPRoute>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes/${routeId}`,
      { method: "PATCH", body: JSON.stringify(body) },
      token
    ),

  remove: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes/${routeId}`,
      { method: "DELETE" },
      token
    ),
}
