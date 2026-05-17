import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { ExternalLink, Globe, Loader2, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { routes as routesApi, type ApiDbRoute } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id/routes/")({
  component: RoutesTab,
})

const ZONE_STYLES: Record<string, string> = {
  public:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  external: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  internal: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  preview:  "bg-blue-500/10 text-blue-400 border-blue-500/20",
}

function RoutesTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/routes/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()

  const { data: routeList = [], isLoading } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const goToNew = () =>
    navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "route" } })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Routes</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && <span className="text-xs text-muted-foreground">{routeList.length}</span>}
        </div>
        <Button size="sm" className="gap-1.5" onClick={goToNew}>
          <Plus className="h-3.5 w-3.5" />
          New Route
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : routeList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Globe className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No routes configured</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Map a hostname to a service or mesh device</p>
          </div>
          <Button size="sm" className="gap-1.5 mt-1" onClick={goToNew}>
            <Plus className="h-3.5 w-3.5" />
            New Route
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border/40 bg-muted/20">
                <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider w-[45%]">Hostname</th>
                <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider w-[12%]">Zone</th>
                <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Paths</th>
                <th className="w-10" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border/30">
              {routeList.map((route) => (
                <RouteRow
                  key={route.id}
                  route={route}
                  onClick={() => navigate({ to: "/projects/$id/routes/$routeId", params: { id: projectId, routeId: route.id } })}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function RouteRow({ route, onClick }: { route: ApiDbRoute; onClick: () => void }) {
  const MAX_PATHS = 3
  const shown = route.targets.slice(0, MAX_PATHS)
  const overflow = route.targets.length - MAX_PATHS

  return (
    <tr
      className="hover:bg-muted/20 transition-colors cursor-pointer"
      onClick={onClick}
    >
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <Globe className="h-3.5 w-3.5 text-muted-foreground/50 shrink-0" />
          <span className="font-medium text-foreground font-mono text-sm">{route.hostname}</span>
        </div>
      </td>
      <td className="px-4 py-3">
        <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border ${ZONE_STYLES[route.zone] ?? "bg-muted text-muted-foreground border-border"}`}>
          {route.zone}
        </Badge>
      </td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap items-center gap-1">
          {route.targets.length === 0 ? (
            <span className="text-xs text-muted-foreground/40">—</span>
          ) : (
            <>
              {shown.map((t) => (
                <span key={t.id} className="flex items-center gap-0.5">
                  <code className="text-[10px] font-mono bg-muted/50 border border-border/40 px-1.5 py-0.5 rounded text-muted-foreground">
                    {t.path}
                  </code>
                  {t.redirect_route_id && (
                    <span className="text-[9px] font-medium text-amber-400 bg-amber-500/10 border border-amber-500/20 px-1 py-0.5 rounded">
                      ↪ {t.redirect_code || 301}
                    </span>
                  )}
                </span>
              ))}
              {overflow > 0 && (
                <span className="text-[10px] text-muted-foreground/50">+{overflow} more</span>
              )}
            </>
          )}
        </div>
      </td>
      <td className="px-3 py-3 text-right">
        <a
          href={`https://${route.hostname}`}
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e) => e.stopPropagation()}
          className="text-muted-foreground/40 hover:text-muted-foreground transition-colors"
        >
          <ExternalLink className="h-3.5 w-3.5" />
        </a>
      </td>
    </tr>
  )
}
