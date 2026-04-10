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
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { nodes as nodesApi, toNode } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import { useState } from "react"

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

  const { data: node, isLoading, isError, error } = useQuery({
    queryKey: ["node", orgId, id],
    queryFn: () => nodesApi.get(orgId!, id, token),
    enabled: !!orgId,
    select: toNode,
  })

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
              <span className="text-xs text-muted-foreground">
                {(() => {
                  const seen = node.lastSeenAt ?? node.headscaleLastSeen
                  return seen ? `Last seen ${formatRelativeTime(seen)}` : "Never seen"
                })()}
              </span>
            </div>
          </div>
        </div>

        {/* Delete — server/gateway node cannot be removed */}
        {node.k3sRole !== "server" && !confirmDelete ? (
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground hover:text-destructive hover:bg-destructive/10 gap-1.5"
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 className="h-3.5 w-3.5" />
            Remove
          </Button>
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

      {/* Hardware spec cards */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <SpecCard icon={<Cpu className="h-4 w-4" />} label="CPU" value={node.cpuCores ? `${node.cpuCores} cores` : "—"} />
        <SpecCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={node.memoryGB ? `${node.memoryGB} GB` : "—"} />
        <SpecCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={node.diskGB ? `${node.diskGB} GB` : "—"} />
        <SpecCard icon={<Server className="h-4 w-4" />} label="K3s version" value={node.k3sVersion || "—"} mono />
      </div>

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
              {node.headscaleLastSeen && (
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
