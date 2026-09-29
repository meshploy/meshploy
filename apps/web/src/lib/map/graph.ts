import { parse } from "yaml"
import type { ApiDbRoute, ApiHint, ApiService, ApiStack, ApiTrouble, ApiVolume } from "@/lib/api"

// A project as a graph: what serves what, what waits on what, and what is
// wrong. Built from what the console already reads, so the map and the pages
// never disagree.

export type Health = "ok" | "warn" | "bad" | "waiting" | "idle"

export interface Problem {
  severity: "bad" | "warn"
  title: string
  detail?: string
  /** Where the fix is: a page of the console. */
  fix?: { label: string; to: string }
}

export interface MapItem {
  id: string
  kind: "route" | "service" | "database" | "volume" | "stack" | "cluster" | "node" | "project" | "level"
  label: string
  sub?: string
  health: Health
  problems: Problem[]
  /** The stack a service or volume belongs to: drawn inside it. */
  parent?: string
  /** The page it opens. */
  to?: string
  /** A service that others' failures reach: what it waits on. */
  waitingOn?: string[]
  service?: ApiService
  /** Draws no problem line on its box: said in its panel, and elsewhere on the page. */
  quiet?: boolean
  /** A machine's memory and disk in use, each as a fraction of what it has. */
  load?: { memory: number; disk: number }
}

export interface MapEdge {
  id: string
  source: string
  target: string
  kind: "serves" | "depends" | "mounts" | "reads" | "runs" | "mesh" | "promotes"
  label?: string
  /** On the path from something waiting to what it waits on. */
  broken?: boolean
  /** A promotion waiting on this step: drawn moving, said in the level's panel. */
  waiting?: boolean
}

export interface ProjectGraph {
  items: MapItem[]
  edges: MapEdge[]
}

const TROUBLE: Record<ApiTrouble["kind"], string> = {
  out_of_memory: "Runs out of memory",
  crashing: "Keeps crashing",
  image_pull: "Cannot pull its image",
  cannot_start: "Cannot start",
}

/** depends_on of each compose service, as compose writes it: a list, or a map with conditions. */
function dependsOn(spec: string): Record<string, { name: string; condition?: string }[]> {
  let doc: { services?: Record<string, { depends_on?: unknown }> }
  try {
    doc = parse(spec) ?? {}
  } catch {
    return {}
  }
  const out: Record<string, { name: string; condition?: string }[]> = {}
  for (const [name, svc] of Object.entries(doc.services ?? {})) {
    const d = svc?.depends_on
    if (Array.isArray(d)) out[name] = d.map((n) => ({ name: String(n) }))
    else if (d && typeof d === "object")
      out[name] = Object.entries(d as Record<string, { condition?: string }>).map(([n, v]) => ({ name: n, condition: v?.condition }))
  }
  return out
}

const CONDITION: Record<string, string> = {
  service_healthy: "healthy",
  service_started: "started",
  service_completed_successfully: "completed",
}

export function buildProjectGraph(input: {
  projectId: string
  services: ApiService[]
  stacks: ApiStack[]
  routes: ApiDbRoute[]
  volumes: ApiVolume[]
  troubles: Record<string, ApiTrouble>
  hints: Record<string, ApiHint[]>
  /** The services whose published variables each service reads, by service id. */
  reads: Record<string, string[]>
  /** Taken out as a what-if: services and volumes drawn as down, and what depends on them as waiting. */
  takenOut?: Set<string>
}): ProjectGraph {
  const { projectId, services, stacks, routes, volumes, troubles, hints, reads, takenOut = new Set<string>() } = input
  const whatIf = (): Problem => ({ severity: "bad", title: "Taken out (what if)", detail: "Nothing is stopped: this shows what would depend on it." })
  const items = new Map<string, MapItem>()
  const edges: MapEdge[] = []
  const svcPage = (id: string, tab = "") => `/projects/${projectId}/services/${id}${tab ? `/${tab}` : ""}`

  for (const st of stacks) {
    items.set(st.id, { id: st.id, kind: "stack", label: st.name, sub: "Stack", health: "ok", problems: [], to: `/projects/${projectId}/stacks/${st.id}` })
  }

  for (const s of services) {
    const problems: Problem[] = []
    let health: Health = "ok"
    const t = troubles[s.id]
    if (t) {
      health = "bad"
      problems.push({
        severity: "bad", title: TROUBLE[t.kind] ?? "Keeps stopping",
        detail: [t.restarts ? `${t.restarts} restarts` : "", t.exit_code != null ? `exit code ${t.exit_code}` : "",
          t.memory_limit ? `limit ${t.memory_limit}` : "", t.message ?? ""].filter(Boolean).join(", "),
        fix: t.kind === "out_of_memory"
          ? { label: "Raise its memory limit", to: svcPage(s.id, "config") }
          : { label: "Open its logs", to: svcPage(s.id, "logs") },
      })
    } else if (s.status === "failed") {
      health = "bad"
      problems.push({ severity: "bad", title: "Failed", fix: { label: "Open its deployments", to: svcPage(s.id, "deployments") } })
    } else if (s.status === "deploying") {
      health = "warn"
    } else if (s.status === "stopped") {
      health = "idle"
    }
    if (s.latest_deploy_failed) {
      if (health === "ok") health = "warn"
      problems.push({ severity: "warn", title: "Latest build failed", detail: "It runs what it ran before.", fix: { label: "Open its deployments", to: svcPage(s.id, "deployments") } })
    }
    for (const h of hints[s.id] ?? []) {
      if (health === "ok") health = "warn"
      problems.push({ severity: "warn", title: h.title, detail: h.detail, fix: { label: "See the suggestion", to: svcPage(s.id) } })
    }
    if (takenOut.has(s.id)) {
      health = "bad"
      problems.unshift(whatIf())
    }
    const database = s.type === "database" || /^(postgres|mysql|mariadb|mongo|redis|valkey)/.test((s.image.split("/").pop() ?? ""))
    items.set(s.id, {
      id: s.id, kind: database ? "database" : "service", label: s.name, health, problems, service: s,
      sub: s.run_once ? "Runs once" : `${s.replicas} replica${s.replicas === 1 ? "" : "s"}`,
      parent: s.stack_id && items.has(s.stack_id) ? s.stack_id : undefined, to: svcPage(s.id),
    })
  }

  // What each compose service waits for, by name within its stack.
  for (const st of stacks) {
    const byName = new Map(services.filter((s) => s.stack_id === st.id).map((s) => [s.name, s.id]))
    for (const [name, deps] of Object.entries(dependsOn(st.spec))) {
      const from = byName.get(name)
      for (const d of deps) {
        const to = byName.get(d.name)
        if (from && to) edges.push({ id: `dep-${from}-${to}`, source: from, target: to, kind: "depends", label: d.condition ? CONDITION[d.condition] ?? d.condition : undefined })
      }
    }
  }

  // Reads another service's published variables: needs it to be up to be right.
  for (const [sid, owners] of Object.entries(reads)) {
    for (const owner of owners) {
      if (owner === sid || !items.has(owner)) continue
      if (edges.some((e) => e.source === sid && e.target === owner)) continue
      edges.push({ id: `reads-${sid}-${owner}`, source: sid, target: owner, kind: "reads", label: "reads its variables" })
    }
  }

  for (const r of routes) {
    const health: Health = r.published ? "ok" : "idle"
    items.set(r.id, {
      id: r.id, kind: "route", label: r.hostname, sub: r.zone === "public" ? "Public route" : "Internal route",
      health, problems: r.published ? [] : [{ severity: "warn", title: "Paused", detail: "Kept with its targets, not served." }],
      to: `/projects/${projectId}/routes/${r.id}`,
    })
    for (const t of r.targets ?? []) {
      if (t.service_id && items.has(t.service_id)) {
        edges.push({ id: `route-${r.id}-${t.id}`, source: r.id, target: t.service_id, kind: "serves", label: t.path && t.path !== "/" ? t.path : undefined })
      }
    }
  }

  for (const v of volumes) {
    const out = takenOut.has(v.id)
    items.set(v.id, {
      id: v.id, kind: "volume", label: v.name, sub: `${v.storage_gb} GiB`,
      health: out || v.status === "failed" ? "bad" : v.status === "idle" ? "idle" : "ok",
      problems: [...(out ? [whatIf()] : []), ...(v.status === "failed" ? [{ severity: "bad" as const, title: "Its claim is missing or lost its node" }] : [])],
      parent: v.stack_id && items.has(v.stack_id) ? v.stack_id : undefined, to: `/projects/${projectId}/volumes/${v.id}`,
    })
    for (const m of v.mounts ?? []) {
      // The path is said in the panel: on the edge it covered the volume.
      if (items.has(m.service_id)) edges.push({ id: `mount-${m.id}`, source: m.service_id, target: v.id, kind: "mounts" })
    }
  }

  // Failures travel against the arrows: what depends on, or reads from, a
  // service that is down waits on it, and says so, rather than each showing
  // a failure of its own that is really the other one's.
  const needs = (id: string) => edges.filter((e) => e.source === id && (e.kind === "depends" || e.kind === "reads" || e.kind === "mounts"))
  const rootCauses = (id: string, seen = new Set<string>()): string[] => {
    if (seen.has(id)) return []
    seen.add(id)
    const out: string[] = []
    for (const e of needs(id)) {
      const dep = items.get(e.target)
      if (!dep) continue
      if (dep.health === "bad") out.push(dep.id)
      else out.push(...rootCauses(dep.id, seen))
    }
    return [...new Set(out)]
  }
  for (const it of items.values()) {
    if (it.kind !== "service" && it.kind !== "database") continue
    // A step that ran once and finished waits on nothing now.
    if (it.service?.run_once && it.service.status === "completed") continue
    // Taken out in a what-if: it is down itself, not waiting on anything.
    if (takenOut.has(it.id)) continue
    const causes = rootCauses(it.id)
    if (causes.length === 0) continue
    it.waitingOn = causes
    const names = causes.map((c) => items.get(c)?.label).join(", ")
    if (it.health === "ok" || it.health === "warn") it.health = "waiting"
    it.problems.unshift({ severity: "warn", title: `Waiting on ${names}`, detail: `${names} is down, and this needs it.`, fix: { label: `Open ${items.get(causes[0])?.label}`, to: items.get(causes[0])?.to ?? "" } })
  }
  // The edges between a waiting service and its causes.
  const onPath = (from: string, cause: string, seen = new Set<string>()): string[] => {
    if (seen.has(from)) return []
    seen.add(from)
    for (const e of needs(from)) {
      if (e.target === cause) return [e.id]
      const rest = onPath(e.target, cause, seen)
      if (rest.length) return [e.id, ...rest]
    }
    return []
  }
  for (const it of items.values()) {
    for (const c of it.waitingOn ?? []) for (const eid of onPath(it.id, c)) {
      const e = edges.find((x) => x.id === eid)
      if (e) e.broken = true
    }
  }
  // A route to something down or waiting says its visitors are affected.
  for (const e of edges.filter((x) => x.kind === "serves")) {
    const target = items.get(e.target)
    const route = items.get(e.source)
    if (!target || !route || (target.health !== "bad" && target.health !== "waiting")) continue
    route.health = route.health === "idle" ? "idle" : "warn"
    route.problems.push({ severity: "warn", title: `Serves ${target.label}, which is ${target.health === "bad" ? "down" : "waiting"}`, fix: target.to ? { label: `Open ${target.label}`, to: target.to } : undefined })
    e.broken = true
  }

  // A stack is as healthy as the worst of what it holds.
  const rank: Record<Health, number> = { ok: 0, idle: 1, warn: 2, waiting: 3, bad: 4 }
  for (const it of items.values()) {
    if (it.kind !== "stack") continue
    const children = [...items.values()].filter((c) => c.parent === it.id)
    const worst = children.reduce<Health>((w, c) => (rank[c.health] > rank[w] ? c.health : w), "ok")
    it.health = worst
    const down = children.filter((c) => c.health === "bad").length
    const waiting = children.filter((c) => c.health === "waiting").length
    if (down || waiting) it.sub = [down ? `${down} down` : "", waiting ? `${waiting} waiting` : ""].filter(Boolean).join(", ")
    else it.sub = `${children.filter((c) => c.kind !== "volume").length} services`
  }

  return { items: [...items.values()], edges }
}
