import ELK, { type ElkNode } from "elkjs/lib/elk.bundled.js"
import type { MapItem, ProjectGraph } from "./graph"

// Positions are worked out on every load, never stored, so the map cannot go
// stale. Left to right: routes, what they serve, what that depends on, and the
// volumes under it all; a stack is a box around its own.

const elk = new ELK()

export const NODE_SIZE: Record<MapItem["kind"], { width: number; height: number }> = {
  route: { width: 230, height: 58 },
  service: { width: 230, height: 84 },
  database: { width: 230, height: 84 },
  volume: { width: 190, height: 54 },
  node: { width: 240, height: 84 },
  level: { width: 230, height: 84 },
  // Boxes around others: sized by what they hold.
  stack: { width: 0, height: 0 },
  cluster: { width: 0, height: 0 },
  project: { width: 0, height: 0 },
}

/**
 * A box's size for what it shows: a name and a line under it, then the top
 * problem when there is one, and a machine's load. A fixed height per kind
 * left an empty band in every box with nothing wrong.
 */
export function sizeOf(it: MapItem): { width: number; height: number } {
  const { width, height } = NODE_SIZE[it.kind]
  if (CONTAINERS.has(it.kind) || it.kind === "route" || it.kind === "volume") return { width, height }
  const problem = it.problems.length > 0 && !it.quiet ? 20 : 0
  const load = it.load ? 28 : 0
  return { width, height: 60 + problem + load }
}

/** Kinds drawn as a box around others. */
export const CONTAINERS = new Set<MapItem["kind"]>(["stack", "cluster", "project"])

export interface Placed {
  x: number
  y: number
  width: number
  height: number
}

/** Where everything goes: boxes by id, and each edge's route, as points on the map. */
export interface Layout {
  placed: Record<string, Placed>
  routes: Record<string, { x: number; y: number }[]>
}

export async function layoutGraph(graph: ProjectGraph): Promise<Layout> {
  const node = (it: MapItem): ElkNode => ({ id: it.id, ...sizeOf(it) })
  // What connects to nothing (a database nothing reads yet, a route with no
  // target) is set out in a grid under the rest: ELK stacks such pieces into
  // one tall column beside a graph with boxes in it.
  const linked = new Set(graph.edges.flatMap((e) => [e.source, e.target]))
  const loose = new Set(graph.items.filter((i) => !CONTAINERS.has(i.kind) && !i.parent && !linked.has(i.id)).map((i) => i.id))
  const boxes = graph.items.filter((i) => CONTAINERS.has(i.kind))
  const children: ElkNode[] = [
    ...boxes
      .map((b) => ({
        id: b.id,
        layoutOptions: { "elk.padding": "[top=52,left=24,bottom=24,right=24]" },
        children: graph.items.filter((i) => i.parent === b.id).map(node),
      }))
      .filter((b) => b.children.length > 0),
    ...graph.items.filter((i) => !CONTAINERS.has(i.kind) && !i.parent && !loose.has(i.id)).map(node),
  ]
  const root = await elk.layout({
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      // Edge routes in the map's own coordinates, whichever box they cross.
      "elk.json.edgeCoords": "ROOT",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.direction": "RIGHT",
      "elk.hierarchyHandling": "INCLUDE_CHILDREN",
      "elk.layered.spacing.nodeNodeBetweenLayers": "110",
      "elk.layered.spacing.edgeNodeBetweenLayers": "30",
      "elk.spacing.nodeNode": "36",
      "elk.spacing.componentComponent": "60",
      "elk.layered.nodePlacement.strategy": "BRANDES_KOEPF",
    },
    children,
    edges: graph.edges.map((e) => ({ id: e.id, sources: [e.source], targets: [e.target] })),
  })
  const out: Record<string, Placed> = {}
  const walk = (n: ElkNode) => {
    for (const c of n.children ?? []) {
      // Relative to its parent, as React Flow places a child.
      out[c.id] = { x: c.x ?? 0, y: c.y ?? 0, width: c.width ?? 0, height: c.height ?? 0 }
      walk(c)
    }
  }
  walk(root)

  // ELK routes each edge around the boxes; drawn as it routed them, an edge
  // does not cut across a box on its way.
  const routes: Layout["routes"] = {}
  const collect = (n: ElkNode) => {
    for (const e of n.edges ?? []) {
      const sec = (e as { sections?: { startPoint: { x: number; y: number }; endPoint: { x: number; y: number }; bendPoints?: { x: number; y: number }[] }[] }).sections?.[0]
      if (sec) routes[e.id] = [sec.startPoint, ...(sec.bendPoints ?? []), sec.endPoint]
    }
    for (const c of n.children ?? []) collect(c)
  }
  collect(root)

  const top = Object.entries(out).filter(([id]) => !graph.items.find((i) => i.id === id)?.parent)
  const bottom = top.reduce((b, [, p]) => Math.max(b, p.y + p.height), 0)
  const cols = Math.max(3, Math.ceil(Math.sqrt(loose.size * 2)))
  const cellW = 260, cellH = 104
  ;[...loose].forEach((id, i) => {
    const it = graph.items.find((x) => x.id === id)!
    out[id] = { x: (i % cols) * cellW, y: (bottom ? bottom + 60 : 0) + Math.floor(i / cols) * cellH, ...sizeOf(it) }
  })
  return { placed: out, routes }
}
