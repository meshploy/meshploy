import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, Plus, Server } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { services as servicesApi, type ApiService } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import type { ServiceStatus } from "@/types"

function ServiceCard({ svc, onClick }: { svc: ApiService; onClick: () => void }) {
  const statusStyle = STATUS_STYLES[svc.status] ?? STATUS_STYLES.stopped
  return (
    <div
      onClick={onClick}
      className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4 hover:border-border transition-all cursor-pointer"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted border border-border/60 shrink-0">
            <Server className="h-3.5 w-3.5 text-muted-foreground" />
          </div>
          <div>
            <p className="text-sm font-semibold text-foreground leading-tight">{svc.name}</p>
            <p className="text-[11px] text-muted-foreground">port :{(svc.ports?.find((p) => p.is_primary) ?? svc.ports?.[0])?.port ?? "—"} · ×{svc.replicas}</p>
          </div>
        </div>
        <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border shrink-0 ${statusStyle}`}>
          {svc.status}
        </Badge>
      </div>

      <div className="border-t border-border/40 pt-3 grid grid-cols-2 gap-x-4 gap-y-1.5">
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Image</p>
          <code className="text-[11px] font-mono text-muted-foreground truncate block">
            {svc.image || "—"}
          </code>
        </div>
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Updated</p>
          <p className="text-[11px] text-muted-foreground">{formatRelativeTime(new Date(svc.updated_at))}</p>
        </div>
      </div>
    </div>
  )
}

export const Route = createFileRoute("/_app/projects/$id/services/")({
  component: ServicesTab,
})

const STATUS_STYLES: Record<ServiceStatus, string> = {
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
  stopped:   "bg-muted text-muted-foreground border-border",
}

function ServicesTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/services/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()

  const ACTIVE_SERVICE_STATUSES = new Set(["deploying"])

  const { data: allServices = [], isLoading } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const data = query.state.data as ApiService[] | undefined
      return data?.some((s) => ACTIVE_SERVICE_STATUSES.has(s.status)) ? 5000 : false
    },
  })

  const serviceList = allServices.filter((s) => s.type === "application")

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
          onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "service" } })}
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
            onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "service" } })}
          >
            <Plus className="h-3.5 w-3.5" />
            New Service
          </Button>
        </div>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {serviceList.map((svc) => (
            <ServiceCard
              key={svc.id}
              svc={svc}
              onClick={() => navigate({ to: "/projects/$id/services/$serviceId", params: { id: projectId, serviceId: svc.id } })}
            />
          ))}
        </div>
      )}
    </div>
  )
}
