import { useQueries } from "@tanstack/react-query"
import { nodes as nodesApi, type ApiNodeMetrics } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { Node } from "@/types"

/**
 * How much of the mesh is in use, from the nodes themselves.
 *
 * Memory and disk only, deliberately. Both are a single reading - total against
 * available - and are right the moment they arrive. CPU is a counter: a
 * percentage needs two samples and would show a blank for the first interval,
 * which on a page someone glances at is worse than not claiming it. The node's
 * own page has CPU, with the history to compute it from.
 *
 * Nodes that are offline are not scraped and not counted. A node that cannot be
 * reached simply contributes nothing, so the summary reads as "what is
 * answering" rather than going blank because one machine is down.
 */

export interface NodeLoad {
  memUsed: number
  memTotal: number
  diskUsed: number
  diskTotal: number
}

export interface MeshLoad {
  /** Per node id, for the nodes that answered. */
  byNode: Record<string, NodeLoad>
  total: NodeLoad
  /** How many online nodes have reported so far. */
  reporting: number
}

export function useMeshLoad(nodes: Node[]): MeshLoad {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const online = nodes.filter((n) => n.status === "online")

  const results = useQueries({
    queries: online.map((n) => ({
      queryKey: ["node-metrics", orgId, n.id],
      queryFn: () => nodesApi.getMetrics(orgId!, n.id, token),
      enabled: !!orgId,
      refetchInterval: 30_000,
      // A node that does not answer is not an error worth retrying hard or
      // showing: it contributes nothing and the row says so.
      retry: false,
      throwOnError: false,
    })),
  })

  const byNode: Record<string, NodeLoad> = {}
  const total: NodeLoad = { memUsed: 0, memTotal: 0, diskUsed: 0, diskTotal: 0 }

  results.forEach((r, i) => {
    const m = r.data as ApiNodeMetrics | undefined
    if (!m || typeof m.memory_total_bytes !== "number" || m.memory_total_bytes === 0) return
    const load: NodeLoad = {
      memUsed: m.memory_total_bytes - m.memory_available_bytes,
      memTotal: m.memory_total_bytes,
      diskUsed: m.disk_total_bytes - m.disk_avail_bytes,
      diskTotal: m.disk_total_bytes,
    }
    byNode[online[i].id] = load
    total.memUsed += load.memUsed
    total.memTotal += load.memTotal
    total.diskUsed += load.diskUsed
    total.diskTotal += load.diskTotal
  })

  return { byNode, total, reporting: Object.keys(byNode).length }
}
