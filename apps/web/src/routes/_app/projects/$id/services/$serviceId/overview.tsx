import { createFileRoute, useParams, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Globe, Server, Box, Database, ExternalLink, Copy, Check, Table2, ArrowRight } from "lucide-react"
import {
  services as servicesApi,
  routes as routesApi,
  nodes as nodesApi,
  toNode,
  type ApiNode,
  type ApiDatabaseConfig,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { useTabStore } from "@/store/tab-store"
import { formatRelativeTime } from "@/lib/utils"
import { useState } from "react"
import { Button } from "@/components/ui/button"

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

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center justify-between py-2 border-b border-border/30 last:border-0 gap-3">
      <span className="text-xs font-medium text-muted-foreground/70 uppercase tracking-wider shrink-0">{label}</span>
      <code className="text-[11px] font-mono text-foreground truncate flex-1 text-right">{value}</code>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={() => { navigator.clipboard.writeText(value); setCopied(true); setTimeout(() => setCopied(false), 1500) }}
        title="Copy"
        className="text-muted-foreground/40 hover:text-muted-foreground shrink-0"
      >
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      </Button>
    </div>
  )
}

const ENGINE_LABELS: Record<string, string> = {
  postgres: "PostgreSQL", mysql: "MySQL", redis: "Redis", mongodb: "MongoDB", dragonfly: "Dragonfly", clickhouse: "ClickHouse",
}

function buildConnectionString(dc: ApiDatabaseConfig, host: string, port?: number): string {
  const portStr = port ? `:${port}` : ""
  const pass = encodeURIComponent(dc.db_password)
  switch (dc.engine) {
    case "postgres":  return `postgres://${dc.db_user}:${pass}@${host}${portStr}/${dc.db_name}`
    case "mysql":     return `mysql://${dc.db_user}:${pass}@${host}${portStr}/${dc.db_name}`
    case "redis":     return dc.db_password ? `redis://:${pass}@${host}${portStr}` : `redis://${host}${portStr}`
    case "mongodb":   return `mongodb://${dc.db_user}:${pass}@${host}${portStr}/${dc.db_name}`
    default:          return `${host}${portStr}`
  }
}

function ServiceOverviewTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/overview",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const openTab = useTabStore((s) => s.openTab)
  const navigate = useNavigate()

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const nodes = rawNodes.map(toNode)

  const { data: projectRoutes = [] } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId && service?.type !== "database",
  })

  const { data: dc } = useQuery<ApiDatabaseConfig>({
    queryKey: ["database-config", orgId, projectId, serviceId],
    queryFn: () => servicesApi.getDatabaseConfig(orgId!, projectId, serviceId, token),
    enabled: !!orgId && service?.type === "database",
  })

  if (!service) return null

  const isDatabase = service.type === "database"
  const node = service.node_id
    ? nodes.find((n) => n.id === service.node_id)
    : rawNodes.find((n) => n.status === "online" && n.k8s_member)
      ? toNode(rawNodes.find((n) => n.status === "online" && n.k8s_member)!)
      : null
  const attachedRoutes = projectRoutes.filter((r) =>
    r.targets.some((t) => t.service_id === service.id)
  )

  // Connection strings for database services
  const internalConnStr = dc ? buildConnectionString(dc, `${dc.slug}.svc.cluster.local`) : null
  const meshConnStr = dc?.node_port && node?.tailscaleIP
    ? buildConnectionString(dc, node.tailscaleIP, dc.node_port)
    : null

  return (
    <div className="p-6 space-y-4">
      {/* Stat tiles */}
      <div className="grid gap-3 grid-cols-3">
        {isDatabase ? (
          <>
            <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1">
              <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                <Database className="h-3 w-3" /> Engine
              </p>
              <p className="text-xl font-semibold tabular-nums leading-tight">
                {dc ? ENGINE_LABELS[dc.engine] ?? dc.engine : "—"}
              </p>
              <p className="text-xs text-muted-foreground">{dc?.version ?? ""}</p>
            </div>
            <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1">
              <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                <Box className="h-3 w-3" /> Storage
              </p>
              <p className="text-2xl font-semibold tabular-nums">{dc?.storage_gb ?? "—"}</p>
              <p className="text-xs text-muted-foreground">GiB allocated</p>
            </div>
          </>
        ) : (
          <>
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
          </>
        )}
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
        {/* Current deployment — shared */}
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
              {service.node_id && node
                ? <span className="flex items-center gap-1.5"><span className={`h-1.5 w-1.5 rounded-full ${node.status === "online" ? "bg-emerald-400" : "bg-muted-foreground/40"}`} />{node.name}</span>
                : <span className="text-muted-foreground/50">auto-scheduled</span>
              }
            </InfoRow>
            {!isDatabase && (
              <InfoRow label="Mesh IP">
                {node?.tailscaleIP
                  ? <code className="text-[11px] font-mono">{node.tailscaleIP}:{service.port}</code>
                  : <span className="text-muted-foreground/50">—</span>
                }
              </InfoRow>
            )}
            {!isDatabase && (
              <InfoRow label="Replicas">
                <span>{service.replicas} / {service.replicas}</span>
              </InfoRow>
            )}
            <InfoRow label="Last updated">
              <span>{formatRelativeTime(new Date(service.updated_at))}</span>
            </InfoRow>
          </div>
        </div>

        {/* Right panel: DB credentials+connections or attached routes */}
        {isDatabase ? (
          <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
            <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Connection</p>
              <Button
                size="sm"
                variant="outline"
                className="h-6 gap-1.5 text-[11px] px-2"
                disabled={service.status !== "running"}
                onClick={() => openTab({
                  id: serviceId,
                  type: "explorer",
                  label: service.name,
                  payload: { serviceId, projectId, dbName: service.name },
                })}
              >
                <Table2 className="h-3 w-3" />
                Open Explorer
              </Button>
            </div>
            {!dc ? (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground/50">Provision the database to see connection details</div>
            ) : (
              <div className="px-4 py-1">
                {dc.engine !== "redis" && <CopyRow label="Database" value={dc.db_name} />}
                {dc.engine !== "redis" && <CopyRow label="Username" value={dc.db_user} />}
                <CopyRow label="Password" value={dc.db_password} />
                <CopyRow label="Slug" value={dc.slug} />
                {internalConnStr && <CopyRow label="Internal" value={internalConnStr} />}
                {meshConnStr
                  ? <CopyRow label="Mesh" value={meshConnStr} />
                  : (
                    <div className="flex items-center justify-between py-2 border-b border-border/30 last:border-0">
                      <span className="text-xs font-medium text-muted-foreground/70 uppercase tracking-wider">Mesh</span>
                      <span className="text-xs text-muted-foreground/40 italic">
                        {dc.node_port ? "resolving node…" : "not provisioned"}
                      </span>
                    </div>
                  )
                }
              </div>
            )}
          </div>
        ) : (
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
                        {r.zone}{r.targets[0] ? ` · ${r.targets[0].target_ip}:${r.targets[0].target_port}` : ""}
                      </p>
                    </div>
                    <div className="flex items-center gap-1 shrink-0">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => navigate({ to: "/projects/$id/routes/$routeId", params: { id: projectId, routeId: r.id } })}
                        title="Open route details"
                        className="text-muted-foreground/40 hover:text-muted-foreground"
                      >
                        <ArrowRight className="h-3.5 w-3.5" />
                      </Button>
                      <a
                        href={`https://${r.hostname}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={(e) => e.stopPropagation()}
                        className="inline-flex items-center justify-center text-muted-foreground/40 hover:text-muted-foreground transition-colors h-7 w-7"
                      >
                        <ExternalLink className="h-3.5 w-3.5" />
                      </a>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
