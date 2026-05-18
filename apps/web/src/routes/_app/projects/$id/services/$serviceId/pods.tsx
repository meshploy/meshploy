import { createFileRoute, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Terminal, RefreshCw } from "lucide-react"
import { services as servicesApi, type ApiPodInfo, type ApiPodMetrics } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { useTabStore } from "@/store/tab-store"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table"
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

function MiniBar({ value, max, colorClass }: { value: number; max: number; colorClass: string }) {
  const pct = max > 0 ? Math.min((value / max) * 100, 100) : 0
  return (
    <div className="w-12 h-1 rounded-full bg-muted/40 overflow-hidden">
      <div className={`h-full rounded-full ${colorClass}`} style={{ width: `${pct}%` }} />
    </div>
  )
}

function formatCPU(millis: number): string {
  if (millis >= 1000) return `${(millis / 1000).toFixed(2)}c`
  return `${millis}m`
}

function formatMem(mib: number): string {
  if (mib >= 1024) return `${(mib / 1024).toFixed(1)}Gi`
  return `${mib}Mi`
}

const thCls = "px-4 py-2.5 font-medium text-muted-foreground/70 uppercase tracking-wider text-[10px]"
const tdCls = "px-4 py-3"

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

  const { data: metricsData = [] } = useQuery<ApiPodMetrics[]>({
    queryKey: ["pod-metrics", orgId, projectId, serviceId],
    queryFn: () => servicesApi.getPodMetrics(orgId!, projectId, serviceId, token),
    enabled: !!orgId && pods.some((p) => p.phase === "Running"),
    refetchInterval: 15_000,
  })

  const metricsMap = new Map(metricsData.map((m) => [m.pod_name, m]))
  const maxCPU = Math.max(...metricsData.map((m) => m.cpu_millis), 1)
  const maxMem = Math.max(...metricsData.map((m) => m.memory_mib), 1)
  const hasMetrics = metricsData.length > 0

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
          {pods.length} pod{pods.length !== 1 ? "s" : ""} · refreshes every 10 s
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
          <Table>
            <TableHeader className="bg-muted/20">
              <TableRow className="border-b border-border/40 hover:bg-transparent">
                <TableHead className={thCls}>Pod</TableHead>
                <TableHead className={thCls}>Status</TableHead>
                <TableHead className={thCls}>Ready</TableHead>
                <TableHead className={thCls}>Restarts</TableHead>
                {hasMetrics && (
                  <>
                    <TableHead className={thCls}>CPU</TableHead>
                    <TableHead className={thCls}>Memory</TableHead>
                  </>
                )}
                <TableHead className={thCls}>Node</TableHead>
                <TableHead className={thCls}>Age</TableHead>
                <TableHead className={thCls} />
              </TableRow>
            </TableHeader>
            <TableBody>
              {pods.map((pod) => {
                const m = metricsMap.get(pod.name)
                return (
                  <TableRow key={pod.name} className="border-b border-border/30">
                    <TableCell className={tdCls}>
                      <code className="font-mono text-[11px] text-foreground">{pod.name}</code>
                    </TableCell>
                    <TableCell className={tdCls}>
                      <Badge className={`text-[10px] px-1.5 py-0 h-4 border ${PHASE_STYLES[pod.phase] ?? PHASE_STYLES.Unknown}`}>
                        {pod.phase}
                      </Badge>
                    </TableCell>
                    <TableCell className={tdCls}>
                      <ReadyDot ready={pod.ready} />
                    </TableCell>
                    <TableCell className={`${tdCls} tabular-nums text-muted-foreground`}>
                      {pod.restarts}
                    </TableCell>
                    {hasMetrics && (
                      <>
                        <TableCell className={tdCls}>
                          {m ? (
                            <div className="flex items-center gap-2">
                              <span className="tabular-nums text-foreground/80 w-10 text-right text-xs">{formatCPU(m.cpu_millis)}</span>
                              <MiniBar value={m.cpu_millis} max={maxCPU} colorClass="bg-blue-400/70" />
                            </div>
                          ) : <span className="text-muted-foreground/40">—</span>}
                        </TableCell>
                        <TableCell className={tdCls}>
                          {m ? (
                            <div className="flex items-center gap-2">
                              <span className="tabular-nums text-foreground/80 w-12 text-right text-xs">{formatMem(m.memory_mib)}</span>
                              <MiniBar value={m.memory_mib} max={maxMem} colorClass="bg-violet-400/70" />
                            </div>
                          ) : <span className="text-muted-foreground/40">—</span>}
                        </TableCell>
                      </>
                    )}
                    <TableCell className={`${tdCls} font-mono text-[11px] text-muted-foreground`}>
                      {pod.node_name || "—"}
                    </TableCell>
                    <TableCell className={`${tdCls} text-muted-foreground text-xs`}>
                      {pod.started_at ? formatRelativeTime(new Date(pod.started_at)) : "—"}
                    </TableCell>
                    <TableCell className={`${tdCls} text-right`}>
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
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </div>

      {!hasMetrics && pods.some((p) => p.phase === "Running") && (
        <p className="text-[11px] text-muted-foreground/40 text-center">
          CPU / memory columns appear when metrics-server is available on the cluster.
        </p>
      )}
    </div>
  )
}
