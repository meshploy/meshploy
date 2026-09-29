import type { ApiNodeDownForecast, ApiPlacement, ApiServiceForecast } from "@/lib/api"

// The demo's copy of the server's node-down forecast (service/blast.go), over
// the demo's own placement. Kept to the same rules, so the demo tells the
// same story the server would.
export function forecastNodeDown(p: ApiPlacement, node: string): ApiNodeDownForecast | null {
  const down = p.nodes.find((n) => n.k8s_node_name === node)
  if (!down) return null
  const free = new Map<string, { cpu: number; mem: number }>()
  for (const n of p.nodes) {
    if (n.takes_workloads && n.k8s_node_name !== node) {
      free.set(n.k8s_node_name, { cpu: n.allocatable_cpu_millis - n.requested_cpu_millis, mem: n.allocatable_memory_bytes - n.requested_memory_bytes })
    }
  }
  const affected = p.services
    .filter((s) => !(s.run_once && s.status === "completed") && s.pods.some((pod) => pod.node === node))
    .sort((a, b) => b.memory_request_bytes - a.memory_request_bytes)
  const services: ApiServiceForecast[] = affected.map((s) => {
    const onHere = s.pods.filter((pod) => pod.node === node).length
    const on = [...new Set(s.pods.map((pod) => pod.node).filter((n) => n && n !== node))].sort()
    if (on.length > 0) return { service: s, outcome: "keeps", on }
    if (down.control_plane) return { service: s, outcome: "down", reason: "the control plane is on this node: nothing is moved while it is down" }
    if (s.pinned_node === node) return { service: s, outcome: "down", reason: "it is pinned to this node" }
    if (s.data_on?.includes(node)) return { service: s, outcome: "down", reason: "its volume's data is on this node, and does not move with it" }
    const cpu = s.cpu_request_millis * onHere, mem = s.memory_request_bytes * onHere
    const best = [...free.entries()].filter(([, f]) => f.cpu >= cpu && f.mem >= mem).sort((a, b) => b[1].mem - a[1].mem || a[0].localeCompare(b[0]))[0]
    if (!best) return { service: s, outcome: "down", reason: "no other node has room for what it asks" }
    best[1].cpu -= cpu
    best[1].mem -= mem
    return { service: s, outcome: "moves", to: best[0] }
  })
  return { node, control_plane: down.control_plane, services }
}
