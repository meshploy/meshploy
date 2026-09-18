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
  /** False when paused: kept with its targets, not served. */
  published: boolean
  published_changed_at: string | null
  published_changed_by: string | null
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
  /** An address the gateway can reach, loopback included - for something
   *  running outside Meshploy that no service or node target can name. */
  target_ip?: string
  port?: number
  redirect_route_id?: string
  redirect_code?: number
}

export const routes = {
  /** Every route in the org. Hostnames are unique across it, not per project. */
  listOrg: (orgId: string, token: string) =>
    apiFetch<ApiDbRoute[]>(`/api/v1/orgs/${orgId}/routes`, {}, token),

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

  /** Serve the hostname again. The proxy picks it up within 30 seconds. */
  publish: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiDbRoute>(`/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/publish`, { method: "POST" }, token),

  /** Keep the route, answer 404 and issue no certificate. */
  pause: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiDbRoute>(`/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/pause`, { method: "POST" }, token),

}

/**
 * What the gateway's own firewall does with a port, from the host agent. Never
 * "reachable": a firewall at the hosting provider is invisible from the host.
 */
export interface ApiPortFirewall {
  state: "open" | "restricted" | "blocked" | "unknown"
  tool?: string
  sources?: string[]
  checked_at?: string
  reason?: string
}

/** A TCP route publishes one port on the gateway and forwards it over the mesh. */
/** Where a forwarded port is reachable from. */
export type TCPZone = "public" | "mesh" | "local"

export interface ApiTCPRoute {
  id: string
  organization_id: string
  project_id: string
  gateway_port: number
  /** Absent from an API older than zones, where every route was public. */
  zone?: TCPZone
  service_id: string | null
  service_port: number
  node_id: string | null
  target_ip: string
  target_port: number
  /** Empty means anyone who can reach the gateway. */
  allowed_cidrs: string[]
  status: "pending" | "open" | "failed" | "paused"
  last_error: string
  /** Absent from an API older than the host agent. */
  host_firewall?: ApiPortFirewall
  /** False when paused: kept with its targets, not served. */
  published: boolean
  published_changed_at: string | null
  published_changed_by: string | null

  created_at: string
  updated_at: string
}

export type TCPRouteBody = {
  /** Off the public zone it may be left out, and the target's port is used. */
  gateway_port: number
  zone?: TCPZone
  service_id?: string
  service_port?: number
  node_id?: string
  node_port?: number
  /** An address the gateway can reach, loopback included. */
  target_ip?: string
  target_port?: number
  allowed_cidrs?: string[]
}

/** Every published port in the org, plus the ports the gateway keeps for itself. */
export interface ApiTCPPortUsage {
  routes: ApiTCPRoute[]
  reserved: number[]
}

export const tcpRoutes = {
  /** Gateway ports are unique across the gateway, not per project. */
  usage: (orgId: string, token: string) =>
    apiFetch<ApiTCPPortUsage>(`/api/v1/orgs/${orgId}/tcp-routes`, {}, token),

  get: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiTCPRoute>(`/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes/${routeId}`, {}, token),

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

  publish: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiTCPRoute>(`/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes/${routeId}/publish`, { method: "POST" }, token),

  pause: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<ApiTCPRoute>(`/api/v1/orgs/${orgId}/projects/${projectId}/tcp-routes/${routeId}/pause`, { method: "POST" }, token),
}
