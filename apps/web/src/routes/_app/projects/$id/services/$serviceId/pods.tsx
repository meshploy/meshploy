import { createFileRoute, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Terminal, RefreshCw } from "lucide-react"
import { services as servicesApi, type ApiPodInfo } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { useTabStore } from "@/store/tab-store"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/pods"
)({
  component: PodsTab,
})

const PHASE_STYLES: Record<string, string> = {
  Running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  Pending:   "bg-amber-500/10  text-amber-400  border-amber-500/20",
  Failed:    "bg-destructive/10 text-destructive border-destructive/20",
  Succeeded: "bg-muted text-muted-foreground border-border",
  Unknown:   "bg-muted text-muted-foreground border-border",
}

function ReadyDot({ ready }: { ready: boolean }) {
  return (
    <span
      className={`inline-block h-1.5 w-1.5 rounded-full ${ready ? "bg-emerald-400" : "bg-muted-foreground/40"}`}
      title={ready ? "Ready" : "Not ready"}
    />
  )
}

function PodsTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/pods",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const openTab = useTabStore((s) => s.openTab)

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const {
    data: pods = [],
    isLoading,
    refetch,
    isFetching,
  } = useQuery<ApiPodInfo[]>({
    queryKey: ["pods", orgId, projectId, serviceId],
    queryFn: () => servicesApi.listPods(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
    refetchInterval: 10_000,
  })

  function openTerminal(pod: ApiPodInfo) {
    openTab({
      id: `svc-terminal-${pod.name}`,
      type: "service-terminal",
      label: pod.name.slice(-12),
      payload: {
        orgId: orgId!,
        projectId,
        serviceId,
        podName: pod.name,
        podLabel: service?.name ?? "pod",
      },
    })
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">
          {pods.length} pod{pods.length !== 1 ? "s" : ""} · auto-refreshes every 10 s
        </p>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 gap-1.5 text-xs text-muted-foreground"
          onClick={() => refetch()}
          disabled={isFetching}
        >
          <RefreshCw className={`h-3 w-3 ${isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      <div className="rounded-lg border border-border/60 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-sm text-muted-foreground/50">Loading pods…</div>
        ) : pods.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground/50">
            No pods found. Deploy the service first.
          </div>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border/40 bg-muted/20">
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]">Pod</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]">Status</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]">Ready</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]">Restarts</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]">Node</th>
                <th className="text-left px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]">Age</th>
                <th className="px-4 py-2.5" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border/30">
              {pods.map((pod) => (
                <tr key={pod.name} className="hover:bg-muted/10 transition-colors">
                  <td className="px-4 py-3">
                    <code className="font-mono text-[11px] text-foreground">{pod.name}</code>
                  </td>
                  <td className="px-4 py-3">
                    <Badge className={`text-[10px] px-1.5 py-0 h-4 border ${PHASE_STYLES[pod.phase] ?? PHASE_STYLES.Unknown}`}>
                      {pod.phase}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <ReadyDot ready={pod.ready} />
                  </td>
                  <td className="px-4 py-3 tabular-nums text-muted-foreground">
                    {pod.restarts}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground font-mono text-[11px]">
                    {pod.node_name || "—"}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {pod.started_at ? formatRelativeTime(new Date(pod.started_at)) : "—"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      title="Open terminal"
                      disabled={pod.phase !== "Running" || !pod.ready}
                      onClick={() => openTerminal(pod)}
                      className="text-muted-foreground/40 hover:text-muted-foreground"
                    >
                      <Terminal className="h-3.5 w-3.5" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
