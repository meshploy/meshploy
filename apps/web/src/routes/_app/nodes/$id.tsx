import { ResourcePanel, ResourceIntro, StatusPill } from "@/components/layout/resource-workbench"
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  Activity,
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
import { HostContainers } from "@/components/nodes/host-containers"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { nodes as nodesApi, toNode, type ApiNodeMetrics } from "@/lib/api"
import type { MeshRole } from "@/types"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin } from "@/store/org-store"
import { useTabStore } from "@/store/tab-store"
import { useMetricsStore, type RawSample } from "@/store/metrics-store"
import { CLUSTER_ROLES, RolePicker } from "@/components/nodes/role-picker"
import { RoleChangeDialog } from "@/components/nodes/role-change-dialog"
import { formatRelativeTime } from "@/lib/utils"
import { useState, useEffect, useRef, useMemo, useId } from "react"
import { AreaChart, Area, YAxis, ResponsiveContainer, Tooltip } from "recharts"

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

const EMPTY_HISTORY: RawSample[] = []

export const Route = createFileRoute("/_app/nodes/$id")({
  component: NodeDetailPage,
})

function NodeDetailPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const isAdmin = useIsAdmin()
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const openTab = useTabStore((s) => s.openTab)

  const { data: node, isPending, isError, error } = useQuery({
    queryKey: ["node", orgId, id],
    queryFn: () => nodesApi.get(orgId!, id, token),
    enabled: !!orgId,
    select: toNode,
    throwOnError: false,
    // Watch a removal that is waiting for Headscale finish.
    refetchInterval: (q) => (q.state.data?.removal_requested_at ? 15000 : false),
  })

  const { data: metricsData, dataUpdatedAt } = useQuery({
    queryKey: ["node-metrics", orgId, id],
    queryFn: () => nodesApi.getMetrics(orgId!, id, token),
    enabled: !!orgId,
    refetchInterval: 5000,
    retry: false,
    throwOnError: false,
  })

  const history = useMetricsStore(state => state.history[id] ?? EMPTY_HISTORY)
  const addSample = useMetricsStore(state => state.addSample)
  const prevUpdatedAt = useRef(history.length > 0 ? history[history.length - 1].ts : 0)

  useEffect(() => {
    if (!metricsData || dataUpdatedAt === prevUpdatedAt.current) return
    if (typeof metricsData.cpu_total_seconds !== "number") return
    prevUpdatedAt.current = dataUpdatedAt
    addSample(id, toRawSample(dataUpdatedAt, metricsData))
  }, [metricsData, dataUpdatedAt, id, addSample])

  const computed = useMemo(() => computeMetrics(history), [history])

  const deleteMutation = useMutation({
    mutationFn: () => nodesApi.delete(orgId!, id, token),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ["nodes", orgId] })
      if (res?.removed ?? true) {
        navigate({ to: "/nodes" })
        return
      }
      setConfirmDelete(false)
      queryClient.invalidateQueries({ queryKey: ["node", orgId, id] })
    },
  })
  const cancelRemoval = useMutation({
    mutationFn: () => nodesApi.cancelRemoval(orgId!, id, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["node", orgId, id] })
      queryClient.invalidateQueries({ queryKey: ["nodes", orgId] })
    },
  })

  if (isPending) {
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
    <div className="console-page p-6 space-y-6">
      <Link to="/nodes" className="inline-flex text-xs text-muted-foreground hover:text-primary">← Back to nodes</Link>
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <span className="accent-icon-tile"><Server className="size-5"/></span>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-semibold tracking-tight">{node.name}</h1><StatusPill status={node.status}/>
              <Badge variant={node.k3sRole === "server" ? "default" : "secondary"} className="text-xs">
                {node.meshRole === "mesh" ? "mesh only" : node.k3sRole}
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

        {/* Actions */}
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5"
            onClick={() => openTab({
              id: `metrics-${node.id}`,
              type: "metrics",
              label: `${node.name} · Metrics`,
              payload: { nodeId: node.id, nodeLabel: node.name },
            })}
          >
            <Activity className="h-3.5 w-3.5" />
            Metrics
          </Button>

          {isAdmin && node.k3sRole !== "server" && (
            <>
              {node.meshRole !== "mesh" && (
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5"
                disabled={node.status !== "online" || !node.k8sMember}
                title={!node.k8sMember ? "The terminal runs through K3s, and this node is not in the cluster" : undefined}
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
              )}
              {!node.removalRequestedAt && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5"
                  onClick={() => { deleteMutation.reset(); setConfirmDelete(true) }}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Remove
                </Button>
              )}
            </>
          )}

        </div>
      </div>

      <Dialog open={confirmDelete} onOpenChange={(open) => { if (!deleteMutation.isPending) setConfirmDelete(open) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove {node.name}?</DialogTitle>
            <DialogDescription>It leaves the mesh, the cluster and Meshploy.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 text-sm">
            <div>
              <p className="font-medium">Removed</p>
              <ul className="mt-1.5 list-disc space-y-1 pl-5 text-muted-foreground">
                <li>Its Headscale peer, so the machine loses its mesh access.</li>
                <li>Its Kubernetes node, so services running there move to other nodes.</li>
                <li>Its Meshploy record. Services and volumes pinned to it are unpinned.</li>
              </ul>
            </div>
            <div>
              <p className="font-medium">Stays on the machine</p>
              <ul className="mt-1.5 list-disc space-y-1 pl-5 text-muted-foreground">
                <li>Tailscale, the K3s agent and the Meshploy CLI. Run <code className="font-mono text-foreground">sudo meshploy node uninstall</code> there to remove them.</li>
                <li>Data on volumes stored on it.</li>
              </ul>
            </div>
            <p className="text-xs text-muted-foreground">If Headscale can&apos;t be reached, the node waits and is removed once it can.</p>
          </div>
          {deleteMutation.isError && (
            <p role="alert" className="text-sm text-destructive">Could not remove the node: {(deleteMutation.error as Error).message}</p>
          )}
          <DialogFooter>
            <Button variant="outline" disabled={deleteMutation.isPending} onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
            <Button variant="destructive" disabled={deleteMutation.isPending} onClick={() => deleteMutation.mutate()}>
              {deleteMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}Remove node
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {deleteMutation.isError && !confirmDelete && (
        <p role="alert" className="text-sm text-destructive">Could not remove the node: {(deleteMutation.error as Error).message}</p>
      )}
      {node.removalRequestedAt && (
        <div role="status" className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4 space-y-3">
          <div>
            {/* The reason decides the heading: a removal can also be held up by
                something still pointing at the node, and calling that "waiting
                for Headscale" sent people to look at the wrong thing. */}
            <p className="text-sm font-medium">
              {node.removalError && !node.removalError.includes("Headscale")
                ? "Removing: not finished yet"
                : "Removing: waiting for Headscale"}
            </p>
            <p className="mt-1 text-xs text-muted-foreground leading-relaxed">
              {node.removalError || "Headscale has not confirmed the node's peer is gone."} Meshploy retries every minute. Until it finishes the node stays, so the machine cannot rejoin unnoticed.
            </p>
          </div>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" disabled={deleteMutation.isPending} onClick={() => deleteMutation.mutate()}>
              {deleteMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}Retry now
            </Button>
            <Button size="sm" variant="ghost" disabled={cancelRemoval.isPending} onClick={() => cancelRemoval.mutate()}>
              Cancel removal
            </Button>
          </div>
          {cancelRemoval.isError && <p role="alert" className="text-xs text-destructive">{(cancelRemoval.error as Error).message}</p>}
        </div>
      )}

      <ResourceIntro title="Capacity and utilization" description={computed ? "Live measurements from this node. Open Metrics for detailed monitoring." : "Reported hardware capacity. Live utilization is not available yet."}/>
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
          <SpecCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={node.memoryGB ? `${node.memoryGB.toFixed(1)} GB` : "—"} />
          <SpecCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={node.diskGB ? `${node.diskGB.toFixed(0)} GB` : "—"} />
        </div>
      )}

      <ResourceIntro title="Connectivity and role" description="Mesh membership, cluster readiness and workload scheduling for this node." />
      {/* Two-column info area */}
      <div className="resource-overview-columns"><div className="space-y-6">
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
                      <Badge key={t} variant="outline" className="text-[11px] px-1.5 py-0 h-4.5 font-mono">
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
            <p className="text-sm text-muted-foreground">{node.meshRole === "mesh" ? "Mesh only: not part of the cluster. Routes can reach its ports: in a project, Routes → New route → Node + port." : "Not joined to the k3s cluster."}</p>
          )}
        </InfoCard>
      </div><aside className="space-y-6">

      {/* Role controls */}
      {node.k3sRole === "server"
        ? <ServerBuildToggle node={node} orgId={orgId!} token={token} />
        : node.meshRole === "mesh"
          ? <MeshOnlyRole />
          : isAdmin
            ? <NodeRolePicker node={node} orgId={orgId!} token={token} />
            : <NodeRoleSummary node={node} />
      }

      </aside></div>
      {/* What else this machine runs - a line, with the list itself on the
          Discovery page. Gateway-only: the host agent reports there, and it
          hides itself anywhere else. */}
      <HostContainers orgId={orgId!} nodeId={node.id} token={token} />

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

// ─── Server build toggle ──────────────────────────────────────────────────────

function ServerBuildToggle({ node, orgId, token }: { node: ReturnType<typeof toNode>; orgId: string; token: string }) {
  const queryClient = useQueryClient()
  const isBuilder = node.meshRole === "workload_builder" || node.meshRole === "builder"
  // Turning this off is the same change a worker's picker makes, and can leave
  // the cluster with nowhere to build - which on a single-server install is the
  // common case. It gets the same confirmation.
  const [proposed, setProposed] = useState<MeshRole | null>(null)

  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId, token),
    enabled: !!orgId,
  })

  const { mutate: toggle, isPending } = useMutation({
    mutationFn: (enable: boolean) =>
      nodesApi.update(orgId, node.id, { mesh_role: enable ? "workload_builder" : "workload" }, token),
    onSuccess: (updated) => {
      queryClient.setQueryData(["node", orgId, node.id], updated)
      queryClient.invalidateQueries({ queryKey: ["nodes", orgId] })
    },
  })

  return (
    <ResourcePanel
      title="Build node"
      description="Allow build jobs to schedule on this gateway node. On by default, so a single server can build; turn it off to keep builds on your other nodes."
    >
      <div className="flex items-center justify-between">
          <div className="space-y-0.5">
            <div className="flex items-center gap-2">
              <p className="text-xs font-medium text-foreground">Act as build node</p>
              {isBuilder && (
                <span className="text-[11px] font-medium px-1.5 py-0.5 rounded-full bg-emerald-500/15 text-emerald-400 border border-emerald-500/20">
                  Active
                </span>
              )}
            </div>
            <p className="text-[11px] text-muted-foreground">
              Adds <code className="font-mono">meshploy.com/role=builder</code> label — build jobs can land here.
              No <code className="font-mono">NoSchedule</code> taint on the gateway.
            </p>
          </div>
          <div className="ml-4 shrink-0">
            {isPending
              ? <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
              : <Switch
                  checked={isBuilder}
                  onCheckedChange={(val) => (val ? toggle(true) : setProposed("workload"))}
                />
            }
          </div>
      </div>
      <RoleChangeDialog
        node={node}
        to={proposed}
        nodes={rawNodes.map(toNode)}
        onCancel={() => setProposed(null)}
        onConfirm={() => {
          toggle(false)
          setProposed(null)
        }}
      />
    </ResourcePanel>
  )
}

// A mesh-only node's role is set when it is installed: moving it into the
// cluster means installing K3s on the machine.
// What a member sees in place of the picker: the same fact, without the
// controls. The API refuses a role change from a member, and a control that
// always fails is worse than none.
function NodeRoleSummary({ node }: { node: ReturnType<typeof toNode> }) {
  const label = node.meshRole === "builder"
    ? "Builds only"
    : node.meshRole === "workload"
      ? "Workloads only"
      : "Workloads and builds"
  return (
    <ResourcePanel title="Node role">
      <p className="text-sm font-medium text-foreground">{label}</p>
      <p className="mt-1 text-xs text-muted-foreground">An administrator can change this.</p>
    </ResourcePanel>
  )
}

function MeshOnlyRole() {
  return (
    <ResourcePanel title="Node role">
      <p className="text-sm font-medium text-foreground">Mesh only</p>
      <p className="mt-1 text-xs text-muted-foreground">
        On the mesh, not in the cluster: nothing is scheduled here, and routes can reach its ports. Moving it into the cluster means installing K3s on the machine.
      </p>
    </ResourcePanel>
  )
}

// ─── Node role picker ─────────────────────────────────────────────────────────

function NodeRolePicker({ node, orgId, token }: { node: ReturnType<typeof toNode>; orgId: string; token: string }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<MeshRole | null>(null)
  // The role a click asked for, held until the consequences have been read.
  const [proposed, setProposed] = useState<MeshRole | null>(null)

  // What else the cluster has. The dialog's sharpest line - "this is the only
  // node that can build" - is about the others, not this one.
  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId, token),
    enabled: !!orgId,
  })

  const { mutate: updateRole } = useMutation({
    mutationFn: (role: MeshRole) => nodesApi.update(orgId, node.id, { mesh_role: role }, token),
    onMutate: (role) => setPending(role),
    onSettled: () => setPending(null),
    onSuccess: (updated) => {
      queryClient.setQueryData(["node", orgId, node.id], updated)
      queryClient.invalidateQueries({ queryKey: ["nodes", orgId] })
    },
  })

  return (
    // The same block the panels beside it use, so a setting does not read as
    // loose text floating next to cards.
    <ResourcePanel title="Node role">
      {/* The same picker the cluster page mints a token with, so the choice is
          described the same way whether it is made before the machine joins or
          changed afterwards. */}
      <RolePicker
        value={node.meshRole}
        onChange={(role) => !pending && role !== node.meshRole && setProposed(role)}
        options={CLUSTER_ROLES}
        busy={pending}
      />
      <RoleChangeDialog
        node={node}
        to={proposed}
        nodes={rawNodes.map(toNode)}
        onCancel={() => setProposed(null)}
        onConfirm={() => {
          if (proposed) updateRole(proposed)
          setProposed(null)
        }}
      />
      {!node.k8sMember && (
        <p className="mt-3 text-xs text-amber-400">Not in the cluster: the role takes effect once this node joins.</p>
      )}
    </ResourcePanel>
  )
}

// ─── Live metric components ───────────────────────────────────────────────────

function SparkAreaChart({
  data,
  color = "oklch(0.65 0.18 200)",
  unit = "%",
  domain,
}: {
  data: number[]
  color?: string
  unit?: string
  domain?: [number, number]
}) {
  const uid = useId().replace(/:/g, "")
  if (data.length < 2) return <div className="h-10" />
  const chartData = data.map((value) => ({ value }))
  return (
    <div className="h-10 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={chartData} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={`sg-${uid}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor={color} stopOpacity={0.25} />
              <stop offset="95%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          {domain && <YAxis domain={domain} hide />}
          <Tooltip
            content={({ active, payload }) => {
              if (!active || !payload?.length) return null
              const v = payload[0]?.value
              if (v == null) return null
              return (
                <div className="rounded border border-border/50 bg-background px-2 py-1 text-xs shadow-lg">
                  <span className="font-mono font-medium">{Number(v).toFixed(1)}{unit}</span>
                </div>
              )
            }}
          />
          <Area
            type="monotone"
            dataKey="value"
            stroke={color}
            strokeWidth={1.5}
            fill={`url(#sg-${uid})`}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
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
    <div className="quiet-surface rounded-xl border border-border bg-card p-5 space-y-3">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        {icon}
        <span className="text-xs font-medium">{label}</span>
      </div>
      <p className="text-2xl font-semibold tabular-nums leading-none">
        {percent !== null ? `${Math.round(percent)}%` : "—"}
      </p>
      <SparkAreaChart data={sparkData} color={color} domain={[0, 100]} />
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
    <div className="quiet-surface rounded-xl border border-border bg-card p-5 space-y-3">
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
      <SparkAreaChart data={sparkData} color="oklch(0.65 0.18 200)" unit=" Mbps" />
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
    <div className="quiet-surface rounded-xl border border-border bg-card p-5 space-y-3">
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
    <ResourcePanel title={title}>{children}</ResourcePanel>
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
