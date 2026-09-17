import { ResourceSearch, useResourceSearch } from "@/components/layout/resource-search"
import { createFileRoute, useNavigate, useParams, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { ExternalLink, Globe, Loader2, Network, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import { routes as routesApi, tcpRoutes as tcpRoutesApi, services as servicesApi, type ApiDbRoute, type ApiTCPRoute } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { StackPill, useStackNames } from "@/components/stacks/stack-pill"
import { PublishStateBadge, PublishToggle } from "@/components/routes/publish-toggle"

export const Route = createFileRoute("/_app/projects/$id/routes/")({
  component: RoutesTab,
})

const ZONE_STYLES: Record<string, string> = {
  public:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  external: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  internal: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  preview:  "bg-blue-500/10 text-blue-400 border-blue-500/20",
}

function RoutesTab() {
  const { search, setSearch, matches } = useResourceSearch()
  const { id: projectId } = useParams({ from: "/_app/projects/$id/routes/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const stackNames = useStackNames(orgId, projectId)
  const navigate = useNavigate()

  const { data: routeList = [], isLoading, isError, error, refetch } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const { data: tcpList = [] } = useQuery({
    queryKey: ["tcp-routes", orgId, projectId],
    queryFn: () => tcpRoutesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
    // Pending until the gateway reports back, usually within 30 seconds.
    refetchInterval: (q) => q.state.data?.some((r) => r.status === "pending") ? 5000 : false,
  })
  const { data: serviceList = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId!, projectId, token),
    enabled: !!orgId && tcpList.length > 0,
  })
  const serviceNames = new Map(serviceList.map((s) => [s.id, s.name]))

  const goToNew = () =>
    navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "route" } })

  return (
    <div className="console-page p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h1>Routes</h1>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && <span className="text-xs text-muted-foreground">{routeList.length + tcpList.length}</span>}
        </div>
        <Button size="sm" className="gap-1.5" onClick={goToNew}>
          <Plus className="h-3.5 w-3.5" />
          New Route
        </Button>
      </div>

      {isError && <div role="alert" className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/40 p-4"><p className="text-sm text-destructive">{error.message}</p><Button variant="outline" onClick={() => refetch()}>Try again</Button></div>}
      <ResourceSearch value={search} onChange={setSearch} count={routeList.length} label="routes" empty={routeList.length > 0 && !routeList.some(matches)} />
      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : routeList.length === 0 && tcpList.length > 0 ? null : routeList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Globe className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No routes configured</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Map a hostname to a service or mesh device</p>
          </div>
          <Button size="sm" className="gap-1.5 mt-1" onClick={goToNew}>
            <Plus className="h-3.5 w-3.5" />
            New Route
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          <div>
            <h2 className="text-sm font-medium">HTTPS routes</h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              Hostnames the gateway serves with TLS and forwards over the mesh, by path.
            </p>
          </div>
        <div className="console-data-table rounded-xl border border-border overflow-hidden">
          <Table>
            <TableHeader className="bg-muted/20">
              <TableRow className="border-b border-border/40 hover:bg-transparent">
                <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground w-[40%]">Hostname</TableHead>
                <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground w-[10%]">State</TableHead>
                <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground w-[10%]">Zone</TableHead>
                <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground">Paths</TableHead>
                <TableHead aria-label="Actions" className="w-32" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {routeList.filter(matches).map((route) => (
                <RouteRow
                  key={route.id}
                  route={route}
                  stackNames={stackNames}
                  orgId={orgId}
                  projectId={projectId}
                  onClick={() => navigate({ to: "/projects/$id/routes/$routeId", params: { id: projectId, routeId: route.id } })}
                />
              ))}
            </TableBody>
          </Table>
        </div>
        </div>
      )}

      {tcpList.length > 0 && (
        <div className="space-y-2 pt-2">
          <div>
            <h2 className="text-sm font-medium">TCP ports</h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              Ports the gateway publishes and forwards over the mesh, for what does not speak HTTP.
            </p>
          </div>
          <div className="console-data-table rounded-xl border border-border overflow-hidden">
            <Table>
              <TableHeader className="bg-muted/20">
                <TableRow className="border-b border-border/40 hover:bg-transparent">
                  <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground w-[40%]">Gateway port</TableHead>
                  <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground w-[20%]">State</TableHead>
                  <TableHead className="px-4 py-2.5 text-[11px] font-medium text-muted-foreground">Allowed from</TableHead>
                  <TableHead aria-label="Actions" className="w-32" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {tcpList.map((route) => (
                  <TCPRouteRow key={route.id} route={route} serviceNames={serviceNames} projectId={projectId} />
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      )}
    </div>
  )
}

const TCP_STATE_STYLES: Record<string, string> = {
  open:    "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  pending: "bg-muted text-muted-foreground border-border",
  failed:  "bg-destructive/10 text-destructive border-destructive/20",
  paused:  "bg-muted text-muted-foreground border-border",
}

const TCP_STATE_LABELS: Record<string, string> = {
  open: "listening",
  pending: "opening…",
  failed: "failed",
  paused: "paused",
}

function TCPRouteRow({ route, serviceNames, projectId }: {
  route: ApiTCPRoute
  serviceNames: Map<string, string>
  projectId: string
}) {
  const target = route.service_id
    ? serviceNames.get(route.service_id) ?? "a service"
    : `${route.target_ip}:${route.target_port}`

  return (
    <TableRow className="border-b border-border/30">
      <TableCell className="px-4 py-3">
        <div className="flex items-center gap-2">
          <Network className="h-3.5 w-3.5 text-muted-foreground/50 shrink-0" />
          <code className="font-medium text-foreground font-mono text-sm">:{route.gateway_port}</code>
          <span className="text-xs text-muted-foreground">→</span>
          {route.service_id ? (
            <Link to="/projects/$id/services/$serviceId/config" params={{ id: projectId, serviceId: route.service_id }} className="text-sm hover:text-primary">
              {target}
            </Link>
          ) : (
            <span className="text-sm text-muted-foreground font-mono">{target}</span>
          )}
        </div>
      </TableCell>
      <TableCell className="px-4 py-3">
        <Badge className={`text-[11px] px-1.5 py-0 h-4.5 border ${TCP_STATE_STYLES[route.status] ?? ""}`}>
          {!route.published && route.status !== "paused" ? "closing…" : TCP_STATE_LABELS[route.status] ?? route.status}
        </Badge>
        {route.status === "failed" && route.last_error && (
          <p className="text-[11px] text-destructive mt-1">{route.last_error}</p>
        )}
      </TableCell>
      <TableCell className="px-4 py-3">
        {route.allowed_cidrs.length === 0 ? (
          <span className="text-xs text-amber-400">Anyone</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {route.allowed_cidrs.map((c) => (
              <code key={c} className="text-[11px] font-mono bg-muted/50 border border-border/40 px-1.5 py-0.5 rounded text-muted-foreground">{c}</code>
            ))}
          </div>
        )}
      </TableCell>
      <TableCell className="px-3 py-3 text-right">
        <PublishToggle kind="tcp" routeId={route.id} projectId={projectId} published={route.published} label={`:${route.gateway_port}`} />
      </TableCell>
    </TableRow>
  )
}

function RouteRow({ route, onClick, stackNames, orgId, projectId }: {
  route: ApiDbRoute
  onClick: () => void
  stackNames: Map<string, string>
  orgId: string | undefined
  projectId: string
}) {
  const MAX_PATHS = 3
  const shown = route.targets.slice(0, MAX_PATHS)
  const overflow = route.targets.length - MAX_PATHS

  return (
    <TableRow tabIndex={0} onKeyDown={e => { if (e.target === e.currentTarget && e.key === "Enter") onClick() }} className="border-b border-border/30 hover:bg-muted/20 cursor-pointer" onClick={e => { if (!(e.target as HTMLElement).closest("a,button")) onClick() }}>
      <TableCell className="px-4 py-3">
        <div className="flex items-center gap-2">
          <Globe className="h-3.5 w-3.5 text-muted-foreground/50 shrink-0" />
          <Link to="/projects/$id/routes/$routeId" params={{ id: projectId, routeId: route.id }} className="font-medium text-foreground font-mono text-sm hover:text-primary">{route.hostname}</Link>
          <StackPill stackId={route.stack_id} stackNames={stackNames} orgId={orgId} projectId={projectId} />
        </div>
      </TableCell>
      <TableCell className="px-4 py-3">
        <PublishStateBadge published={route.published} />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Badge className={`text-[11px] px-1.5 py-0 h-4.5 border ${ZONE_STYLES[route.zone] ?? "bg-muted text-muted-foreground border-border"}`}>
          {route.zone}
        </Badge>
      </TableCell>
      <TableCell className="px-4 py-3">
        <div className="flex flex-wrap items-center gap-1">
          {route.targets.length === 0 ? (
            <span className="text-xs text-muted-foreground/40">—</span>
          ) : (
            <>
              {shown.map((t) => (
                <span key={t.id} className="flex items-center gap-0.5">
                  <code className="text-[11px] font-mono bg-muted/50 border border-border/40 px-1.5 py-0.5 rounded text-muted-foreground">
                    {t.path}
                  </code>
                  {t.redirect_route_id && (
                    <span className="text-[11px] font-medium text-amber-400 bg-amber-500/10 border border-amber-500/20 px-1 py-0.5 rounded">
                      ↪ {t.redirect_code || 301}
                    </span>
                  )}
                </span>
              ))}
              {overflow > 0 && (
                <span className="text-[11px] text-muted-foreground/50">+{overflow} more</span>
              )}
            </>
          )}
        </div>
      </TableCell>
      <TableCell className="px-3 py-3 text-right">
        <div className="flex items-center justify-end gap-3">
        <PublishToggle kind="http" routeId={route.id} projectId={projectId} published={route.published} label={route.hostname} />
        <a
          aria-label={`Open ${route.hostname} in a new tab`}
          href={`https://${route.hostname}`}
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e) => e.stopPropagation()}
          className="text-muted-foreground/40 hover:text-muted-foreground transition-colors"
        >
          <ExternalLink className="h-3.5 w-3.5" />
        </a>
        </div>
      </TableCell>
    </TableRow>
  )
}
