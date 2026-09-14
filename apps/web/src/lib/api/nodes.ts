import type { Node, MeshRole } from "@/types"
import { apiFetch } from "./core"

export interface ApiNode {
  id: string
  name: string
  tailscale_ip: string
  status: "online" | "offline"
  k3s_role: "server" | "agent"
  mesh_role: MeshRole
  k3s_version: string
  k3s_labels: Record<string, string>
  cpu_cores: number
  memory_gb: number
  disk_gb: number
  last_seen_at: string | null
  organization_id: string
  created_at: string
  updated_at: string
  // Headscale peer data (zeroed when Headscale is not configured)
  headscale_id: string
  headscale_online: boolean
  headscale_last_seen: string | null
  headscale_expiry: string | null
  headscale_tags: string[]
  headscale_user: string
  headscale_fqdn: string
  // K8s cluster membership
  k8s_member: boolean
  k8s_ready: boolean
  k8s_node_name: string
  // Active project namespaces on this node
  active_projects: string[]
  // Public internet IP (gateway/server nodes only)
  public_ip: string
  // Set while a removal waits for Headscale to drop the node's peer
  removal_requested_at?: string | null
  removal_error?: string
}

export interface ApiNodeMetrics {
  cpu_total_seconds: number
  cpu_idle_seconds: number
  cpu_cores: number
  memory_total_bytes: number
  memory_available_bytes: number
  disk_total_bytes: number
  disk_avail_bytes: number
  net_rx_bytes: number
  net_tx_bytes: number
}

function parseTimestamp(s: string | null | undefined): Date | null {
  if (!s) return null
  const d = new Date(s)
  if (d.getFullYear() <= 1) return null
  return d
}

/** Nodes a service, database, job or volume can be pinned to: online cluster
 * members that accept workloads. The gateway does, by default; a builder-only
 * node is tainted so workloads never start there, and a mesh-only node is not
 * in the cluster at all. */
export function schedulableNodes(nodes: ApiNode[]): ApiNode[] {
  return nodes.filter((n) => n.k8s_member && n.status === "online" && n.mesh_role !== "builder" && n.mesh_role !== "mesh")
}

/** The second line of a node's card: its mesh IP, marked when it is the gateway. */
export function nodeCardSub(node: Node): string {
  return node.k3sRole === "server" ? `${node.tailscaleIP} · gateway` : node.tailscaleIP
}

export function toNode(n: ApiNode): Node {
  return {
    id: n.id,
    name: n.name,
    tailscaleIP: n.tailscale_ip,
    removalRequestedAt: parseTimestamp(n.removal_requested_at ?? null),
    removalError: n.removal_error ?? "",
    status: n.status,
    k3sRole: n.k3s_role,
    meshRole: n.mesh_role ?? "workload_builder",
    k3sVersion: n.k3s_version,
    os: "",
    cpuCores: n.cpu_cores,
    memoryGB: n.memory_gb,
    diskGB: n.disk_gb,
    lastSeenAt: parseTimestamp(n.last_seen_at),
    organizationId: n.organization_id,
    headscaleId: n.headscale_id,
    headscaleOnline: n.headscale_online,
    headscaleLastSeen: parseTimestamp(n.headscale_last_seen),
    headscaleExpiry: parseTimestamp(n.headscale_expiry),
    headscaleTags: n.headscale_tags ?? [],
    headscaleUser: n.headscale_user,
    headscaleFQDN: n.headscale_fqdn ?? "",
    k8sMember: n.k8s_member,
    k8sReady: n.k8s_ready,
    k8sNodeName: n.k8s_node_name,
    activeProjects: n.active_projects ?? [],
  }
}

export const nodes = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiNode[]>(`/api/v1/orgs/${orgId}/nodes`, {}, token),

  get: (orgId: string, nodeId: string, token: string) =>
    apiFetch<ApiNode>(`/api/v1/orgs/${orgId}/nodes/${nodeId}`, {}, token),

  getMetrics: (orgId: string, nodeId: string, token: string) =>
    apiFetch<ApiNodeMetrics>(`/api/v1/orgs/${orgId}/nodes/${nodeId}/metrics`, {}, token),

  getRegistrationToken: (orgId: string, token: string) =>
    apiFetch<{ token: string }>(`/api/v1/orgs/${orgId}/node-registration-token`, {}, token),

  generateRegistrationToken: (orgId: string, token: string) =>
    apiFetch<{ token: string }>(
      `/api/v1/orgs/${orgId}/node-registration-token`,
      { method: "POST" },
      token
    ),

  update: (orgId: string, nodeId: string, body: { mesh_role?: MeshRole; name?: string }, token: string) =>
    apiFetch<ApiNode>(`/api/v1/orgs/${orgId}/nodes/${nodeId}`, { method: "PATCH", body: JSON.stringify(body) }, token),

  // removed is false while the removal waits for Headscale; an older server
  // answers 204, which means removed.
  delete: (orgId: string, nodeId: string, token: string) =>
    apiFetch<{ removed: boolean; error?: string } | undefined>(`/api/v1/orgs/${orgId}/nodes/${nodeId}`, { method: "DELETE" }, token),

  cancelRemoval: (orgId: string, nodeId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/nodes/${nodeId}/cancel-removal`, { method: "POST" }, token),

  createProvisioningToken: (orgId: string, label: string, expiresAt: string | null, token: string) =>
    apiFetch<ProvisioningTokenCreated>(
      `/api/v1/orgs/${orgId}/node-provisioning-tokens`,
      { method: "POST", body: JSON.stringify({ label, ...(expiresAt ? { expires_at: expiresAt } : {}) }) },
      token
    ),
}

export interface ProvisioningTokenCreated {
  id: string
  organization_id: string
  label: string
  used_at: string | null
  expires_at: string | null
  created_at: string
  updated_at: string
  token: string // plaintext — shown once
}
