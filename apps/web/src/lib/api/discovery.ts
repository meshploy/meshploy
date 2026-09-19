import { apiFetch } from "./core"
import type { ApiHostContainer } from "./nodes"

// Discovery: what runs on this org's nodes that Meshploy does not route.
//
// Read-only. Every action a row leads to is an ordinary route or request call
// with what the row prefills.

export interface ApiEndpointContainer {
  id: string
  name: string
  image?: string
  compose_project?: string
  host_network?: boolean
}

export interface ApiEndpointRoute {
  kind: "http" | "tcp"
  id: string
  name: string
}

export interface ApiEndpoint {
  id: string
  address: string
  port: number
  protocol: string
  process?: string
  label?: string
  /** Worth serving on a hostname. */
  http: boolean
  /** The endpoint speaks HTTPS, so the hop to it has to be HTTPS too. */
  tls?: boolean
  /** A gateway port a TCP route could take, absent when the endpoint's own
   *  port is one the gateway keeps for itself. */
  suggested_port?: number
  /** listener: a process holds it. published: only the runtime forwards it. */
  source: "listener" | "published"
  /** host: loopback only. all: every interface. address: one address. */
  scope: "host" | "all" | "address"
  container?: ApiEndpointContainer
  via_runtime?: boolean
  routed?: ApiEndpointRoute[]
  routable: boolean
  reason?: string
  /** Somebody has decided this one is correct as it is. */
  ignored?: boolean
  ignore_id?: string
  ignore_note?: string
}

export interface ApiNodeDiscovery {
  node_id: string
  name: string
  mesh_ip?: string
  gateway: boolean
  endpoints: ApiEndpoint[]
  containers: ApiHostContainer[]
  runtime?: string
  version?: string
  checked_at?: string
  stale: boolean
  hidden: number
  ignored: number
  error?: string
}

/** A node that reports nothing yet, named rather than left out. */
export interface ApiSilentNode {
  node_id: string
  name: string
  reason: string
}

export interface ApiDiscovery {
  nodes: ApiNodeDiscovery[]
  silent: ApiSilentNode[]
}

export interface ApiIgnoredEndpoint {
  id: string
  organization_id: string
  node_id: string
  address: string
  port: number
  note: string
}

export const discovery = {
  get: (orgId: string, token: string) =>
    apiFetch<ApiDiscovery>(`/api/v1/orgs/${orgId}/discovery`, {}, token),

  ignore: (orgId: string, body: { node_id: string; address: string; port: number; note?: string }, token: string) =>
    apiFetch<ApiIgnoredEndpoint>(
      `/api/v1/orgs/${orgId}/discovery/ignores`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  unignore: (orgId: string, ignoreId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/discovery/ignores/${ignoreId}`, { method: "DELETE" }, token),
}
