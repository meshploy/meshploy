import { createFileRoute } from "@tanstack/react-router"
import { Cpu, HardDrive, MemoryStick, Network } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { mockNodes } from "@/lib/mock-data"

export const Route = createFileRoute("/_app/cluster/")({
  component: ClusterPage,
})

function ClusterPage() {
  const online = mockNodes.filter((n) => n.status === "online")
  const servers = mockNodes.filter((n) => n.k3sRole === "server")
  const agents = mockNodes.filter((n) => n.k3sRole === "agent")

  const totalCPU = mockNodes.reduce((s, n) => s + n.cpuCores, 0)
  const totalMemGB = mockNodes.reduce((s, n) => s + n.memoryGB, 0)
  const totalDiskGB = mockNodes.reduce((s, n) => s + n.diskGB, 0)
  const onlineCPU = online.reduce((s, n) => s + n.cpuCores, 0)
  const onlineMemGB = online.reduce((s, n) => s + n.memoryGB, 0)

  const versionMap = new Map<string, number>()
  mockNodes.forEach((n) => versionMap.set(n.k3sVersion, (versionMap.get(n.k3sVersion) ?? 0) + 1))

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Cluster</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Single K3s cluster spanning all mesh nodes</p>
      </div>

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatCard icon={<Network className="h-4 w-4" />} label="Nodes" value={`${online.length}/${mockNodes.length}`} sub="online" accent={online.length < mockNodes.length ? "warn" : "ok"} />
        <StatCard icon={<Cpu className="h-4 w-4" />} label="CPU cores" value={String(onlineCPU)} sub={`${totalCPU} total`} />
        <StatCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={`${onlineMemGB} GB`} sub={`${totalMemGB} GB total`} />
        <StatCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={`${totalDiskGB} GB`} sub={`${mockNodes.length} nodes`} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div className="rounded-lg border border-border/60 overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Node topology</p>
          </div>
          <div className="divide-y divide-border/30">
            {[...servers, ...agents].map((node) => (
              <div key={node.id} className="flex items-center gap-3 px-4 py-3">
                <NodeStatusDot status={node.status} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-foreground truncate">{node.name}</p>
                  <code className="text-[11px] font-mono text-muted-foreground">{node.tailscaleIP}</code>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <Badge variant={node.k3sRole === "server" ? "default" : "secondary"} className="text-[10px] px-1.5 py-0 h-4.5">
                    {node.k3sRole}
                  </Badge>
                  <span className="text-xs text-muted-foreground tabular-nums">{node.cpuCores}c/{node.memoryGB}G</span>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="space-y-4">
          <div className="rounded-lg border border-border/60 overflow-hidden">
            <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Role breakdown</p>
            </div>
            <div className="p-4 space-y-3">
              <RoleBar label="Control plane" count={servers.length} total={mockNodes.length} color="bg-primary" />
              <RoleBar label="Worker agents" count={agents.length} total={mockNodes.length} color="bg-primary/40" />
            </div>
          </div>

          <div className="rounded-lg border border-border/60 overflow-hidden">
            <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">K3s versions</p>
            </div>
            <div className="divide-y divide-border/30">
              {Array.from(versionMap.entries()).map(([ver, count]) => (
                <div key={ver} className="flex items-center justify-between px-4 py-3">
                  <code className="text-xs font-mono text-foreground">{ver}</code>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{count} {count === 1 ? "node" : "nodes"}</span>
                    {ver !== "v1.28.4+k3s1" && (
                      <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4.5">outdated</Badge>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function StatCard({ icon, label, value, sub, accent }: { icon: React.ReactNode; label: string; value: string; sub: string; accent?: "ok" | "warn" }) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-2">
      <div className="flex items-center gap-2 text-muted-foreground">{icon}<span className="text-xs font-medium">{label}</span></div>
      <p className={`text-2xl font-semibold tabular-nums ${accent === "warn" ? "text-amber-400" : "text-foreground"}`}>{value}</p>
      <p className="text-xs text-muted-foreground">{sub}</p>
    </div>
  )
}

function RoleBar({ label, count, total, color }: { label: string; count: number; total: number; color: string }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="text-foreground font-medium tabular-nums">{count} <span className="text-muted-foreground font-normal">/ {total}</span></span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div className={`h-full rounded-full ${color}`} style={{ width: `${Math.round((count / total) * 100)}%` }} />
      </div>
    </div>
  )
}
