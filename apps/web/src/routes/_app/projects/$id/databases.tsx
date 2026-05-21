import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Database, Loader2, Plus, Table2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import { services as servicesApi, type ApiService } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { useTabStore } from "@/store/tab-store"
import { formatRelativeTime } from "@/lib/utils"

const ENGINE_LABELS: Record<string, string> = {
  postgres: "PostgreSQL",
  mysql:    "MySQL",
  redis:    "Redis",
  mongodb:  "MongoDB",
}

const STATUS_STYLES: Record<string, string> = {
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
  stopped:   "bg-muted text-muted-foreground border-border",
}

function DatabaseCard({
  svc,
  projectId,
  onClick,
}: {
  svc: ApiService
  projectId: string
  onClick: () => void
}) {
  const openTab = useTabStore((s) => s.openTab)
  const statusStyle = STATUS_STYLES[svc.status] ?? STATUS_STYLES.stopped
  const [engineRaw, version] = svc.image.split(":")
  const engineLabel = ENGINE_LABELS[engineRaw] ?? engineRaw

  function handleOpenExplorer(e: React.MouseEvent) {
    e.stopPropagation()
    openTab({
      id: svc.id,
      type: "explorer",
      label: svc.name,
      payload: { serviceId: svc.id, projectId, dbName: svc.name },
    })
  }

  return (
    <div
      onClick={onClick}
      className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4 hover:border-border transition-all cursor-pointer"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted border border-border/60 shrink-0">
            <Database className="h-3.5 w-3.5 text-muted-foreground" />
          </div>
          <div>
            <p className="text-sm font-semibold text-foreground leading-tight">{svc.name}</p>
            <p className="text-[11px] text-muted-foreground">port :{(svc.ports?.find((p) => p.is_primary) ?? svc.ports?.[0])?.port ?? "—"}</p>
          </div>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={handleOpenExplorer}
                  className="flex items-center justify-center h-6 w-6 rounded text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors"
                />
              }
            >
              <Table2 className="h-3.5 w-3.5" />
            </TooltipTrigger>
            <TooltipContent>Open Explorer</TooltipContent>
          </Tooltip>
          <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border ${statusStyle}`}>
            {svc.status}
          </Badge>
        </div>
      </div>

      <div className="border-t border-border/40 pt-3 grid grid-cols-2 gap-x-4 gap-y-1.5">
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Engine</p>
          <p className="text-[11px] text-muted-foreground">
            {engineLabel} {version && <span className="font-mono">{version}</span>}
          </p>
        </div>
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Updated</p>
          <p className="text-[11px] text-muted-foreground">{formatRelativeTime(new Date(svc.updated_at))}</p>
        </div>
      </div>
    </div>
  )
}

export const Route = createFileRoute("/_app/projects/$id/databases")({
  component: DatabasesTab,
})

function DatabasesTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/databases" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()

  const { data: allServices = [], isLoading } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const dbList = allServices.filter((s) => s.type === "database")

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Databases</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && (
            <span className="text-xs text-muted-foreground">{dbList.length}</span>
          )}
        </div>
        <Button
          size="sm"
          className="gap-1.5"
          onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "database" } })}
        >
          <Plus className="h-3.5 w-3.5" />
          New Database
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : dbList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Database className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No databases yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              Provision PostgreSQL, MySQL, Redis, or MongoDB as a K8s workload
            </p>
          </div>
          <Button
            size="sm"
            className="gap-1.5 mt-1"
            onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "database" } })}
          >
            <Plus className="h-3.5 w-3.5" />
            New Database
          </Button>
        </div>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {dbList.map((svc) => (
            <DatabaseCard
              key={svc.id}
              svc={svc}
              projectId={projectId}
              onClick={() => navigate({ to: "/projects/$id/services/$serviceId", params: { id: projectId, serviceId: svc.id } })}
            />
          ))}
        </div>
      )}
    </div>
  )
}
