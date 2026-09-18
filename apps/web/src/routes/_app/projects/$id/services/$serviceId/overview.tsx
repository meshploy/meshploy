import { MetricTile, ResourcePanel, ResourceFact, StatusPill } from "@/components/layout/resource-workbench"
import { livePoll } from "@/lib/live-poll"
import { createFileRoute, useParams, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Server, Box, ExternalLink, Copy, Check, Table2, ArrowRight, Activity, MemoryStick, ArrowUpRight } from "lucide-react"
import {
  services as servicesApi,
  deployments, variableGroups, stacks,
  routes as routesApi,
  tcpRoutes,
  nodes as nodesApi,
  projects as projectsApi,
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

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center justify-between py-2 border-b border-border/30 last:border-0 gap-3">
      <span className="text-xs text-muted-foreground shrink-0">{label}</span>
      <code className="text-[11px] font-mono text-foreground truncate flex-1 min-w-0 text-right">{value}</code>
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

  const { data: project } = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const { data: dc } = useQuery<ApiDatabaseConfig>({
    queryKey: ["database-config", orgId, projectId, serviceId],
    queryFn: () => servicesApi.getDatabaseConfig(orgId!, projectId, serviceId, token),
    enabled: !!orgId && service?.type === "database",
  })

  // A TCP route publishes this database on the gateway, so its address is one
  // more thing to connect with -- and the one to be careful with.
  const { data: tcpList = [] } = useQuery({
    queryKey: ["tcp-routes", orgId, projectId],
    queryFn: () => tcpRoutes.list(orgId!, projectId, token),
    enabled: !!orgId && service?.type === "database",
  })

  const { data: pods, isError: podsError } = useQuery({ queryKey: ["pods", orgId, projectId, serviceId], queryFn: () => servicesApi.listPods(orgId!, projectId, serviceId, token), enabled: !!orgId, refetchInterval: 15000 })
  const { data: metrics, isError: metricsError } = useQuery({ queryKey: ["pod-metrics", orgId, projectId, serviceId], queryFn: () => servicesApi.getPodMetrics(orgId!, projectId, serviceId, token), enabled: !!orgId, refetchInterval: 15000, retry: false })
  const { data: history, isError: historyError } = useQuery({ queryKey: ["deployments", orgId, projectId, serviceId], queryFn: () => deployments.list(orgId!, projectId, serviceId, token), enabled: !!orgId, refetchInterval: livePoll(list => list.some(d => ["pending", "building", "deploying"].includes(d.status))) })
  const { data: groups = [] } = useQuery({ queryKey: ["service-variable-groups", orgId, projectId, serviceId], queryFn: () => variableGroups.listForService(orgId!, projectId, serviceId, token), enabled: !!orgId })
  const { data: stack } = useQuery({ queryKey: ["stack", orgId, projectId, service?.stack_id], queryFn: () => stacks.get(orgId!, projectId, service!.stack_id!, token), enabled: !!orgId && !!service?.stack_id })
  if (!service) return null

  const isDatabase = service.type === "database"
  const observedNodeNames = [...new Set((pods ?? []).map(p => p.node_name).filter(Boolean))]
  const node = service.node_id ? nodes.find(n => n.id === service.node_id) : observedNodeNames.length === 1 ? nodes.find(n => n.k8sNodeName === observedNodeNames[0]) : undefined
  const latest = history?.slice().sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0]
  const cpu = metrics?.length ? (metrics.reduce((sum, m) => sum + m.cpu_millis, 0) / 1000).toFixed(2) : null
  const memory = metrics?.length ? Math.round(metrics.reduce((sum, m) => sum + m.memory_mib, 0)) : null
  const readyPods = pods?.filter(p => p.ready).length
  const attachedRoutes = projectRoutes.filter((r) =>
    r.targets.some((t) => t.service_id === service.id)
  )

  // Connection strings for database services
  const internalConnStr = dc && project
    ? buildConnectionString(dc, `${dc.slug}.${project.slug}.svc.cluster.local`)
    : null
  // A published port answers on every node, so a database whose placement is
  // not known still has a mesh address: the gateway's.
  const meshNode = node ?? nodes.find(n => n.k3sRole === "server")
  // The gateway's public address comes from the raw row: the mapped node type
  // carries what the console shows about a node, not how to reach it.
  const gatewayPublicIP = rawNodes.find(n => n.k3s_role === "server")?.public_ip
  const tcpRoute = tcpList.find(r => r.service_id === serviceId)
  const publicConnStr = dc && tcpRoute && tcpRoute.status === "open" && gatewayPublicIP
    ? buildConnectionString(dc, gatewayPublicIP, tcpRoute.gateway_port)
    : null
  const meshConnStr = dc?.node_port && meshNode?.tailscaleIP
    ? buildConnectionString(dc, meshNode.tailscaleIP, dc.node_port)
    : null

  return (
    <div className="console-page resource-overview space-y-6">
      <div className="resource-metrics">
        <MetricTile icon={Activity} label="CPU" value={cpu ?? "-"} unit={cpu ? "cores" : undefined} detail={metricsError || !metrics?.length ? "Metrics unavailable" : `Limit · ${service.cpu_limit || "Not set"}`} />
        <MetricTile icon={MemoryStick} label="Memory" value={memory ?? "-"} unit={memory !== null ? "MiB" : undefined} detail={metricsError || !metrics?.length ? "Metrics unavailable" : `Limit · ${service.memory_limit || "Not set"}`} />
        <MetricTile icon={Box} label="Replicas" value={podsError || !pods ? "-" : readyPods} unit={`/ ${service.replicas} ready`} detail={podsError ? "Pod readiness unavailable" : observedNodeNames.length ? `On ${observedNodeNames.join(", ")}` : "No placement reported"} />
      </div>
      <div className="resource-overview-columns">
        <div className="space-y-6 min-w-0">
          <ResourcePanel title="Latest deployment" action={latest && <StatusPill status={latest.status} />}>
            {historyError ? <p className="resource-empty">Deployment history is unavailable.</p> : !latest ? <div className="resource-empty"><Box className="size-7 mx-auto mb-4" /><p>No deployments yet</p><p className="mt-2 text-xs">Deploy this service to see its build and rollout here.</p></div> : <>
              <div className="flex items-center justify-between gap-4"><div className="flex items-center gap-3"><Box className="size-5 text-muted-foreground" /><h3 className="text-sm font-semibold">{latest.status === "success" ? "Deployment completed" : latest.status === "failed" ? "Deployment failed" : `Deployment ${latest.status}`}</h3></div><Link to="/projects/$id/services/$serviceId/deployments/$deploymentId" params={{ id: projectId, serviceId, deploymentId: latest.id }} className="resource-id-link">{latest.id.slice(0, 8)}<ArrowUpRight className="size-3" /></Link></div>
              <p className="mt-5 text-xs font-mono text-muted-foreground break-all">{latest.image || service.image || "Image not reported"}</p>
              <div className="deployment-state-track"><div className="flex items-center justify-between gap-3"><span className="text-sm font-medium">Reported state</span><StatusPill status={latest.status} /></div><p className="mt-3 text-xs text-muted-foreground">Started {formatRelativeTime(new Date(latest.created_at))}{latest.deployed_at ? ` · Deployed ${formatRelativeTime(new Date(latest.deployed_at))}` : ""}</p></div>
              <pre className="resource-log-preview">{latest.log?.trim() ? latest.log.split("\n").slice(-6).join("\n") : "No deployment log has been recorded yet."}</pre>
              <div className="mt-5 flex flex-wrap items-center justify-between gap-3"><p className="text-xs text-muted-foreground">Runtime readiness is shown separately above.</p><Link className="text-xs text-primary inline-flex gap-1 items-center" to="/projects/$id/services/$serviceId/deployments/$deploymentId" params={{ id: projectId, serviceId, deploymentId: latest.id }}>View deployment<ArrowRight className="size-3" /></Link></div>
            </>}
          </ResourcePanel>
          <ResourcePanel title="Runtime" description="The image and networking configured for this service.">
            <ResourceFact label="Image"><code className="break-all">{service.image || "Not configured"}</code></ResourceFact>
            {(service.ports ?? []).map(port => <ResourceFact key={port.id} label={port.name || "Port"}><span className="flex flex-wrap justify-end gap-2"><code>{port.port}{port.node_port > 0 ? ` → ${node?.tailscaleIP || "mesh node"}:${port.node_port}` : ""}</code><span className="text-muted-foreground">{port.is_http ? "HTTP" : "TCP"}{port.is_primary ? " · Primary" : ""}{port.is_public ? " · Public" : ""}</span></span></ResourceFact>)}
            {isDatabase && dc && <><ResourceFact label="Engine">{ENGINE_LABELS[dc.engine] || dc.engine} {dc.version}</ResourceFact><ResourceFact label="Storage">{dc.storage_gb} GiB allocated</ResourceFact></>}
            <ResourceFact label="Updated">{formatRelativeTime(new Date(service.updated_at))}</ResourceFact>
          </ResourcePanel>
        </div>
        <div className="space-y-6 min-w-0">
          <ResourcePanel title="Connections">
            {attachedRoutes.map(r => <ResourceFact key={r.id} label={r.zone === "internal" ? "Internal domain" : "Domain"}><Link to="/projects/$id/routes/$routeId" params={{ id: projectId, routeId: r.id }} className="text-primary break-all">{r.hostname}</Link><a className="inline-flex ml-2 text-muted-foreground" href={`https://${r.hostname}`} target="_blank" rel="noopener noreferrer" aria-label={`Open ${r.hostname}`}><ExternalLink className="size-3" /></a></ResourceFact>)}
            {!attachedRoutes.length && <ResourceFact label="Domains"><span className="text-muted-foreground">No routes attached</span></ResourceFact>}
            {(service.ports ?? []).map(port => <ResourceFact key={port.id} label={port.is_primary ? "Primary port" : "Port"}><code>{port.port} · {port.is_http ? "HTTP" : "TCP"}</code></ResourceFact>)}
            {groups.map(group => <ResourceFact key={group.id} label="Variable group"><Link className="text-primary" to="/projects/$id/variables/$groupId" params={{ id: projectId, groupId: group.id }}>{group.name}</Link></ResourceFact>)}
            <ResourceFact label="Stack">{stack ? <Link className="text-primary" to="/projects/$id/stacks/$stackId" params={{ id: projectId, stackId: stack.id }}>{stack.name}</Link> : service.stack_id ? "Loading stack…" : "Standalone service"}</ResourceFact>
          </ResourcePanel>
          <ResourcePanel title="Placement" action={<StatusPill status={service.status} />}>
            {observedNodeNames.length ? observedNodeNames.map(name => <div key={name} className="placement-node"><Server className="size-5 text-muted-foreground" /><div><p className="text-sm font-medium">{name}</p><p className="mt-1 text-xs text-muted-foreground">{(pods || []).filter(p => p.node_name === name).length} pods assigned</p></div></div>) : <p className="text-sm text-muted-foreground">No pod placement has been reported.</p>}
            <ResourceFact label="Scheduling">{service.node_id ? "Pinned to a node" : "Automatic"}</ResourceFact>
            {node && <ResourceFact label="Node"><Link className="text-primary" to="/nodes/$id" params={{ id: node.id }}>{node.name} · {node.tailscaleIP}</Link></ResourceFact>}
            <Link className="mt-4 inline-flex items-center gap-2 text-xs text-primary" to="/projects/$id/services/$serviceId/pods" params={{ id: projectId, serviceId }}>Inspect pods<ArrowRight className="size-3" /></Link>
          </ResourcePanel>
          {isDatabase && <ResourcePanel title="Database access" action={<Button size="sm" variant="outline" disabled={service.status !== "running"} onClick={() => openTab({ id: serviceId, type: "explorer", label: service.name, payload: { serviceId, projectId, dbName: service.name } })}><Table2 className="size-4" />Open Explorer</Button>}>
            {!dc ? (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground/50">Provision the database to see connection details</div>
            ) : (
              <div className="px-4 py-1">
                {dc.engine !== "redis" && <CopyRow label="Database" value={dc.db_name} />}
                {dc.engine !== "redis" && <CopyRow label="Username" value={dc.db_user} />}
                <CopyRow label="Password" value={dc.db_password} />
                <CopyRow label="Slug" value={dc.slug} />
                {internalConnStr && <CopyRow label="Internal" value={internalConnStr} />}
                {publicConnStr && <CopyRow label="Public" value={publicConnStr} />}
                {meshConnStr
                  ? <CopyRow label="Mesh" value={meshConnStr} />
                  : (
                    <div className="flex items-center justify-between py-2 border-b border-border/30 last:border-0">
                      <span className="text-xs text-muted-foreground">Mesh</span>
                      <span className="text-xs text-muted-foreground/40 italic">
                        {dc.node_port ? "resolving node…" : dc.mesh_exposed ? "publishing…" : "in-cluster only"}
                      </span>
                    </div>
                  )
                }
                <p className="py-2.5 text-xs text-muted-foreground leading-relaxed">
                  To connect an app, attach the <span className="text-foreground">{service.name} (service)</span> variable
                  group to it, then reference the connection URL in its env:{" "}
                  <code className="font-mono text-foreground break-all">
                    {`DATABASE_URL=\${${service.name.toUpperCase().replace(/[^A-Z0-9]+/g, "_")}_URL}`}
                  </code>
                </p>
              </div>
            )}
          </ResourcePanel>}
        </div>
      </div>
    </div>
  )
}
