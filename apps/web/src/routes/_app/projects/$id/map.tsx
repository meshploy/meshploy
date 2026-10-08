import { useMemo, useState } from "react"
import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { projects as projectsApi, type ApiProjectMap } from "@/lib/api"
import { buildProjectGraph, type ProjectGraph } from "@/lib/map/graph"
import { MapCanvas, MapLegend, MapPanel } from "@/components/map/map-canvas"
import { ServiceActions } from "@/components/map/service-actions"
import { useAuthStore } from "@/store/auth-store"
import { useProjectLimited } from "@/components/projects/use-project-limited"
import { useOrgStore } from "@/store/org-store"
import { livePoll } from "@/lib/live-poll"
import { cn } from "@/lib/utils"
import { FlaskConical, X } from "lucide-react"
import { Button } from "@/components/ui/button"

export const Route = createFileRoute("/_app/projects/$id/map")({
  // ?down=<ids>: services and volumes taken out as a what-if, as the
  // workspace map's forecast links here with what a node going down would stop.
  validateSearch: (search: Record<string, unknown>): { down?: string } =>
    typeof search.down === "string" && search.down ? { down: search.down } : {},
  component: MapPage,
})

// A level of a project drawn as what it is: routes, the services they reach,
// what those wait on, and where their data lives, each saying whether it is
// well and, if not, why and what fixes it. Read and fix only: the wiring is
// changed where it is written, in the compose file and the forms. Promote and
// Roll back are fixes, so the panel offers them.

function MapPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/map" })
  const { down } = Route.useSearch()
  const navigate = Route.useNavigate()
  const takenOutKey = down ?? ""
  const setTakenOut = (ids: string[]) => navigate({ search: ids.length ? { down: ids.join(",") } : {}, replace: true })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const on = !!orgId
  const limited = useProjectLimited(orgId, projectId, token)

  // Everything the map is drawn from, in one read; faster while something deploys.
  const map = useQuery({
    queryKey: ["project-map", orgId, projectId],
    queryFn: () => projectsApi.map(orgId!, projectId, token),
    enabled: on && !limited,
    refetchInterval: livePoll((d: ApiProjectMap) => d.services.some((s) => s.status === "deploying")),
  })
  const levels = useQuery({ queryKey: ["environments", orgId, projectId], queryFn: () => projectsApi.environments(orgId!, projectId, token), enabled: on })

  const graph: ProjectGraph | null = useMemo(() => {
    if (!map.data) return null
    const m = map.data
    return buildProjectGraph({
      projectId, services: m.services, stacks: m.stacks, routes: m.routes, volumes: m.volumes,
      troubles: m.troubles ?? {}, hints: m.hints ?? {}, reads: m.reads ?? {},
      takenOut: new Set(takenOutKey.split(",").filter(Boolean)),
    })
  }, [map.data, projectId, takenOutKey])
  const takenOut = takenOutKey.split(",").filter(Boolean)
  const waiting = graph?.items.filter((i) => i.health === "waiting").length ?? 0
  const routesHit = graph?.items.filter((i) => i.kind === "route" && i.problems.some((p) => p.title.startsWith("Serves "))).length ?? 0

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const selected = graph?.items.find((i) => i.id === selectedId) ?? null
  const sortedLevels = [...(levels.data ?? [])].sort((a, b) => b.level - a.level)

  // The map is the whole project's; someone given only some of it opens the
  // project at its overview, which shows what they have.
  if (limited) return (
    <div className="console-page p-6 space-y-3">
      <h1 className="text-base font-semibold">Map</h1>
      <p className="text-sm text-muted-foreground">
        The map shows the whole project, and you were given only some of it.{" "}
        <Link to="/projects/$id" params={{ id: projectId }} className="text-primary hover:underline">See what you have</Link>
      </p>
    </div>
  )

  return (
    <div className="console-page p-6 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="space-y-1.5">
          <h1 className="text-base font-semibold">Map</h1>
          <p className="text-xs text-muted-foreground">What serves what, what waits on what, and what needs fixing. Click anything for why.</p>
        </div>
        <div className="flex flex-wrap items-center gap-4">
          {sortedLevels.length > 1 && (
            // The levels, lowest first, as the board reads: each its own map.
            <div className="flex items-center gap-1 rounded-lg border border-border/60 p-0.5" aria-label="Level">
              {sortedLevels.map((l) => (
                <Link key={l.project_id} to="/projects/$id/map" params={{ id: l.project_id }}
                  className={cn("rounded-md px-2.5 py-1 text-xs", l.project_id === projectId ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground")}>
                  <span className={cn("mr-1.5 inline-block h-1.5 w-1.5 rounded-full", l.production ? "bg-emerald-400" : "bg-sky-400")} />{l.name}
                </Link>
              ))}
            </div>
          )}
          <MapLegend graph={graph} kinds={["service", "database"]} />
        </div>
      </div>

      {takenOut.length > 0 && graph && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-xs" data-testid="what-if-banner">
          <p className="flex items-center gap-2">
            <FlaskConical className="h-3.5 w-3.5 text-amber-300" />
            <span className="font-medium text-amber-200">
              What if {takenOut.map((id) => graph.items.find((i) => i.id === id)?.label ?? "something").join(", ")} went down
            </span>
            <span className="text-muted-foreground">{waiting} would wait on {takenOut.length === 1 ? "it" : "them"} · {routesHit} route{routesHit === 1 ? "" : "s"} affected. Nothing is stopped.</span>
          </p>
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => setTakenOut([])}><X className="h-3 w-3" />Clear</Button>
        </div>
      )}

      {/* A map that cannot be read says so, rather than loading forever. */}
      {map.isError && <p role="alert" className="text-sm text-destructive">The map could not be loaded: {map.error.message}</p>}

      <MapCanvas graph={graph} selectedId={selectedId} onSelect={setSelectedId}
        panel={selected && graph && orgId && (
          <MapPanel item={selected} graph={graph} onClose={() => setSelectedId(null)} onSelect={setSelectedId}>
            {(selected.kind === "service" || selected.kind === "database" || selected.kind === "volume") && (
              takenOut.includes(selected.id) ? (
                <Button size="sm" variant="outline" className="h-7 w-full gap-1.5 text-xs" onClick={() => setTakenOut(takenOut.filter((id) => id !== selected.id))}>
                  <X className="h-3 w-3" />Put {selected.label} back
                </Button>
              ) : (
                <Button size="sm" variant="outline" className="h-7 w-full gap-1.5 text-xs" onClick={() => setTakenOut([...takenOut, selected.id])}>
                  <FlaskConical className="h-3 w-3" />What if {selected.label} went down?
                </Button>
              )
            )}
            {selected.service && <ServiceActions orgId={orgId} levelId={projectId} service={selected.service} token={token} />}
          </MapPanel>
        )} />
    </div>
  )
}
