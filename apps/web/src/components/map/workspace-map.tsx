import { useMemo, useState } from "react"
import { Link } from "@tanstack/react-router"
import { useQueries, useQuery } from "@tanstack/react-query"
import { ArrowRight, FlaskConical, X } from "lucide-react"
import {
  activity as activityApi, nodes as nodesApi, placement as placementApi, projects as projectsApi,
  toNode, type ApiNodeDownForecast,
} from "@/lib/api"
import { buildWorkspaceGraph, type WorkspaceItem } from "@/lib/map/workspace"
import { MapCanvas, MapLegend, MapPanel } from "@/components/map/map-canvas"
import { Button } from "@/components/ui/button"
import { useMeshLoad } from "@/components/system/use-mesh-load"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin } from "@/store/org-store"
import { cn } from "@/lib/utils"

// The workspace around its machines: each node, and the project levels whose
// pods are on it. It answers what depends on a machine, and, taken out as a
// what-if, what would move and what would stay down. Shown on the overview,
// as the other way to read it. Where everything runs, and the forecast, come
// from the server's placement endpoint, one read of the cluster.

export function WorkspaceMap() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const isAdmin = useIsAdmin()
  const on = !!orgId

  const nodeList = useQuery({ queryKey: ["nodes", orgId], queryFn: () => nodesApi.list(orgId!, token), enabled: on, refetchInterval: 30_000 })
  const projectList = useQuery({ queryKey: ["projects", orgId, "map"], queryFn: () => projectsApi.list(orgId!, token), enabled: on })
  const levelQs = useQueries({
    queries: (projectList.data ?? []).map((p) => ({
      queryKey: ["environments", orgId, p.id],
      queryFn: () => projectsApi.environments(orgId!, p.id, token),
      enabled: on,
    })),
  })
  const overview = useQuery({ queryKey: ["overview", orgId], queryFn: () => activityApi.overview(orgId!, token), enabled: on, refetchInterval: 30_000 })
  // The levels of a project seen only for what was granted inside it: their
  // map is the whole project's, so they are opened at their overview instead.
  const limitedLevels = new Set((projectList.data ?? []).flatMap((p, i) =>
    p.limited ? [p.id, ...(levelQs[i]?.data ?? []).map((l) => l.project_id)] : []))
  // Spans every project, so org admins only; a member sees the machines and
  // levels without what runs where.
  const placement = useQuery({ queryKey: ["placement", orgId], queryFn: () => placementApi.get(orgId!, token), enabled: on && isAdmin, refetchInterval: 30_000 })

  const load = useMeshLoad((nodeList.data ?? []).map(toNode))
  const [whatIfNode, setWhatIfNode] = useState<string | null>(null)
  const whatIf = useQuery({
    queryKey: ["node-what-if", orgId, whatIfNode, placement.dataUpdatedAt],
    queryFn: () => placementApi.nodeWhatIf(orgId!, whatIfNode!, token),
    enabled: on && !!whatIfNode,
  })
  const forecast: ApiNodeDownForecast | null = whatIfNode ? whatIf.data ?? null : null

  const ready = !!(nodeList.data && projectList.data && overview.data && levelQs.every((q) => q.data) && (!isAdmin || placement.data || placement.isError))
  const key = ready ? JSON.stringify([
    nodeList.dataUpdatedAt, projectList.dataUpdatedAt, overview.dataUpdatedAt, placement.dataUpdatedAt,
    ...levelQs.map((q) => q.dataUpdatedAt), load.byNode,
  ]) : ""
  const placements = useMemo(() => placement.data?.services ?? [], [placement.data])

  const graph = useMemo(() => {
    if (!ready) return null
    return buildWorkspaceGraph({
      nodes: nodeList.data!,
      projects: projectList.data!,
      levels: Object.fromEntries(projectList.data!.map((p, i) => [p.id, levelQs[i].data ?? []])),
      attention: overview.data!.attention,
      load: load.byNode,
      placements,
      whatIf: forecast,
    })
    // key is what the per-project reads hold.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, key, placements, forecast])

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const selected = (graph?.items.find((i) => i.id === selectedId) ?? null) as WorkspaceItem | null

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">Each machine, and the project levels running on it. Click a node for what depends on it, and what would happen if it went down.</p>
        <MapLegend graph={graph} kinds={["node", "level"]} states={["bad", "waiting", "warn"]} />
      </div>
      {forecast && <WhatIfBanner forecast={forecast} onClear={() => setWhatIfNode(null)} />}
      <MapCanvas graph={graph} selectedId={selectedId} onSelect={setSelectedId}
        panel={selected && graph && (
          <MapPanel item={selected} graph={graph} onClose={() => setSelectedId(null)} onSelect={setSelectedId}>
            {selected.node && (
              <NodeSide item={selected} canWhatIf={isAdmin} forecast={forecast?.node === (selected.node.k8s_node_name || selected.node.name) ? forecast : null}
                loading={whatIf.isFetching} onWhatIf={() => setWhatIfNode(selected.node!.k8s_node_name || selected.node!.name)} onClear={() => setWhatIfNode(null)} />
            )}
            {selected.levelOf && !limitedLevels.has(selected.levelOf.project_id) && (
              <Link to="/projects/$id/map" params={{ id: selected.levelOf.project_id }}
                className="inline-flex items-center gap-1 rounded-md border border-border/60 px-2.5 py-1.5 text-primary hover:bg-muted/40">
                Open {selected.levelOf.name}&apos;s map<ArrowRight className="h-3 w-3" />
              </Link>
            )}
          </MapPanel>
        )} />
    </div>
  )
}

function WhatIfBanner({ forecast, onClear }: { forecast: ApiNodeDownForecast; onClear: () => void }) {
  const down = forecast.services.filter((f) => f.outcome === "down").length
  const moves = forecast.services.filter((f) => f.outcome === "moves").length
  const keeps = forecast.services.filter((f) => f.outcome === "keeps").length
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-xs" data-testid="what-if-banner">
      <p className="flex items-center gap-2">
        <FlaskConical className="h-3.5 w-3.5 text-amber-300" />
        <span className="font-medium text-amber-200">What if {forecast.node} went down</span>
        <span className="text-muted-foreground">
          {down} would stay down · {moves} would move · {keeps} keep running on other nodes
          {forecast.control_plane ? " · the control plane and the edge go with it: nothing is reachable from outside" : ""}
        </span>
      </p>
      <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={onClear}><X className="h-3 w-3" />Clear</Button>
    </div>
  )
}

// A node's side of the panel: what runs on it, the what-if, and the machine.
function NodeSide({ item, forecast, canWhatIf, loading, onWhatIf, onClear }: {
  item: WorkspaceItem; forecast: ApiNodeDownForecast | null; canWhatIf: boolean; loading: boolean; onWhatIf: () => void; onClear: () => void
}) {
  const n = item.node!
  const runs = item.runs ?? []
  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Runs</p>
        {runs.length === 0 ? <p className="text-muted-foreground">Nothing of any project yet.</p> : runs.map((r) => (
          <div key={r.levelId} className="rounded-md px-2 py-1 hover:bg-muted/30">
            <Link to="/projects/$id/map" params={{ id: r.levelId }} className="font-medium hover:underline">{r.projectName} · {r.levelName}</Link>
            <p className="truncate text-muted-foreground">{r.services.join(", ")}</p>
          </div>
        ))}
      </div>

      {forecast ? <Forecast forecast={forecast} onClear={onClear} /> : canWhatIf && (
        <Button size="sm" variant="outline" className="h-7 w-full gap-1.5 text-xs" onClick={onWhatIf} disabled={runs.length === 0 || loading}>
          <FlaskConical className="h-3 w-3" />{loading ? "Working it out…" : `What if ${n.name} went down?`}
        </Button>
      )}

      <div className="space-y-1">
        <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Machine</p>
        {([
          ["Mesh address", n.tailscale_ip || "-"],
          ["CPU", n.cpu_cores ? `${n.cpu_cores} cores` : "-"],
          ["Memory", n.memory_gb ? `${n.memory_gb.toFixed(1)} GiB` : "-"],
          ["Disk", n.disk_gb ? `${n.disk_gb.toFixed(0)} GB` : "-"],
          ["k3s", n.k3s_version || (n.k8s_member ? "-" : "not in the cluster")],
        ] as [string, string][]).map(([k, v]) => (
          <div key={k} className="flex items-center justify-between gap-2 px-2">
            <span className="text-muted-foreground">{k}</span><span className="truncate font-mono text-[11px]">{v}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// What would happen to each service on the node, level by level, with the
// way into that level's map with what would stay down taken out.
function Forecast({ forecast, onClear }: { forecast: ApiNodeDownForecast; onClear: () => void }) {
  const byLevel = new Map<string, typeof forecast.services>()
  for (const f of forecast.services) byLevel.set(f.service.level_id, [...(byLevel.get(f.service.level_id) ?? []), f])
  return (
    <div className="space-y-2" data-testid="what-if-forecast">
      <div className="flex items-center justify-between">
        <p className="text-[11px] uppercase tracking-wide text-amber-300">If it went down</p>
        <button type="button" onClick={onClear} className="text-[11px] text-muted-foreground hover:text-foreground">Clear</button>
      </div>
      {forecast.control_plane && (
        <p className="rounded-md border border-destructive/40 bg-destructive/5 px-2.5 py-1.5 text-red-300">
          It is the gateway: the cluster's control plane and the edge. Nothing would be rescheduled, and no route would answer from outside.
        </p>
      )}
      {[...byLevel.values()].map((fs) => {
        const p = fs[0].service
        const downIds = fs.filter((f) => f.outcome === "down").map((f) => f.service.id)
        return (
          <div key={p.level_id} className="rounded-md border border-border/60 px-2.5 py-2">
            <p className="font-medium">{p.project_name} · {p.level_name}</p>
            <ul className="mt-1 space-y-0.5">
              {fs.map((f) => (
                <li key={f.service.id} className="flex items-start justify-between gap-2">
                  <span>{f.service.name}</span>
                  <span className={cn("text-right", f.outcome === "down" ? "text-red-300" : f.outcome === "moves" ? "text-amber-300" : "text-emerald-300")}>
                    {f.outcome === "down" ? `stays down: ${f.reason}`
                      : f.outcome === "moves" ? `moves to ${f.to}`
                      : `keeps running on ${(f.on ?? []).join(", ")}`}
                  </span>
                </li>
              ))}
            </ul>
            {downIds.length > 0 && (
              <Link to="/projects/$id/map" params={{ id: p.level_id }} search={{ down: downIds.join(",") }}
                className="mt-1.5 inline-flex items-center gap-1 text-primary hover:underline">
                See what waits on them<ArrowRight className="h-3 w-3" />
              </Link>
            )}
          </div>
        )
      })}
      <p className="text-[11px] text-muted-foreground">A forecast from what each service asks for, each node's room and where data is bound; the scheduler decides on the day.</p>
    </div>
  )
}
