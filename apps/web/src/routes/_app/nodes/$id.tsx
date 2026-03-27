import { createFileRoute, notFound } from "@tanstack/react-router"
import { Cpu, HardDrive, MemoryStick, Server } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { mockNodes, mockServices, mockDeployments } from "@/lib/mock-data"
import { formatRelativeTime } from "@/lib/utils"
import type { ServiceStatus, DeploymentStatus } from "@/types"

const SERVICE_STATUS_STYLES: Record<ServiceStatus, string> = {
  running: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed: "bg-destructive/10 text-destructive border-destructive/20",
  stopped: "bg-muted text-muted-foreground border-border",
}

const DEPLOY_STATUS_STYLES: Record<DeploymentStatus, string> = {
  success: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  running: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed: "bg-destructive/10 text-destructive border-destructive/20",
  pending: "bg-muted text-muted-foreground border-border",
}

export const Route = createFileRoute("/_app/nodes/$id")({
  loader: ({ params }) => {
    const node = mockNodes.find((n) => n.id === params.id)
    if (!node) throw notFound()
    const nodeServices = mockServices.filter((s) => s.nodeId === node.id)
    const nodeDeployments = mockDeployments.filter((d) =>
      nodeServices.some((s) => s.id === d.serviceId)
    )
    return { node, nodeServices, nodeDeployments }
  },
  component: NodeDetailPage,
})

function NodeDetailPage() {
  const { node, nodeServices, nodeDeployments } = Route.useLoaderData()

  return (
    <div className="p-6 space-y-6">
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
            <span className="text-xs text-muted-foreground">{node.os}</span>
            <span className="text-xs text-muted-foreground">
              Last seen {formatRelativeTime(node.lastSeenAt)}
            </span>
          </div>
        </div>
      </div>

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <SpecCard icon={<Cpu className="h-4 w-4" />} label="CPU" value={`${node.cpuCores} cores`} />
        <SpecCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={`${node.memoryGB} GB`} />
        <SpecCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={`${node.diskGB} GB`} />
        <SpecCard icon={<Server className="h-4 w-4" />} label="K3s version" value={node.k3sVersion} mono />
      </div>

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-foreground">
          Services{" "}
          <span className="text-muted-foreground font-normal">({nodeServices.length})</span>
        </h2>
        {nodeServices.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4">No services scheduled on this node.</p>
        ) : (
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {nodeServices.map((svc) => (
              <div key={svc.id} className="flex items-center gap-3 px-4 py-3.5">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-medium text-foreground">{svc.name}</p>
                    <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border ${SERVICE_STATUS_STYLES[svc.status]}`}>
                      {svc.status}
                    </Badge>
                    <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-4.5">{svc.type}</Badge>
                  </div>
                  <code className="text-[11px] font-mono text-muted-foreground/70 mt-0.5 block truncate">{svc.image}</code>
                </div>
                <span className="text-xs text-muted-foreground shrink-0">×{svc.replicas}</span>
              </div>
            ))}
          </div>
        )}
      </section>

      {nodeDeployments.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-sm font-medium text-foreground">Recent deployments</h2>
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {nodeDeployments.map((dep) => {
              const svc = nodeServices.find((s) => s.id === dep.serviceId)
              return (
                <div key={dep.id} className="flex items-center gap-3 px-4 py-3.5">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-medium text-foreground">{svc?.name ?? dep.serviceId}</p>
                      <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border ${DEPLOY_STATUS_STYLES[dep.status]}`}>
                        {dep.status}
                      </Badge>
                    </div>
                    <code className="text-[11px] font-mono text-muted-foreground/70 mt-0.5 block truncate">{dep.image}</code>
                  </div>
                  <span className="text-xs text-muted-foreground shrink-0">{formatRelativeTime(dep.deployedAt)}</span>
                </div>
              )
            })}
          </div>
        </section>
      )}
    </div>
  )
}

function SpecCard({ icon, label, value, mono }: { icon: React.ReactNode; label: string; value: string; mono?: boolean }) {
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
