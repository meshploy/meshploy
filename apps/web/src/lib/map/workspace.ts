import type { ApiAttentionItem, ApiNode, ApiNodeDownForecast, ApiPlacedService, ApiProject, EnvironmentLevel } from "@/lib/api"
import type { Health, MapEdge, MapItem, Problem, ProjectGraph } from "./graph"

// The workspace around its machines: the cluster's nodes, and each project
// level as a small chip joined to the nodes its pods are on. What only this
// map can say is what depends on a machine, so that is what it is built
// around: a node down makes what runs only there wait on it, and a node can be
// taken out as a what-if to see what would move, and what would stay down.

export interface NodeRuns {
  levelId: string
  levelName: string
  projectName: string
  services: string[]
}

export interface WorkspaceItem extends MapItem {
  node?: ApiNode
  levelOf?: EnvironmentLevel
  /** A node's: each level with pods on it, and which services. */
  runs?: NodeRuns[]
}

const CLUSTER = "cluster"

/** Whether k3s release a is newer than b ("v1.33.4+k3s1"), by major.minor.patch. */
function newerK3s(a?: string, b?: string): boolean {
  const parse = (v?: string) => (v ?? "").replace(/^v/, "").split("+")[0].split(".").map(Number)
  const x = parse(a), y = parse(b)
  if (x.length < 3 || y.length < 3 || x.some(isNaN) || y.some(isNaN)) return false
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] > y[i]
  return false
}

/** Where an attention item's fix is, as the overview links it. */
function fixFor(a: ApiAttentionItem): Problem["fix"] {
  if (a.service_id && a.project_id) return { label: "Open the service", to: `/projects/${a.project_id}/services/${a.service_id}` }
  if (a.node_id) return { label: "Open the node", to: `/nodes/${a.node_id}` }
  if (a.domain_id) return { label: "Open domains", to: "/domains" }
  if (a.project_id) return { label: "Open the project", to: `/projects/${a.project_id}` }
  return undefined
}

function problemOf(a: ApiAttentionItem): Problem {
  return { severity: a.severity === "critical" ? "bad" : "warn", title: a.title, detail: a.detail, fix: fixFor(a) }
}

function worst(problems: Problem[], otherwise: Health): Health {
  if (problems.some((p) => p.severity === "bad")) return "bad"
  if (problems.length > 0) return "warn"
  return otherwise
}

export function buildWorkspaceGraph(input: {
  nodes: ApiNode[]
  projects: ApiProject[]
  levels: Record<string, EnvironmentLevel[]>
  attention: ApiAttentionItem[]
  load?: Record<string, { memUsed: number; memTotal: number; diskUsed: number; diskTotal: number }>
  /** Where every service's pods are, across every level (the placement endpoint's services). */
  placements: ApiPlacedService[]
  /** A node taken out as a what-if, and what would happen. */
  whatIf?: ApiNodeDownForecast | null
}): ProjectGraph {
  const { nodes, projects, levels, attention, load = {}, placements, whatIf } = input
  const items: WorkspaceItem[] = []
  const edges: MapEdge[] = []

  // ── The cluster ──────────────────────────────────────────────────────────
  const cluster = nodes.filter((n) => n.os !== "darwin" && n.os !== "windows")
  const gateway = cluster.find((n) => n.k3s_role === "server")
  if (cluster.length > 0) items.push({ id: CLUSTER, kind: "cluster", label: "Cluster", health: "ok", problems: [], to: "/cluster" })
  const nameOf = (n: ApiNode) => n.k8s_node_name || n.name

  for (const n of nodes) {
    const problems = attention.filter((a) => a.node_id === n.id).map(problemOf)
    if (n.status !== "online" && !problems.some((p) => p.severity === "bad")) problems.unshift({ severity: "bad", title: "Offline", fix: { label: "Open the node", to: `/nodes/${n.id}` } })
    // Kubernetes supports a node older than its control plane, never newer:
    // a worker that joined later with the day's k3s is one.
    if (gateway && n.id !== gateway.id && newerK3s(n.k3s_version, gateway.k3s_version)) {
      problems.push({ severity: "warn", title: "Newer Kubernetes than the gateway",
        detail: `It runs ${n.k3s_version}, the gateway ${gateway.k3s_version}. Update the gateway's k3s to at least this, or rejoin the node.` })
    }
    if (n.status === "online" && n.k8s_member && !n.k8s_ready) problems.push({ severity: "warn", title: "Not ready in the cluster", detail: "k3s says this node cannot take workloads now." })
    const taken = !!whatIf && whatIf.node === nameOf(n)
    if (taken) problems.unshift({ severity: "bad", title: "Taken out (what if)", detail: "Nothing is stopped: this shows what would happen." })
    const role = n.k3s_role === "server" ? "gateway · control plane" : n.mesh_role === "mesh" ? "mesh only" : n.mesh_role === "builder" ? "builds only" : n.mesh_role === "workload" ? "workloads" : "workloads + builds"
    const size = [n.cpu_cores ? `${n.cpu_cores} CPU` : "", n.memory_gb ? `${Math.round(n.memory_gb)} GiB` : ""].filter(Boolean).join(", ")
    const runs = new Map<string, NodeRuns>()
    for (const p of placements) {
      if (!p.pods.some((pod) => pod.node === nameOf(n))) continue
      const r = runs.get(p.level_id) ?? { levelId: p.level_id, levelName: p.level_name, projectName: p.project_name, services: [] }
      r.services.push(p.name)
      runs.set(p.level_id, r)
    }
    items.push({
      id: n.id, kind: "node", label: n.name, sub: [role, size].filter(Boolean).join(" · "),
      health: taken ? "bad" : worst(problems, n.status === "online" ? "ok" : "bad"), problems, node: n, runs: [...runs.values()],
      parent: cluster.includes(n) ? CLUSTER : undefined, to: `/nodes/${n.id}`,
      load: load[n.id] && load[n.id].memTotal > 0
        ? { memory: load[n.id].memUsed / load[n.id].memTotal, disk: load[n.id].diskTotal ? load[n.id].diskUsed / load[n.id].diskTotal : 0 }
        : undefined,
    })
    if (gateway && n.id !== gateway.id && cluster.includes(n)) {
      edges.push({ id: `mesh-${n.id}`, source: gateway.id, target: n.id, kind: "mesh", broken: n.status !== "online" || taken })
    }
  }
  const nodeByName = new Map(nodes.map((n) => [nameOf(n), n]))

  // ── Projects and their levels, as chips ─────────────────────────────────
  for (const p of projects) {
    const ls = [...(levels[p.id] ?? [])].sort((a, b) => b.level - a.level) // lowest first, production last
    if (ls.length === 0) ls.push({ project_id: p.id, name: "production", level: 0, namespace: p.slug, production: true, services_count: p.services_count, databases_count: p.databases_count })
    items.push({ id: `project-${p.id}`, kind: "project", label: p.name, health: "ok", problems: [], to: `/projects/${p.id}` })
    for (const l of ls) {
      const problems = attention.filter((a) => a.project_id === l.project_id).map(problemOf)
      const count = l.services_count + l.databases_count
      const here = placements.filter((pl) => pl.level_id === l.project_id)
      const onNodes = new Set(here.flatMap((pl) => pl.pods.map((pod) => pod.node)).filter(Boolean))
      const it: WorkspaceItem = {
        id: l.project_id, kind: "level", label: l.name, parent: `project-${p.id}`, levelOf: l,
        sub: count === 0 ? "Empty" : `${count} service${count === 1 ? "" : "s"}`,
        health: worst(problems, count === 0 ? "idle" : "ok"), problems,
        // A project seen only for what was granted inside it is opened at its
        // overview: its map is the whole project's.
        to: p.limited ? `/projects/${l.project_id}` : `/projects/${l.project_id}/map`,
      }
      items.push(it)
      for (const name of onNodes) {
        const n = nodeByName.get(name)
        if (n) edges.push({ id: `runs-${l.project_id}-${n.id}`, source: l.project_id, target: n.id, kind: "runs" })
      }
      if (count > 0 && onNodes.size === 0 && placements.length > 0) {
        it.problems.push({ severity: "warn", title: "Runs on no node", detail: "It has services, and none of their pods is on any machine: stopped, or waiting to be scheduled.", fix: { label: "Open its services", to: `/projects/${l.project_id}/services` } })
        if (it.health === "ok") it.health = "warn"
      }
      // A node that is down: what runs only there waits on it.
      const downOn = [...onNodes].filter((name) => nodeByName.get(name)?.status !== "online")
      if (downOn.length > 0) {
        const all = downOn.length === onNodes.size
        it.problems.unshift({ severity: all ? "bad" : "warn", title: `${all ? "Waiting on" : "Partly on"} ${downOn.join(", ")}`, detail: `${downOn.join(", ")} is offline${all ? ", and everything of this level ran there" : ""}.` })
        it.health = all ? "waiting" : it.health === "bad" ? "bad" : "warn"
      }
    }
  }

  // ── The what-if ─────────────────────────────────────────────────────────
  if (whatIf) {
    const takenNode = nodeByName.get(whatIf.node)
    for (const it of items.filter((i) => i.kind === "level")) {
      const mine = whatIf.services.filter((f) => f.service.level_id === it.id)
      const down = mine.filter((f) => f.outcome === "down")
      const moves = mine.filter((f) => f.outcome === "moves")
      if (whatIf.control_plane && (it.levelOf?.services_count ?? 0) > 0) {
        it.problems.unshift({ severity: "bad", title: "Unreachable from outside", detail: "The gateway is the edge: every public route goes through it." })
      }
      if (down.length > 0) {
        it.problems.unshift({ severity: "bad", title: `${down.length} would stay down`, detail: down.map((f) => f.service.name).join(", ") })
      } else if (moves.length > 0) {
        it.problems.unshift({ severity: "warn", title: `${moves.length} would move`, detail: moves.map((f) => f.service.name).join(", ") })
      }
      if (down.length > 0 || (whatIf.control_plane && (it.levelOf?.services_count ?? 0) > 0)) it.health = "bad"
      else if (moves.length > 0 && it.health === "ok") it.health = "warn"
      const e = edges.find((x) => x.source === it.id && x.target === takenNode?.id)
      if (e) e.broken = true
    }
  }

  // A level's own problems are the overview's list, which the summary shows:
  // on the map its chip says only what the map adds, a node it waits on or a
  // what-if's outcome.
  for (const it of items.filter((i) => i.kind === "level")) it.quiet = !whatIf && it.health !== "waiting"

  // A box is as healthy as the worst of what it holds.
  const rank: Record<Health, number> = { ok: 0, idle: 1, warn: 2, waiting: 3, bad: 4 }
  for (const box of items.filter((i) => i.kind === "project" || i.kind === "cluster")) {
    const children = items.filter((c) => c.parent === box.id)
    box.health = children.reduce<Health>((w, c) => (c.health !== "idle" && rank[c.health] > rank[w] ? c.health : w), "ok")
    const down = children.filter((c) => c.health === "bad").length
    const warn = children.filter((c) => c.health === "warn" || c.health === "waiting").length
    box.sub = down || warn
      ? [down ? `${down} down` : "", warn ? `${warn} need a look` : ""].filter(Boolean).join(", ")
      : box.kind === "cluster" ? `${children.length} node${children.length === 1 ? "" : "s"}` : `${children.length} level${children.length === 1 ? "" : "s"}`
  }
  return { items, edges }
}
