import { apiFetch } from "./core"

export interface MeshHealth {
  configured: boolean
  checked: boolean
  healthy: boolean
  unauthorized: boolean
  last_error?: string
  last_error_at?: string | null
  last_success_at?: string | null
}

export interface OrphanWorkload {
  name: string
  project: string
  namespace: string
  replicas: number
  ready: number
  age_days: number
  has_pvc: boolean
}

export const cluster = {
  deleteOrphan: (orgId: string, namespace: string, name: string, deleteData: boolean, token: string) =>
    apiFetch<{ removed: string }>(
      `/api/v1/orgs/${orgId}/cluster/orphans/${namespace}/${name}?delete_data=${deleteData}`,
      { method: "DELETE" },
      token
    ),

  listOrphans: (orgId: string, token: string) =>
    apiFetch<{ orphans: OrphanWorkload[] }>(
      `/api/v1/orgs/${orgId}/cluster/orphans`,
      {},
      token
    ),

  getJoinToken: (orgId: string, token: string) =>
    /** k3s_version is the release the server runs, which a joining node installs. */
    apiFetch<{ token: string; server_url: string; k3s_version?: string }>(
      `/api/v1/orgs/${orgId}/cluster/join-token`,
      {},
      token
    ),

  getHeadscalePreAuthKey: (orgId: string, token: string) =>
    apiFetch<{ has_active_key: boolean; key?: string; headscale_url: string }>(
      `/api/v1/orgs/${orgId}/cluster/headscale-preauth-key`,
      {},
      token
    ),

  getMeshHealth: (orgId: string, token: string) =>
    apiFetch<MeshHealth>(`/api/v1/orgs/${orgId}/cluster/mesh-health`, {}, token),

  createHeadscalePreAuthKey: (orgId: string, token: string) =>
    apiFetch<{ key: string; reusable: boolean; expiration: string; headscale_url: string }>(
      `/api/v1/orgs/${orgId}/cluster/headscale-preauth-key`,
      { method: "POST" },
      token
    ),
}

// ── Placement: where everything runs, and what a node going down would do ──

export interface ApiPlacementNode {
  id: string
  name: string
  k8s_node_name: string
  online: boolean
  control_plane: boolean
  /** The scheduler may put a service on it: in the cluster, online, not kept for builds or the mesh alone. */
  takes_workloads: boolean
  /** What Kubernetes leaves for pods, after its own reserve. */
  allocatable_cpu_millis: number
  allocatable_memory_bytes: number
  /** What every pod on it asks for. */
  requested_cpu_millis: number
  requested_memory_bytes: number
}

export interface ApiPlacedService {
  id: string
  name: string
  level_id: string
  level_name: string
  project_id: string
  project_name: string
  type: string
  status: string
  run_once?: boolean
  /** The cluster node it is held to, when it is. */
  pinned_node?: string
  /** What one pod asks for. */
  cpu_request_millis: number
  memory_request_bytes: number
  pods: { name: string; node: string; phase: string }[]
  /** Nodes its volumes' data is bound to: a pod cannot leave them. */
  data_on?: string[]
}

export interface ApiPlacement {
  nodes: ApiPlacementNode[]
  services: ApiPlacedService[]
}

export interface ApiServiceForecast {
  service: ApiPlacedService
  outcome: "keeps" | "moves" | "down"
  on?: string[]
  to?: string
  reason?: string
}

export interface ApiNodeDownForecast {
  node: string
  /** The gateway: the control plane and the edge. Nothing is rescheduled, and no route answers from outside. */
  control_plane: boolean
  services: ApiServiceForecast[]
}

export const placement = {
  /** Org admins only: it spans every project. */
  get: (orgId: string, token: string) => apiFetch<ApiPlacement>(`/api/v1/orgs/${orgId}/placement`, {}, token),
  /** What would happen if the cluster node went down. Nothing is stopped. */
  nodeWhatIf: (orgId: string, node: string, token: string) =>
    apiFetch<ApiNodeDownForecast>(`/api/v1/orgs/${orgId}/placement/nodes/${encodeURIComponent(node)}/what-if`, {}, token),
}
