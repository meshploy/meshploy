import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Server, PlayCircle, CheckCircle2, XCircle } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { stacks as stacksApi, type ApiService, type ApplyStackResult } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import type { ServiceStatus } from "@/types"

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
    refetchInterval: (query) => {
      const data = query.state.data as ApiService[] | undefined
      return data?.some((s) => s.status === "deploying") ? 5000 : false
    },
  })

  const applyMutation = useMutation({
    mutationFn: () => stacksApi.apply(orgId!, projectId, stackId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: stackQueryKey })
      queryClient.invalidateQueries({ queryKey: servicesQueryKey })
    },
  })

  const applyResult = applyMutation.data as ApplyStackResult | undefined

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
      </div>

      {/* Apply result summary */}
      {applyResult && (
        <div className="rounded-lg border border-border/60 bg-muted/20 p-3 space-y-2">
          <p className="text-xs font-medium text-foreground">Apply complete</p>
          <div className="flex flex-wrap gap-3">
            {applyResult.created.length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-emerald-400">
                <CheckCircle2 className="h-3 w-3" />
                Created: {applyResult.created.join(", ")}
              </div>
            )}
            {applyResult.updated.length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-blue-400">
                <CheckCircle2 className="h-3 w-3" />
                Updated: {applyResult.updated.join(", ")}
              </div>
            )}
            {applyResult.deleted.length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                Unlinked: {applyResult.deleted.join(", ")}
              </div>
            )}
            {applyResult.errors.length > 0 && (
              <div className="flex items-center gap-1.5 text-xs text-destructive">
                <XCircle className="h-3 w-3" />
                Errors: {applyResult.errors.join("; ")}
              </div>
            )}
            {applyResult.created.length === 0 && applyResult.updated.length === 0 && applyResult.errors.length === 0 && (
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
    </div>
  )
}
