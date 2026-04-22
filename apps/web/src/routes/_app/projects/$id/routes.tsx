import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Globe, Loader2, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { routes as routesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id/routes")({
  component: RoutesTab,
})

function RoutesTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/routes" })
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
          {!isLoading && (
            <span className="text-xs text-muted-foreground">{routeList.length}</span>
          )}
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
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {routeList.map((route) => (
            <div
              key={route.id}
              className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/20 transition-colors cursor-pointer"
              onClick={() =>
                navigate({
                  to: "/projects/$id/routes/$routeId",
                  params: { id: projectId, routeId: route.id },
                })
              }
            >
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-foreground font-mono">{route.hostname}</p>
                <code className="text-[11px] font-mono text-muted-foreground/70 mt-0.5 block truncate">
                  → {route.target_ip}:{route.target_port}
                </code>
              </div>
              <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4.5 shrink-0">
                {route.zone}
              </Badge>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
