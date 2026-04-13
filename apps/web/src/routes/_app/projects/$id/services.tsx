import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, Plus, Server } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { services as servicesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { ServiceStatus } from "@/types"

export const Route = createFileRoute("/_app/projects/$id/services")({
  component: ServicesTab,
})

const STATUS_STYLES: Record<ServiceStatus, string> = {
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
  stopped:   "bg-muted text-muted-foreground border-border",
}

function ServicesTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/services" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: serviceList = [], isLoading } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Services</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && (
            <span className="text-xs text-muted-foreground">{serviceList.length}</span>
          )}
        </div>
        <Button
          size="sm"
          className="gap-1.5"
          render={<Link to="/projects/$id/new-service" params={{ id: projectId }} />}
        >
          <Plus className="h-3.5 w-3.5" />
          New Service
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : serviceList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Server className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No services yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Deploy your first service to get started</p>
          </div>
          <Button
            size="sm"
            className="gap-1.5 mt-1"
            render={<Link to="/projects/$id/new-service" params={{ id: projectId }} />}
          >
            <Plus className="h-3.5 w-3.5" />
            New Service
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {serviceList.map((svc) => (
            <div
              key={svc.id}
              className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/20 transition-colors cursor-pointer"
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <p className="text-sm font-medium text-foreground">{svc.name}</p>
                  <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border ${STATUS_STYLES[svc.status]}`}>
                    {svc.status}
                  </Badge>
                  <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-4.5">
                    {svc.type}
                  </Badge>
                </div>
                <code className="text-[11px] font-mono text-muted-foreground/70 mt-0.5 block truncate">
                  {svc.image}
                </code>
              </div>
              <span className="text-xs text-muted-foreground shrink-0">
                {svc.type === "database" ? "1 replica" : `×${svc.replicas}`}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
