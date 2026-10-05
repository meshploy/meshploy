import { apiFetch } from "./core"

/** A rule's source or destination. */
export interface AccessEnd {
  kind: "person" | "machine" | "all" | "service" | "stack" | "project" | "job" | "route"
  id?: string
  name: string
  /** A person's email, to tell people apart. */
  email?: string
  /** A person in the organisation, with a member page. */
  member?: boolean
  project_id?: string
  database?: boolean
  /** A source an edition manages (a grant derived from it): its kind as read, where it is managed, how many people it covers. */
  label?: string
  link?: string
  people?: number
}

/**
 * One line of the Access page: a grant (the same one a resource's Access tab
 * shows) or a network rule (a machine and ports, for what is not a resource).
 */
export interface AccessRule {
  id: string
  kind: "grant" | "network"
  from: AccessEnd
  to: AccessEnd
  /** The ports reached; empty on a network rule is every port. */
  ports: number[]
  /** What a grant opens from its person's machines: where it answers on the mesh, and its internal routes. */
  opens?: AccessOpen[]
  /** What a grant covers that is switched off from their machines. */
  off?: number
  actions?: string[]
  /** Whether a grant's person reaches it from their machines; absent while the default decides. */
  reach?: boolean
  note?: string
}

/** One thing a grant opens from its person's machines. */
export interface AccessOpen {
  kind: "port" | "route"
  /** What answers, when the grant covers several services. */
  service?: string
  /** Where it answers: a NodePort on every cluster machine, or a TCP route's port on the gateway. */
  port?: number
  on?: "cluster" | "gateway"
  /** An internal route's hostname. */
  hostname?: string
}

/** Someone an internal route answers. */
export interface RouteOpener {
  user_id: string
  name: string
  email?: string
  role: "owner" | "admin" | "member"
  /** The grant that opens it to a member. */
  via?: string
  /** False when switched off from their machines. */
  reach: boolean
  machines: string[]
}

/** Who an internal route answers on the mesh, once the policy is enforced. */
export interface RouteOpeners {
  internal: boolean
  enforced: boolean
  admins: RouteOpener[]
  members: RouteOpener[]
  /** Machines a network rule opens the gateway's web port to. */
  rules: string[]
}

export interface NetworkRuleBody {
  from_kind: "person" | "machine" | "all"
  from_id?: string
  to_node_id: string
  ports: string
  note: string
}

const base = (orgId: string) => `/api/v1/orgs/${orgId}/access/rules`

export const access = {
  rules: (orgId: string, token: string) => apiFetch<AccessRule[]>(base(orgId), {}, token),
  addRule: (orgId: string, body: NetworkRuleBody, token: string) =>
    apiFetch<void>(base(orgId), { method: "POST", body: JSON.stringify(body) }, token),
  previewRule: (orgId: string, body: NetworkRuleBody, token: string) =>
    apiFetch<{ acls: string }>(`${base(orgId)}/preview`, { method: "POST", body: JSON.stringify(body) }, token),
  updateRule: (orgId: string, id: string, body: NetworkRuleBody, token: string) =>
    apiFetch<void>(`${base(orgId)}/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(body) }, token),
  removeRule: (orgId: string, id: string, token: string) =>
    apiFetch<void>(`${base(orgId)}/${encodeURIComponent(id)}`, { method: "DELETE" }, token),
  routeOpeners: (orgId: string, projectId: string, routeId: string, token: string) =>
    apiFetch<RouteOpeners>(`/api/v1/orgs/${orgId}/projects/${projectId}/routes/${routeId}/openers`, {}, token),
}
