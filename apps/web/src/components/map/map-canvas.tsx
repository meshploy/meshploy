import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import {
  BaseEdge, Background, BackgroundVariant, Controls, EdgeLabelRenderer, Handle, MarkerType, MiniMap, Position, ReactFlow,
  getSmoothStepPath, type Edge, type EdgeProps, type Node, type NodeProps,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import {
  AlertTriangle, ArrowRight, Box, Database, FolderKanban, Globe, HardDrive, Hourglass, Layers, Loader2, Network, Server, X,
} from "lucide-react"
import type { Health, MapEdge, MapItem, ProjectGraph } from "@/lib/map/graph"
import { CONTAINERS, layoutGraph, sizeOf, type Layout } from "@/lib/map/layout"
import { cn } from "@/lib/utils"

// The drawing both maps share: boxes that say how each thing is, edges for how
// they connect, and a panel for the one clicked. What goes in the panel is the
// page's: a service offers Promote, a node its machine.

export const HEALTH: Record<Health, { ring: string; dot: string; text: string; label: string }> = {
  ok:      { ring: "border-border/70",                   dot: "bg-emerald-400",         text: "text-emerald-400",      label: "Healthy" },
  warn:    { ring: "border-amber-500/50",                dot: "bg-amber-400",           text: "text-amber-400",        label: "Needs a look" },
  waiting: { ring: "border-amber-500/40 border-dashed",  dot: "bg-amber-400/70",        text: "text-amber-300",        label: "Waiting" },
  bad:     { ring: "border-destructive/70",              dot: "bg-destructive",         text: "text-destructive",      label: "Down" },
  idle:    { ring: "border-border/50",                   dot: "bg-muted-foreground/40", text: "text-muted-foreground", label: "Stopped" },
}

const ICON = {
  route: Globe, service: Box, database: Database, volume: HardDrive, stack: Layers,
  cluster: Network, node: Server, project: FolderKanban, level: Layers,
}

type ItemNode = Node<{ item: MapItem }>

// What is selected and what is followed, read by each box. Kept out of the
// nodes themselves: a new node object is one React Flow hides until it has
// measured it again, and a hidden node under the pointer ends the hover that
// made it, which rebuilt the nodes again - a flicker without end.
const FocusContext = createContext<{ selectedId: string | null; near: Set<string> | null }>({ selectedId: null, near: null })

function useFocus(item: MapItem) {
  const { selectedId, near } = useContext(FocusContext)
  return { selected: item.id === selectedId, dim: !!near && !near.has(item.id) && !CONTAINERS.has(item.kind) }
}

function ItemBox({ data }: NodeProps<ItemNode>) {
  const { item } = data
  const { selected, dim } = useFocus(item)
  const h = HEALTH[item.health]
  const Icon = ICON[item.kind]
  const top = item.problems[0]
  return (
    <div className={cn("h-full w-full rounded-xl border bg-card px-3 py-2.5 shadow-sm transition-opacity", h.ring,
      selected && "ring-2 ring-primary/60", dim && "opacity-30")}>
      <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-border" />
      <div className="flex min-w-0 items-center gap-2">
        <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate text-[13px] font-medium">{item.label}</span>
        <span className={cn("ml-auto h-2 w-2 shrink-0 rounded-full", h.dot, item.health === "bad" && "animate-pulse")} title={h.label} />
      </div>
      {item.sub && <p className="mt-0.5 truncate text-[11px] text-muted-foreground">{item.sub}</p>}
      {top && !item.quiet && item.kind !== "route" && item.kind !== "volume" && (
        <p className={cn("mt-1 flex items-center gap-1 truncate text-[11px]", top.severity === "bad" ? "text-destructive" : "text-amber-400")}>
          {item.health === "waiting" ? <Hourglass className="h-3 w-3 shrink-0" /> : <AlertTriangle className="h-3 w-3 shrink-0" />}
          <span className="truncate">{top.title}</span>
        </p>
      )}
      {item.load && (
        <div className="mt-1.5 grid grid-cols-2 gap-2">
          <LoadBar label="mem" value={item.load.memory} />
          <LoadBar label="disk" value={item.load.disk} />
        </div>
      )}
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-border" />
    </div>
  )
}

/** How full a machine is: amber past 85%, where a deploy starts to fail for room. */
function LoadBar({ label, value }: { label: string; value: number }) {
  const pct = Math.round(Math.min(1, Math.max(0, value)) * 100)
  return (
    <div className="min-w-0" title={`${label} ${pct}% in use`}>
      <div className="flex justify-between text-[10px] text-muted-foreground"><span>{label}</span><span className="tabular-nums">{pct}%</span></div>
      <div className="mt-0.5 h-1 overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full", pct >= 85 ? "bg-amber-400" : "bg-emerald-400/70")} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

function ContainerBox({ data }: NodeProps<ItemNode>) {
  const { item } = data
  const { selected } = useFocus(item)
  const h = HEALTH[item.health]
  const Icon = ICON[item.kind]
  return (
    <div className={cn("h-full w-full rounded-2xl border bg-muted/[0.06]", item.health === "ok" ? "border-border/50" : h.ring,
      selected && "ring-2 ring-primary/60")}>
      <Handle type="target" position={Position.Left} className="!opacity-0" />
      <div className="flex items-center gap-2 px-4 pt-3">
        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{item.label}</span>
        {item.sub && <span className={cn("text-[11px]", item.health === "ok" ? "text-muted-foreground/70" : h.text)}>{item.sub}</span>}
      </div>
      <Handle type="source" position={Position.Right} className="!opacity-0" />
    </div>
  )
}

const nodeTypes = { item: ItemBox, container: ContainerBox }

type Point = { x: number; y: number }

/** A path through points, its corners rounded. */
function roundedPath(points: Point[], r = 8): string {
  let d = `M ${points[0].x} ${points[0].y}`
  for (let i = 1; i < points.length - 1; i++) {
    const p = points[i], a = points[i - 1], b = points[i + 1]
    const inLen = Math.hypot(p.x - a.x, p.y - a.y), outLen = Math.hypot(b.x - p.x, b.y - p.y)
    const k = Math.min(r, inLen / 2, outLen / 2)
    const p1 = { x: p.x - ((p.x - a.x) / (inLen || 1)) * k, y: p.y - ((p.y - a.y) / (inLen || 1)) * k }
    const p2 = { x: p.x + ((b.x - p.x) / (outLen || 1)) * k, y: p.y + ((b.y - p.y) / (outLen || 1)) * k }
    d += ` L ${p1.x} ${p1.y} Q ${p.x} ${p.y} ${p2.x} ${p2.y}`
  }
  const last = points[points.length - 1]
  return d + ` L ${last.x} ${last.y}`
}

// Drawn along the route ELK worked out, which goes around the boxes; without
// one, a plain step path between the handles.
function RoutedEdge(props: EdgeProps<Edge<{ route?: Point[] }>>) {
  const { data, label, markerEnd, style, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition } = props
  let path: string, lx: number, ly: number
  const route = data?.route
  if (route && route.length >= 2) {
    path = roundedPath(route)
    const mid = route[Math.floor(route.length / 2)], before = route[Math.floor(route.length / 2) - 1] ?? mid
    lx = (mid.x + before.x) / 2
    ly = (mid.y + before.y) / 2
  } else {
    ;[path, lx, ly] = getSmoothStepPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, borderRadius: 10 })
  }
  return (
    <>
      <BaseEdge path={path} markerEnd={markerEnd} style={style} />
      {label && (
        <EdgeLabelRenderer>
          <div className="nodrag nopan pointer-events-none absolute rounded bg-background/90 px-1 text-[10px] text-muted-foreground"
            style={{ transform: `translate(-50%, -50%) translate(${lx}px, ${ly}px)` }}>{label}</div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

const edgeTypes = { routed: RoutedEdge }

const STROKE = { base: "oklch(0.55 0.02 260)", soft: "oklch(0.6 0.02 260)", broken: "oklch(0.75 0.15 70)", go: "oklch(0.72 0.15 160)" }

function edgeStyle(e: MapEdge, route: Point[] | undefined, focus: "on" | "off" | "none"): Edge {
  const color = e.broken ? STROKE.broken : e.kind === "promotes" && e.waiting ? STROKE.go : e.kind === "reads" || e.kind === "runs" ? STROKE.soft : STROKE.base
  // Where things run is many lines at once: faint until something is followed.
  const opacity = focus === "on" ? 1 : focus === "off" ? 0.12 : e.kind === "runs" ? 0.35 : 1
  return {
    // A depends_on condition is said in the panel: on the map, several met
    // at one box and covered each other.
    id: e.id, source: e.source, target: e.target, label: e.kind === "depends" ? undefined : e.label, zIndex: 2, type: "routed",
    data: { route }, animated: !!e.broken || !!e.waiting,
    style: { stroke: color, opacity, strokeWidth: e.broken || focus === "on" ? 2 : 1.25, strokeDasharray: e.kind === "reads" || e.kind === "runs" ? "4 4" : undefined },
    labelStyle: { fill: "oklch(0.7 0.02 260)", fontSize: 10 },
    labelBgStyle: { fill: "oklch(0.18 0.01 260)" },
    markerEnd: e.kind === "mesh" ? undefined : { type: MarkerType.ArrowClosed, width: 14, height: 14, color },
  }
}

export function MapCanvas({ graph, selectedId, onSelect, panel }: {
  graph: ProjectGraph | null
  selectedId: string | null
  onSelect: (id: string | null) => void
  /** What the page shows beside the map for the item clicked. */
  panel?: ReactNode
}) {
  const [layout, setLayout] = useState<Layout | null>(null)
  const placed = layout?.placed ?? null
  const layoutKey = graph ? graph.items.map((i) => `${i.id}:${i.parent ?? ""}:${sizeOf(i).height}`).join() + "|" + graph.edges.map((e) => e.id).join() : ""
  // What is followed: hovered, or else clicked. It, what it connects to and
  // the edges between them stay; the rest steps back.
  const [hoverId, setHoverId] = useState<string | null>(null)
  const focusId = hoverId ?? selectedId
  const near = useMemo(() => {
    if (!graph || !focusId) return null
    const ids = new Set([focusId])
    for (const e of graph.edges) {
      if (e.source === focusId) ids.add(e.target)
      if (e.target === focusId) ids.add(e.source)
    }
    // A box around the followed one is not dimmed around it.
    for (const it of graph.items) if (it.id === focusId && it.parent) ids.add(it.parent)
    for (const it of graph.items) if (it.parent === focusId) ids.add(it.id)
    return ids
  }, [graph, focusId])
  useEffect(() => {
    if (!graph) return
    let live = true
    layoutGraph(graph).then((l) => live && setLayout(l))
    return () => { live = false }
    // Laid out again only when what is on the map changes, not its health.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutKey])

  const nodes: ItemNode[] = useMemo(() => {
    if (!graph || !placed) return []
    // A box before what is in it: React Flow places a child in a parent it has seen.
    const depth = (it: MapItem): number => (it.parent ? 1 + depth(graph.items.find((p) => p.id === it.parent) ?? it) : 0)
    const ordered = [...graph.items].sort((a, b) => depth(a) - depth(b))
    return ordered.filter((it) => placed[it.id]).map((it) => ({
      id: it.id,
      type: CONTAINERS.has(it.kind) ? "container" : "item",
      position: { x: placed[it.id].x, y: placed[it.id].y },
      // Its size given, so React Flow has nothing to measure before showing it.
      width: placed[it.id].width, height: placed[it.id].height,
      style: { width: placed[it.id].width, height: placed[it.id].height },
      parentId: it.parent && placed[it.parent] ? it.parent : undefined,
      data: { item: it },
      draggable: false,
      zIndex: CONTAINERS.has(it.kind) ? 0 : 1,
    }))
  }, [graph, placed])

  const focusValue = useMemo(() => ({ selectedId, near }), [selectedId, near])
  const edges = useMemo(() => (graph?.edges ?? []).map((e) => edgeStyle(e, layout?.routes[e.id],
    !focusId ? "none" : e.source === focusId || e.target === focusId ? "on" : "off")), [graph, layout, focusId])

  return (
    <div className="relative h-[calc(100vh-15rem)] min-h-[480px] overflow-hidden rounded-xl border border-border bg-background">
      {!graph || !placed ? (
        <div className="flex h-full items-center justify-center"><Loader2 className="h-4 w-4 animate-spin text-muted-foreground" /></div>
      ) : graph.items.length === 0 ? (
        <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Nothing here yet.</div>
      ) : (
        <FocusContext.Provider value={focusValue}>
        <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} colorMode="dark" fitView fitViewOptions={{ padding: 0.15 }}
          minZoom={0.15} maxZoom={1.8} nodesConnectable={false} elementsSelectable proOptions={{ hideAttribution: true }}
          onNodeClick={(_, n) => onSelect(n.id)} onPaneClick={() => onSelect(null)}
          onNodeMouseEnter={(_, n) => !CONTAINERS.has((n.data as ItemNode["data"]).item.kind) && setHoverId(n.id)}
          onNodeMouseLeave={() => setHoverId(null)}>
          <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="oklch(0.3 0.01 260)" />
          <Controls showInteractive={false} />
          {/* Only where there is enough to get lost in. */}
          {graph.items.length > 24 && <MiniMap pannable zoomable className="!bg-card" maskColor="oklch(0.15 0.01 260 / 0.6)"
            nodeColor={(n) => {
              const h = (n.data as ItemNode["data"]).item.health
              return h === "bad" ? "oklch(0.6 0.2 25)" : h === "warn" || h === "waiting" ? "oklch(0.75 0.15 70)" : n.type === "container" ? "oklch(0.25 0.01 260)" : "oklch(0.45 0.02 260)"
            }} />}
        </ReactFlow>
        </FocusContext.Provider>
      )}
      {panel}
    </div>
  )
}

const LEGEND: Partial<Record<Health, { dot: string; label: string }>> = {
  bad: { dot: "bg-destructive", label: "down" },
  waiting: { dot: "bg-amber-400/70", label: "waiting" },
  warn: { dot: "bg-amber-400", label: "need a look" },
}

/** Counts of the states a map can show: the workspace has nothing that waits. */
export function MapLegend({ graph, kinds, states = ["bad", "waiting", "warn"] }: {
  graph: ProjectGraph | null; kinds: MapItem["kind"][]; states?: Health[]
}) {
  if (!graph) return null
  const it = graph.items.filter((i) => kinds.includes(i.kind))
  return (
    <div className="flex items-center gap-3 text-xs">
      {states.map((h) => LEGEND[h] && (
        <Legend key={h} dot={LEGEND[h]!.dot} label={`${it.filter((i) => i.health === h).length} ${LEGEND[h]!.label}`} />
      ))}
    </div>
  )
}

function Legend({ dot, label }: { dot: string; label: string }) {
  return <span className="flex items-center gap-1.5 text-muted-foreground"><span className={cn("h-2 w-2 rounded-full", dot)} />{label}</span>
}

const VERB: Record<MapEdge["kind"], [string, string]> = {
  serves: ["Serves", "Served by"],
  depends: ["Waits for", "Waited for by"],
  mounts: ["Stores data in", "Holds the data of"],
  reads: ["Reads variables of", "Its variables are read by"],
  runs: ["Runs on", "Runs"],
  mesh: ["On the mesh with", "On the mesh with"],
  promotes: ["Promotes to", "Promoted from"],
}

// The frame of the panel: the item's state, its problems with their fixes,
// what it connects to, and whatever the page adds (actions) below them.
export function MapPanel({ item, graph, onClose, onSelect, children }: {
  item: MapItem; graph: ProjectGraph; onClose: () => void; onSelect: (id: string) => void; children?: ReactNode
}) {
  const h = HEALTH[item.health]
  const name = (id: string) => graph.items.find((i) => i.id === id)?.label ?? id
  const out = graph.edges.filter((e) => e.source === item.id)
  const into = graph.edges.filter((e) => e.target === item.id)
  return (
    <aside className="absolute bottom-3 right-3 top-3 z-10 flex w-80 flex-col overflow-hidden rounded-xl border border-border bg-card shadow-xl" data-testid="map-panel">
      <div className="flex items-start justify-between gap-2 border-b border-border/60 px-4 py-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{item.label}</p>
          <p className={cn("text-xs", h.text)}>{h.label}{item.sub ? <span className="text-muted-foreground"> · {item.sub}</span> : null}</p>
        </div>
        <button type="button" onClick={onClose} aria-label="Close" className="text-muted-foreground hover:text-foreground"><X className="h-4 w-4" /></button>
      </div>
      <div className="flex-1 space-y-4 overflow-y-auto px-4 py-3 text-xs">
        {item.problems.length === 0 ? (
          <p className="text-muted-foreground">Nothing wrong here.</p>
        ) : item.problems.map((p, i) => (
          <div key={i} className={cn("rounded-lg border px-3 py-2", p.severity === "bad" ? "border-destructive/40 bg-destructive/5" : "border-amber-500/30 bg-amber-500/5")}>
            <p className={cn("font-medium", p.severity === "bad" ? "text-destructive" : "text-amber-300")}>{p.title}</p>
            {p.detail && <p className="mt-0.5 text-muted-foreground">{p.detail}</p>}
            {p.fix?.to && (
              <Link to={p.fix.to} className="mt-1.5 inline-flex items-center gap-1 text-primary hover:underline">{p.fix.label}<ArrowRight className="h-3 w-3" /></Link>
            )}
          </div>
        ))}
        {children}
        {(out.length > 0 || into.length > 0) && (
          <div className="space-y-1.5">
            <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Connections</p>
            {out.map((e) => (
              <button key={e.id} type="button" onClick={() => onSelect(e.target)} className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1 text-left hover:bg-muted/40">
                <span className="shrink-0 text-muted-foreground">{VERB[e.kind][0]}</span>
                <span className={cn("truncate font-medium", e.broken && "text-amber-300")}>{name(e.target)}{e.label ? <span className="text-muted-foreground"> ({e.label})</span> : null}</span>
              </button>
            ))}
            {into.map((e) => (
              <button key={e.id} type="button" onClick={() => onSelect(e.source)} className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1 text-left hover:bg-muted/40">
                <span className="shrink-0 text-muted-foreground">{VERB[e.kind][1]}</span>
                <span className="truncate font-medium">{name(e.source)}</span>
              </button>
            ))}
          </div>
        )}
      </div>
      {item.to && (
        <div className="border-t border-border/60 px-4 py-2.5">
          <Link to={item.to} className="inline-flex items-center gap-1 text-xs text-primary hover:underline">
            Open {CONTAINERS.has(item.kind) ? `the ${item.kind}` : item.label}<ArrowRight className="h-3 w-3" />
          </Link>
        </div>
      )}
    </aside>
  )
}
