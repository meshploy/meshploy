import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Rocket, ScrollText, Trash2, X } from "lucide-react"
import { services as servicesApi } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { deployments as deploymentsApi, type ApiDeployment } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/deployments/"
)({
  component: DeploymentsTab,
})

const ACTIVE_STATUSES = new Set(["pending", "building", "deploying"])

const STATUS_STYLES: Record<ApiDeployment["status"], string> = {
  pending:   "bg-muted text-muted-foreground border-border",
  building:  "bg-amber-500/10 text-amber-400 border-amber-500/20",
  deploying: "bg-blue-500/10 text-blue-400 border-blue-500/20",
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  success:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
}

const STATUS_DOT: Record<ApiDeployment["status"], string> = {
  pending:   "bg-muted-foreground/40",
  building:  "bg-amber-400 animate-pulse",
  deploying: "bg-blue-400 animate-pulse",
  running:   "bg-emerald-400",
  success:   "bg-emerald-400",
  failed:    "bg-destructive",
}

function DeploymentsTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/deployments/",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
    staleTime: 30_000,
  })
  const isDatabase = service?.type === "database"
  const queryClient = useQueryClient()

  const queryKey = ["deployments", orgId, projectId, serviceId]

  const { data: deploymentList = [], isLoading } = useQuery({
    queryKey,
    queryFn: () => deploymentsApi.list(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const data = query.state.data as ApiDeployment[] | undefined
      return data?.some((d) => ACTIVE_STATUSES.has(d.status)) ? 3000 : false
    },
  })

  const triggerMutation = useMutation({
    mutationFn: () => deploymentsApi.trigger(orgId!, projectId, serviceId, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Deployments</h2>
          {isLoading && (
            <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
          )}
          {!isLoading && (
            <span className="text-xs text-muted-foreground">
              {deploymentList.length}
            </span>
          )}
        </div>

        <Button
          size="sm"
          className="gap-1.5"
          onClick={() => triggerMutation.mutate()}
          disabled={triggerMutation.isPending}
        >
          {triggerMutation.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Rocket className="h-3.5 w-3.5" />
          )}
          {isDatabase ? "Provision" : "Deploy"}
        </Button>
      </div>

      {triggerMutation.isError && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
          <p className="text-xs text-destructive">
            {(triggerMutation.error as Error)?.message ?? "Failed to trigger deployment"}
          </p>
        </div>
      )}

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : deploymentList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Rocket className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No deployments yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              {isDatabase ? "Provision the database to start it" : "Trigger a deployment to build and deploy this service"}
            </p>
          </div>
          <Button
            size="sm"
            className="gap-1.5 mt-1"
            onClick={() => triggerMutation.mutate()}
            disabled={triggerMutation.isPending}
          >
            <Rocket className="h-3.5 w-3.5" />
            {isDatabase ? "Provision now" : "Deploy now"}
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {deploymentList.map((dep) => (
            <DeploymentRow
              key={dep.id}
              deployment={dep}
              projectId={projectId}
              serviceId={serviceId}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function DeploymentRow({
  deployment,
  projectId,
  serviceId,
}: {
  deployment: ApiDeployment
  projectId: string
  serviceId: string
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const isActive = ACTIVE_STATUSES.has(deployment.status)

  const cancelMutation = useMutation({
    mutationFn: () => deploymentsApi.cancel(orgId, projectId, serviceId, deployment.id, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["deployments", orgId, projectId, serviceId] }),
  })

  const deleteMutation = useMutation({
    mutationFn: () => deploymentsApi.deleteRecord(orgId, projectId, serviceId, deployment.id, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["deployments", orgId, projectId, serviceId] }),
  })

  return (
    <div className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors">
      {/* Status dot */}
      <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${STATUS_DOT[deployment.status]}`} />

      {/* ID + badge */}
      <div className="flex items-center gap-2 min-w-0 flex-1">
        <code className="text-xs font-mono text-foreground/80">
          {deployment.id.slice(0, 8)}
        </code>
        <Badge
          className={`text-[10px] px-1.5 py-0 h-4 border shrink-0 ${STATUS_STYLES[deployment.status]}`}
        >
          {deployment.status}
        </Badge>
        {deployment.image && (
          <code className="text-[11px] font-mono text-muted-foreground/60 truncate hidden sm:block">
            {deployment.image}
          </code>
        )}
      </div>

      {/* Time + actions */}
      <div className="flex items-center gap-2 shrink-0">
        <span className="text-xs text-muted-foreground">
          {formatRelativeTime(new Date(deployment.created_at))}
        </span>
        <Link
          to="/projects/$id/services/$serviceId/deployments/$deploymentId"
          params={{ id: projectId, serviceId, deploymentId: deployment.id }}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <ScrollText className="h-3 w-3" />
          Logs
        </Link>
        {isActive ? (
          <button
            onClick={() => cancelMutation.mutate()}
            disabled={cancelMutation.isPending}
            title="Cancel deployment"
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-destructive transition-colors disabled:opacity-50"
          >
            {cancelMutation.isPending
              ? <Loader2 className="h-3 w-3 animate-spin" />
              : <X className="h-3 w-3" />
            }
          </button>
        ) : (
          <button
            onClick={() => deleteMutation.mutate()}
            disabled={deleteMutation.isPending}
            title="Delete record"
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-destructive transition-colors disabled:opacity-50"
          >
            {deleteMutation.isPending
              ? <Loader2 className="h-3 w-3 animate-spin" />
              : <Trash2 className="h-3 w-3" />
            }
          </button>
        )}
      </div>
    </div>
  )
}
