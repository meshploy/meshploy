import { createFileRoute, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ScrollText } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { deployments as deploymentsApi, type ApiDeployment } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/logs"
)({
  component: LogsTab,
})

const STATUS_STYLES: Record<ApiDeployment["status"], string> = {
  pending:   "bg-muted text-muted-foreground border-border",
  building:  "bg-amber-500/10 text-amber-400 border-amber-500/20",
  deploying: "bg-blue-500/10 text-blue-400 border-blue-500/20",
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  success:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
}

const ACTIVE_STATUSES = new Set(["pending", "building", "deploying"])

function LogsTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/logs",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: deploymentList = [], isLoading } = useQuery({
    queryKey: ["deployments", orgId, projectId, serviceId],
    queryFn: () => deploymentsApi.list(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const data = query.state.data as ApiDeployment[] | undefined
      return data?.some((d) => ACTIVE_STATUSES.has(d.status)) ? 3000 : false
    },
  })

  const latest = deploymentList[0]

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (!latest) {
    return (
      <div className="p-6 flex flex-col items-center gap-3 py-24 text-center">
        <ScrollText className="h-7 w-7 text-muted-foreground/40" />
        <div>
          <p className="text-sm text-muted-foreground">No deployments yet</p>
          <p className="text-xs text-muted-foreground/60 mt-0.5">
            Trigger a deployment to see build and deploy logs here
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-3">
      {/* Deployment selector strip */}
      <div className="flex items-center gap-2 overflow-x-auto pb-1">
        {deploymentList.slice(0, 10).map((dep, i) => (
          <button
            key={dep.id}
            onClick={() => {
              // Scroll to / highlight — for now just show latest
            }}
            className="shrink-0 flex items-center gap-1.5 px-2.5 py-1 rounded-md border border-border/60 text-xs bg-muted/20 hover:bg-muted/40 transition-colors"
          >
            <span className="font-mono text-muted-foreground/70">{dep.id.slice(0, 7)}</span>
            <Badge className={`text-[9px] px-1 py-0 h-3.5 border ${STATUS_STYLES[dep.status]}`}>
              {dep.status}
            </Badge>
            <span className="text-muted-foreground/50">
              {formatRelativeTime(new Date(dep.created_at))}
            </span>
            {i === 0 && (
              <span className="text-[9px] text-primary font-medium">latest</span>
            )}
          </button>
        ))}
      </div>

      {/* Log output */}
      <div className="rounded-lg border border-border/60 bg-[oklch(0.12_0_0)] overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-border/40">
          <div className="flex items-center gap-2">
            <code className="text-xs font-mono text-muted-foreground/70">
              deployment/{latest.id.slice(0, 8)}
            </code>
            <Badge className={`text-[10px] px-1.5 py-0 h-4 border ${STATUS_STYLES[latest.status]}`}>
              {latest.status}
            </Badge>
          </div>
          <span className="text-xs text-muted-foreground/50">
            {formatRelativeTime(new Date(latest.created_at))}
          </span>
        </div>
        <pre className="px-4 py-4 text-[12px] font-mono text-muted-foreground leading-relaxed whitespace-pre-wrap break-all overflow-y-auto max-h-[calc(100vh-20rem)]">
          {latest.log || "No log output."}
        </pre>
      </div>

      <p className="text-[11px] text-muted-foreground/50 text-center">
        Showing build + deploy log from latest deployment.
        Live container stdout/stderr streaming coming soon.
      </p>
    </div>
  )
}
