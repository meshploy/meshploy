import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  Cpu,
  HardDrive,
  MemoryStick,
  Server,
  Loader2,
  ServerCrash,
  Tag,
  Globe,
  Clock,
  CheckCircle2,
  XCircle,
  Trash2,
  SquareTerminal,
  Network,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { nodes as nodesApi, toNode, type ApiNodeMetrics } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { useTabStore } from "@/store/tab-store"
import { useMetricsStore, type RawSample } from "@/store/metrics-store"
import { formatRelativeTime } from "@/lib/utils"
import { useState, useEffect, useRef, useMemo } from "react"

function toRawSample(ts: number, m: ApiNodeMetrics): RawSample {
  return {
    ts,
    cpuTotal: m.cpu_total_seconds,
    cpuIdle: m.cpu_idle_seconds,
    cpuCores: m.cpu_cores,
    memTotal: m.memory_total_bytes,
    memAvail: m.memory_available_bytes,
    diskTotal: m.disk_total_bytes,
    diskAvail: m.disk_avail_bytes,
    netRx: m.net_rx_bytes,
    netTx: m.net_tx_bytes,
  }
}

interface ComputedMetrics {
  cpuPct: number | null
  cpuCoresUsed: number | null
  cpuCores: number
  memPct: number
  memUsedGB: number
  memTotalGB: number
  diskPct: number
  diskUsedGB: number
  diskTotalGB: number
  netRxMbps: number | null
  netTxMbps: number | null
  cpuSeries: number[]
  memSeries: number[]
  diskSeries: number[]
  netSeries: number[]
}

function computeMetrics(history: RawSample[]): ComputedMetrics | null {
  if (history.length === 0) return null
  const latest = history[history.length - 1]
  const prev = history.length > 1 ? history[history.length - 2] : null

  const GB = 1_073_741_824
  const memUsedGB = (latest.memTotal - latest.memAvail) / GB
  const memTotalGB = latest.memTotal / GB
  const memPct = latest.memTotal > 0 ? (1 - latest.memAvail / latest.memTotal) * 100 : 0
  const diskUsedGB = (latest.diskTotal - latest.diskAvail) / GB
  const diskTotalGB = latest.diskTotal / GB
  const diskPct = latest.diskTotal > 0 ? (1 - latest.diskAvail / latest.diskTotal) * 100 : 0

  let cpuPct: number | null = null
  let cpuCoresUsed: number | null = null
  let netRxMbps: number | null = null
  let netTxMbps: number | null = null

  if (prev) {
    const dTotal = latest.cpuTotal - prev.cpuTotal
    const dIdle = latest.cpuIdle - prev.cpuIdle
    if (dTotal > 0) {
      cpuPct = (1 - dIdle / dTotal) * 100
      cpuCoresUsed = (cpuPct / 100) * latest.cpuCores
    }
    const dtS = (latest.ts - prev.ts) / 1000
    if (dtS > 0) {
      netRxMbps = ((latest.netRx - prev.netRx) / dtS / 1_000_000) * 8
      netTxMbps = ((latest.netTx - prev.netTx) / dtS / 1_000_000) * 8
    }
  }

  const cpuSeries: number[] = []
  const memSeries: number[] = []
  const diskSeries: number[] = []
  const netSeries: number[] = []

  for (let i = 1; i < history.length; i++) {
    const c = history[i]
    const p = history[i - 1]
    const dT = c.cpuTotal - p.cpuTotal
    const dI = c.cpuIdle - p.cpuIdle
    cpuSeries.push(dT > 0 ? (1 - dI / dT) * 100 : 0)
    memSeries.push(c.memTotal > 0 ? (1 - c.memAvail / c.memTotal) * 100 : 0)
    diskSeries.push(c.diskTotal > 0 ? (1 - c.diskAvail / c.diskTotal) * 100 : 0)
    const dt = (c.ts - p.ts) / 1000
    netSeries.push(dt > 0 ? ((c.netRx - p.netRx + c.netTx - p.netTx) / dt / 1_000_000) * 8 : 0)
  }

  return {
    cpuPct, cpuCoresUsed, cpuCores: latest.cpuCores,
    memPct, memUsedGB, memTotalGB,
    diskPct, diskUsedGB, diskTotalGB,
    netRxMbps, netTxMbps,
    cpuSeries, memSeries, diskSeries, netSeries,
  }
}

export const Route = createFileRoute("/_app/nodes/$id")({
  component: NodeDetailPage,
})

function NodeDetailPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const openTab = useTabStore((s) => s.openTab)

  const { data: node, isLoading, isError, error } = useQuery({
    queryKey: ["node", orgId, id],
    queryFn: () => nodesApi.get(orgId!, id, token),
    enabled: !!orgId,
    select: toNode,
  })

  const { data: metricsData, dataUpdatedAt } = useQuery({
    queryKey: ["node-metrics", orgId, id],
    queryFn: () => nodesApi.getMetrics(orgId!, id, token),
    enabled: !!orgId,
    refetchInterval: 5000,
    retry: false,
  })

  const history = useMetricsStore(state => state.history[id] ?? [])
  const addSample = useMetricsStore(state => state.addSample)
  const prevUpdatedAt = useRef(0)

  useEffect(() => {
    if (!metricsData || dataUpdatedAt === prevUpdatedAt.current) return
    prevUpdatedAt.current = dataUpdatedAt
    addSample(id, toRawSample(dataUpdatedAt, metricsData))
  }, [metricsData, dataUpdatedAt, id, addSample])

  const computed = useMemo(() => computeMetrics(history), [history])

  const deleteMutation = useMutation({
    mutationFn: () => nodesApi.delete(orgId!, id, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["nodes", orgId] })
      navigate({ to: "/nodes" })
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading node…</span>
      </div>
    )
  }

  if (isError || !node) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Failed to load node</p>
        <p className="text-xs text-muted-foreground/60">{(error as Error)?.message}</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <NodeStatusDot status={node.status} className="h-2.5 w-2.5" />
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-semibold tracking-tight">{node.name}</h1>
              <Badge variant={node.k3sRole === "server" ? "default" : "secondary"} className="text-xs">
                {node.k3sRole}
              </Badge>
            </div>
            <div className="flex items-center gap-3 mt-0.5">
              <code className="text-xs font-mono text-muted-foreground">{node.tailscaleIP}</code>
              {node.status !== "online" && (
                <span className="text-xs text-muted-foreground">
                  {(() => {
                    const seen = node.lastSeenAt ?? node.headscaleLastSeen
                    return seen ? `Last seen ${formatRelativeTime(seen)}` : "Never seen"
                  })()}
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Actions — terminal + delete, worker nodes only */}
        {node.k3sRole !== "server" && !confirmDelete ? (
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              disabled={node.status !== "online"}
              onClick={() => openTab({
                id: node.id,
                type: "terminal",
                label: node.name,
                payload: { nodeId: node.id, nodeLabel: node.name, nodeMeshIP: node.tailscaleIP },
              })}
            >
              <SquareTerminal className="h-3.5 w-3.5" />
              Terminal
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              Remove
            </Button>
          </div>
        ) : node.k3sRole !== "server" && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">Remove this node?</span>
            <Button
              variant="destructive"
              size="sm"
              disabled={deleteMutation.isPending}
              onClick={() => deleteMutation.mutate()}
            >
              {deleteMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : "Confirm"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
          </div>
        )}
      </div>

      {/* Live metrics cards — only rendered when node_exporter is reachable */}
      {computed && (
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
          <MetricCard
            label="CPU"
            icon={<Cpu className="h-4 w-4" />}
            percent={computed.cpuPct}
            sparkData={computed.cpuSeries}
            subtitle={computed.cpuPct !== null && computed.cpuCoresUsed !== null
              ? `${computed.cpuCores} vCPU · ${computed.cpuCoresUsed.toFixed(2)} in use`
              : `${computed.cpuCores} vCPU`}
            color="oklch(0.65 0.18 250)"
          />
          <MetricCard
            label="Memory"
            icon={<MemoryStick className="h-4 w-4" />}
            percent={computed.memPct}
            sparkData={computed.memSeries}
            subtitle={`${computed.memUsedGB.toFixed(1)} / ${computed.memTotalGB.toFixed(1)} GB`}
            color="oklch(0.65 0.18 300)"
          />
          <MetricCard
            label="Disk"
            icon={<HardDrive className="h-4 w-4" />}
            percent={computed.diskPct}
            sparkData={computed.diskSeries}
            subtitle={`${computed.diskUsedGB.toFixed(0)} / ${computed.diskTotalGB.toFixed(0)} GB`}
            color="oklch(0.72 0.18 70)"
          />
          <NetworkCard
            rxMbps={computed.netRxMbps}
            txMbps={computed.netTxMbps}
            sparkData={computed.netSeries}
          />
        </div>
      )}

      {/* Static spec cards — hidden when live metrics replace them */}
      {!computed && (
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
          <SpecCard icon={<Cpu className="h-4 w-4" />} label="CPU" value={node.cpuCores ? `${node.cpuCores} cores` : "—"} />
          <SpecCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={node.memoryGB ? `${node.memoryGB} GB` : "—"} />
          <SpecCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={node.diskGB ? `${node.diskGB} GB` : "—"} />
        </div>
      )}

      {/* Two-column info area */}
      <div className="grid gap-4 lg:grid-cols-2">
        {/* Headscale Peer */}
        <InfoCard title="Headscale Peer">
          {node.headscaleId ? (
            <dl className="space-y-2.5">
              <InfoRow icon={<Server className="h-3.5 w-3.5" />} label="Peer ID" value={node.headscaleId} mono />
              <InfoRow
                icon={
                  node.headscaleOnline
                    ? <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                    : <XCircle className="h-3.5 w-3.5 text-muted-foreground/60" />
                }
                label="Status"
                value={node.headscaleOnline ? "Online" : "Offline"}
                valueClass={node.headscaleOnline ? "text-emerald-400" : "text-muted-foreground"}
              />
              {node.headscaleFQDN && (
                <InfoRow icon={<Globe className="h-3.5 w-3.5" />} label="Mesh Domain" value={node.headscaleFQDN} mono />
              )}
              {node.headscaleLastSeen && !node.headscaleOnline && (
                <InfoRow
                  icon={<Clock className="h-3.5 w-3.5" />}
                  label="Last Seen"
                  value={formatRelativeTime(node.headscaleLastSeen)}
                />
              )}
              {node.headscaleExpiry && (
                <InfoRow
                  icon={<Clock className="h-3.5 w-3.5" />}
                  label="Key Expires"
                  value={node.headscaleExpiry.toLocaleDateString()}
                />
              )}
              {node.headscaleTags.length > 0 && (
                <div className="flex items-start gap-2">
                  <Tag className="h-3.5 w-3.5 text-muted-foreground mt-0.5 shrink-0" />
                  <div className="flex flex-wrap gap-1">
                    {node.headscaleTags.map((t) => (
                      <Badge key={t} variant="outline" className="text-[10px] px-1.5 py-0 h-4.5 font-mono">
                        {t}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">Not found in Headscale.</p>
          )}
        </InfoCard>

        {/* K8s Cluster */}
        <InfoCard title="Cluster">
          {node.k8sMember ? (
            <dl className="space-y-2.5">
              <InfoRow
                icon={<CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />}
                label="Membership"
                value="In Cluster"
                valueClass="text-emerald-400"
              />
              {node.k8sNodeName && (
                <InfoRow icon={<Server className="h-3.5 w-3.5" />} label="Node Name" value={node.k8sNodeName} mono />
              )}
              <InfoRow
                icon={
                  node.k8sReady
                    ? <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                    : <XCircle className="h-3.5 w-3.5 text-amber-400" />
                }
                label="Ready"
                value={node.k8sReady ? "Ready" : "Not Ready"}
                valueClass={node.k8sReady ? "text-emerald-400" : "text-amber-400"}
              />
              {node.k3sVersion && (
                <InfoRow icon={<Server className="h-3.5 w-3.5" />} label="K3s version" value={node.k3sVersion} mono />
              )}
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">Not joined to the k3s cluster.</p>
          )}
        </InfoCard>
      </div>

      {/* Active Projects */}
      {node.activeProjects.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-sm font-medium text-foreground">
            Active Projects{" "}
            <span className="text-muted-foreground font-normal">({node.activeProjects.length})</span>
          </h2>
          <div className="flex flex-wrap gap-2">
            {node.activeProjects.map((ns) => (
              <Badge key={ns} variant="secondary" className="font-mono text-xs">
                {ns}
              </Badge>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

// ─── Live metric components ───────────────────────────────────────────────────

function Sparkline({ data, color = "oklch(0.65 0.18 200)" }: { data: number[]; color?: string }) {
  if (data.length < 2) return <div className="h-8" />
  const W = 100
  const H = 32
  const max = Math.max(...data, 1)
  const pts = data.map((v, i) => [
    (i / (data.length - 1)) * W,
    H - (v / max) * H * 0.85,
  ] as [number, number])
  const line = pts.map((p, i) => (i === 0 ? `M${p[0]},${p[1]}` : `L${p[0]},${p[1]}`)).join(" ")
  const area = `${line} L${W},${H} L0,${H} Z`
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-8" preserveAspectRatio="none">
      <path d={area} fill={color} fillOpacity={0.12} />
      <path d={line} fill="none" stroke={color} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function MetricCard({
  label,
  icon,
  percent,
  sparkData,
  subtitle,
  color,
}: {
  label: string
  icon: React.ReactNode
  percent: number | null
  sparkData: number[]
  subtitle: string
  color: string
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-2">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        {icon}
        <span className="text-xs font-medium">{label}</span>
      </div>
      <p className="text-2xl font-semibold tabular-nums leading-none">
        {percent !== null ? `${Math.round(percent)}%` : "—"}
      </p>
      <Sparkline data={sparkData} color={color} />
      <p className="text-xs text-muted-foreground truncate">{subtitle}</p>
    </div>
  )
}

function NetworkCard({
  rxMbps,
  txMbps,
  sparkData,
}: {
  rxMbps: number | null
  txMbps: number | null
  sparkData: number[]
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-2">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        <Network className="h-4 w-4" />
        <span className="text-xs font-medium">Network</span>
      </div>
      <div className="space-y-0.5">
        <p className="text-sm font-semibold tabular-nums leading-none">
          {rxMbps !== null ? `↓ ${rxMbps.toFixed(0)} Mbps` : "↓ —"}
        </p>
        <p className="text-sm font-semibold tabular-nums leading-none text-muted-foreground">
          {txMbps !== null ? `↑ ${txMbps.toFixed(0)} Mbps` : "↑ —"}
        </p>
      </div>
      <Sparkline data={sparkData} color="oklch(0.65 0.18 200)" />
    </div>
  )
}

function SpecCard({
  icon,
  label,
  value,
  mono,
}: {
  icon: React.ReactNode
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1.5">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        {icon}
        <span className="text-xs font-medium">{label}</span>
      </div>
      <p className={`text-sm font-medium text-foreground ${mono ? "font-mono" : ""}`}>{value}</p>
    </div>
  )
}

function InfoCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-3">
      <h2 className="text-sm font-medium text-foreground">{title}</h2>
      {children}
    </div>
  )
}

function InfoRow({
  icon,
  label,
  value,
  mono,
  valueClass = "text-foreground",
}: {
  icon: React.ReactNode
  label: string
  value: string
  mono?: boolean
  valueClass?: string
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-muted-foreground shrink-0">{icon}</span>
      <span className="text-xs text-muted-foreground w-24 shrink-0">{label}</span>
      <span className={`text-xs ${mono ? "font-mono" : ""} ${valueClass} truncate`}>{value}</span>
    </div>
  )
}
