import { createFileRoute, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Globe, Server, Box, ExternalLink } from "lucide-react"
import { services as servicesApi, routes as routesApi, nodes as nodesApi, toNode } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/overview"
)({
  component: ServiceOverviewTab,
})

function InfoRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-border/30 last:border-0">
      <span className="text-xs font-medium text-muted-foreground/70 uppercase tracking-wider">{label}</span>
      <span className="text-xs text-foreground">{children}</span>
    </div>
  )
}

function ServiceOverviewTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/overview",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const nodes = rawNodes.map(toNode)

  const { data: projectRoutes = [] } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  if (!service) return null

  const node = service.node_id ? nodes.find((n) => n.id === service.node_id) : null
  const attachedRoutes = projectRoutes.filter((r) => r.service_id === service.id)

  return (
    <div className="p-6 space-y-4">
      {/* Stat tiles */}
      <div className="grid gap-3 grid-cols-3">
        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
            <Box className="h-3 w-3" /> Replicas
          </p>
          <p className="text-2xl font-semibold tabular-nums">{service.replicas}</p>
          <p className="text-xs text-muted-foreground">
            {service.status === "running" ? "all healthy" : service.status}
          </p>
        </div>
        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
            <Globe className="h-3 w-3" /> Routes
          </p>
          <p className="text-2xl font-semibold tabular-nums">{attachedRoutes.length}</p>
          <p className="text-xs text-muted-foreground">attached</p>
        </div>
        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
            <Server className="h-3 w-3" /> Port
          </p>
          <p className="text-2xl font-semibold tabular-nums">:{service.port || "—"}</p>
          <p className="text-xs text-muted-foreground">
            {service.node_id ? "node pinned" : "auto-scheduled"}
          </p>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Current deployment */}
        <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Current deployment</p>
          </div>
          <div className="px-4 py-1">
            <InfoRow label="Image">
              {service.image
                ? <code className="text-[11px] font-mono truncate max-w-[220px] block text-right">{service.image}</code>
                : <span className="text-muted-foreground/50">Not deployed yet</span>
              }
            </InfoRow>
            <InfoRow label="Node">
              {node
                ? <span className="flex items-center gap-1.5"><span className={`h-1.5 w-1.5 rounded-full ${node.status === "online" ? "bg-emerald-400" : "bg-muted-foreground/40"}`} />{node.name}</span>
                : <span className="text-muted-foreground/50">auto-scheduled</span>
              }
            </InfoRow>
            <InfoRow label="Mesh IP">
              {node?.tailscaleIP
                ? <code className="text-[11px] font-mono">{node.tailscaleIP}:{service.port}</code>
                : <span className="text-muted-foreground/50">—</span>
              }
            </InfoRow>
            <InfoRow label="Replicas">
              <span>{service.replicas} / {service.replicas}</span>
            </InfoRow>
            <InfoRow label="Last updated">
              <span>{formatRelativeTime(new Date(service.updated_at))}</span>
            </InfoRow>
          </div>
        </div>

        {/* Attached routes */}
        <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Attached routes</p>
          </div>
          {attachedRoutes.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-muted-foreground/50">
              No routes attached
            </div>
          ) : (
            <div className="divide-y divide-border/30">
              {attachedRoutes.map((r) => (
                <div key={r.id} className="flex items-center justify-between px-4 py-3 gap-3">
                  <div className="min-w-0">
                    <code className="text-xs font-mono text-foreground truncate block">{r.hostname}</code>
                    <p className="text-[11px] text-muted-foreground mt-0.5">
                      {r.zone} · {r.target_ip}:{r.target_port}
                    </p>
                  </div>
                  <a
                    href={`https://${r.hostname}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    onClick={(e) => e.stopPropagation()}
                    className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                  </a>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
