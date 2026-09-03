import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Server, PlayCircle, CheckCircle2, XCircle, Trash2, Globe, HardDrive, FileCog } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  stacks as stacksApi,
  routes as routesApi,
  volumes as volumesApi,
  configFiles as configFilesApi,
  type ApiService,
  type ApplyStackResult,
  type DestroyStackResult,
} from "@/lib/api"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { useState } from "react"
import { Switch } from "@/components/ui/switch"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import type { ServiceStatus } from "@/types"
import { livePoll } from "@/lib/live-poll"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/services")({
  component: StackServicesTab,
})

const STATUS_STYLES: Record<ServiceStatus, string> = {
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
  stopped:   "bg-muted text-muted-foreground border-border",
}

function StackServicesTab() {
  const { id: projectId, stackId } = useParams({ from: "/_app/projects/$id/stacks/$stackId/services" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const stackQueryKey = ["stack", orgId, projectId, stackId]
  const servicesQueryKey = ["stack-services", orgId, projectId, stackId]

  const { data: serviceList = [], isLoading } = useQuery({
    queryKey: servicesQueryKey,
    queryFn: () => stacksApi.listServices(orgId!, projectId, stackId, token),
    enabled: !!orgId,
    refetchInterval: livePoll<ApiService[]>((d) => d.some((s) => s.status === "deploying")),
  })

  const applyMutation = useMutation({
    mutationFn: () => stacksApi.apply(orgId!, projectId, stackId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: stackQueryKey })
      queryClient.invalidateQueries({ queryKey: servicesQueryKey })
    },
  })

  const [confirmDestroy, setConfirmDestroy] = useState(false)
  // Both default off on every open: the extra destruction is opt-in each time,
  // never remembered from a previous run.
  const [deleteVolumes, setDeleteVolumes] = useState(false)
  const [deleteRoutes, setDeleteRoutes] = useState(false)

  const openDestroy = () => {
    setDeleteVolumes(false)
    setDeleteRoutes(false)
    setConfirmDestroy(true)
  }

  const destroyMutation = useMutation({
    mutationFn: () =>
      stacksApi.destroy(orgId!, projectId, stackId, token, {
        delete_volumes: deleteVolumes,
        delete_routes: deleteRoutes,
      }),
    onSuccess: () => {
      setConfirmDestroy(false)
      queryClient.invalidateQueries({ queryKey: stackQueryKey })
      queryClient.invalidateQueries({ queryKey: servicesQueryKey })
      queryClient.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
      queryClient.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
    },
  })

  // Routes and volumes are project-scoped in the API and carry stack_id, so the
  // stack's own are filtered from the project's list rather than needing a
  // dedicated endpoint. A stack owns more than its services, and a page that
  // shows only services under-reports what Destroy would touch.
  const { data: allRoutes = [] } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const { data: allVolumes = [] } = useQuery({
    queryKey: ["volumes", orgId, projectId],
    queryFn: () => volumesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const stackRoutes = allRoutes.filter((r) => r.stack_id === stackId)
  const stackVolumes = allVolumes.filter((v) => v.stack_id === stackId)

  const { data: cfgData } = useQuery({
    queryKey: ["config-files", orgId, projectId],
    queryFn: () => configFilesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const stackConfigs = (cfgData?.files ?? []).filter((f) => f.stack_id === stackId)

  const applyResult = applyMutation.data as ApplyStackResult | undefined
  const destroyResult = destroyMutation.data as DestroyStackResult | undefined

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
        <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          className="gap-1.5"
          onClick={() => applyMutation.mutate()}
          disabled={applyMutation.isPending}
        >
          {applyMutation.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <PlayCircle className="h-3.5 w-3.5" />
          )}
          Apply
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="gap-1.5 text-destructive hover:text-destructive"
          onClick={openDestroy}
          // Not disabled when the service list is empty: a stack whose services
          // are already gone may still own volumes or routes, and that is
          // exactly when someone comes back to clean them up.
          disabled={destroyMutation.isPending}
        >
          {destroyMutation.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
          Destroy
        </Button>
        </div>
      </div>

      {destroyResult && (
        <div className="rounded-lg border border-border/60 bg-muted/20 p-3 space-y-2">
          <p className="text-xs font-medium text-foreground">Destroy complete</p>
          {(destroyResult.destroyed ?? []).length > 0 && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <CheckCircle2 className="h-3 w-3" />
              Destroyed: {(destroyResult.destroyed ?? []).join(", ")}
            </div>
          )}
          {(destroyResult.volumes ?? []).length > 0 && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <CheckCircle2 className="h-3 w-3" />
              Volumes deleted: {(destroyResult.volumes ?? []).join(", ")}
            </div>
          )}
          {(destroyResult.routes ?? []).length > 0 && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <CheckCircle2 className="h-3 w-3" />
              Routes deleted: {(destroyResult.routes ?? []).join(", ")}
            </div>
          )}
          {(destroyResult.errors ?? []).map((e) => (
            <div key={e} className="flex items-center gap-1.5 text-xs text-destructive">
              <XCircle className="h-3 w-3 shrink-0" />
              {e}
            </div>
          ))}
        </div>
      )}

      <Dialog open={confirmDestroy} onOpenChange={setConfirmDestroy}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Destroy this stack?</DialogTitle>
            <DialogDescription>
              {serviceList.length > 0
                ? `The ${serviceList.length} service${serviceList.length === 1 ? "" : "s"} this stack created ${serviceList.length === 1 ? "is" : "are"} removed from the cluster and from meshploy. The stack and its spec stay, so Apply recreates them.`
                : "This stack has no services left to remove. The stack and its spec stay, so Apply recreates them."}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 py-1">
            <div className="flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/20 p-3">
              <div className="space-y-0.5">
                <p className="text-xs font-medium text-foreground">Also delete volumes</p>
                <p className="text-[11px] text-muted-foreground/70">
                  Deletes the volumes this stack created and everything stored in them. This cannot
                  be undone — applying again gives you empty volumes.
                </p>
              </div>
              <Switch checked={deleteVolumes} onCheckedChange={(v) => setDeleteVolumes(Boolean(v))} />
            </div>

            <div className="flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/20 p-3">
              <div className="space-y-0.5">
                <p className="text-xs font-medium text-foreground">Also delete routes</p>
                <p className="text-[11px] text-muted-foreground/70">
                  Frees the hostnames this stack published. Anyone using them stops being able to
                  reach it, and applying again may not produce the same hostname.
                </p>
              </div>
              <Switch checked={deleteRoutes} onCheckedChange={(v) => setDeleteRoutes(Boolean(v))} />
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setConfirmDestroy(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              className="gap-1.5"
              onClick={() => destroyMutation.mutate()}
              disabled={destroyMutation.isPending}
            >
              {destroyMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Destroy
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Apply result summary */}
      {applyResult && (
        <div className="rounded-lg border border-border/60 bg-muted/20 p-3 space-y-2">
          <p className="text-xs font-medium text-foreground">Apply complete</p>
          <div className="flex flex-wrap gap-3">
            {(applyResult.created ?? []).length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-emerald-400">
                <CheckCircle2 className="h-3 w-3" />
                Created: {(applyResult.created ?? []).join(", ")}
              </div>
            )}
            {(applyResult.updated ?? []).length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-blue-400">
                <CheckCircle2 className="h-3 w-3" />
                Updated: {(applyResult.updated ?? []).join(", ")}
              </div>
            )}
            {(applyResult.deleted ?? []).length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                Unlinked: {(applyResult.deleted ?? []).join(", ")}
              </div>
            )}
            {(applyResult.errors ?? []).length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-destructive">
                <XCircle className="h-3 w-3" />
                Errors: {(applyResult.errors ?? []).join("; ")}
              </div>
            )}
            {(applyResult.created ?? []).length === 0 && (applyResult.updated ?? []).length === 0 && (applyResult.errors ?? []).length === 0 && (
              <p className="text-xs text-muted-foreground">No changes</p>
            )}
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : serviceList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Server className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No services yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              Define services in the Editor tab and click Apply
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="gap-1.5 mt-1"
            onClick={() => applyMutation.mutate()}
            disabled={applyMutation.isPending}
          >
            <PlayCircle className="h-3.5 w-3.5" />
            Apply spec
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {serviceList.map((svc) => (
            <div
              key={svc.id}
              className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors cursor-pointer"
              onClick={() => navigate({ to: "/projects/$id/services/$serviceId", params: { id: projectId, serviceId: svc.id } })}
            >
              <Server className="h-3.5 w-3.5 text-muted-foreground/60 shrink-0" />
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <span className="text-sm font-medium text-foreground">{svc.name}</span>
                <Badge className={`text-[10px] px-1.5 py-0 h-4 border shrink-0 ${STATUS_STYLES[svc.status]}`}>
                  {svc.status}
                </Badge>
                {svc.image && (
                  <code className="text-[11px] font-mono text-muted-foreground/60 truncate hidden sm:block">
                    {svc.image}
                  </code>
                )}
              </div>
              <span className="text-xs text-muted-foreground shrink-0">
                {formatRelativeTime(new Date(svc.updated_at))}
              </span>
            </div>
          ))}
        </div>
      )}

      {stackRoutes.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-medium">Routes</h2>
            <span className="text-xs text-muted-foreground">{stackRoutes.length}</span>
          </div>
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {stackRoutes.map((r) => (
              <div
                key={r.id}
                className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors cursor-pointer"
                onClick={() => navigate({ to: "/projects/$id/routes/$routeId", params: { id: projectId, routeId: r.id } })}
              >
                <Globe className="h-3.5 w-3.5 text-muted-foreground/60 shrink-0" />
                <span className="text-sm font-mono text-foreground flex-1 min-w-0 truncate">{r.hostname}</span>
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0">{r.zone}</Badge>
              </div>
            ))}
          </div>
        </div>
      )}

      {stackConfigs.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-medium">Config files</h2>
            <span className="text-xs text-muted-foreground">{stackConfigs.length}</span>
          </div>
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {stackConfigs.map((f) => (
              <div
                key={f.id}
                className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors cursor-pointer"
                onClick={() => navigate({ to: "/projects/$id/config-files/$fileId", params: { id: projectId, fileId: f.id } })}
              >
                <FileCog className="h-3.5 w-3.5 text-muted-foreground/60 shrink-0" />
                <span className="text-sm font-medium text-foreground shrink-0">{f.name}</span>
                <code className="text-[11px] font-mono text-muted-foreground/60 flex-1 min-w-0 truncate">
                  {f.path}
                </code>
                {f.services.length > 0 && (
                  <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0">
                    {f.services.length === 1 ? f.services[0] : `${f.services.length} services`}
                  </Badge>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {stackVolumes.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-medium">Volumes</h2>
            <span className="text-xs text-muted-foreground">{stackVolumes.length}</span>
          </div>
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {stackVolumes.map((v) => (
              <div
                key={v.id}
                className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors cursor-pointer"
                onClick={() => navigate({ to: "/projects/$id/volumes/$volumeId", params: { id: projectId, volumeId: v.id } })}
              >
                <HardDrive className="h-3.5 w-3.5 text-muted-foreground/60 shrink-0" />
                <span className="text-sm font-medium text-foreground flex-1 min-w-0 truncate">{v.name}</span>
                <span className="text-[11px] text-muted-foreground/60 shrink-0">{v.storage_gb} GB</span>
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0">{v.status}</Badge>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
