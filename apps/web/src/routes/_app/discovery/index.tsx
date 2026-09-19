import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { useState } from "react"
import { Boxes, Server, Loader2, Network, Lock, Globe } from "lucide-react"
import { discovery as discoveryApi, type ApiDiscovery, type ApiEndpoint, type ApiNodeDiscovery } from "@/lib/api"
import { ContainerTable } from "@/components/nodes/host-containers"
import { RouteEndpointDialog } from "@/components/discovery/route-endpoint-dialog"
import { Button } from "@/components/ui/button"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

// What runs on this org's nodes that Meshploy does not route.
//
// Two sources merged into one row per port: what listens on the host, and what
// the container runtime publishes. Read-only for now - the actions each row
// leads to are added one at a time.

export const Route = createFileRoute("/_app/discovery/")({
  component: DiscoveryPage,
})

const thCls = "px-4 py-2.5 font-medium text-muted-foreground/70 text-[11px]"
const tdCls = "px-4 py-3"

type View = "endpoints" | "containers"

function DiscoveryPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const [view, setView] = useState<View>("endpoints")

  const { data, isPending } = useQuery<ApiDiscovery>({
    queryKey: ["discovery", orgId],
    queryFn: () => discoveryApi.get(orgId!, token),
    enabled: !!orgId,
    refetchInterval: 30_000,
    retry: false,
    throwOnError: false,
  })

  const nodes = data?.nodes ?? []
  const endpoints = nodes.flatMap((n) => n.endpoints)
  const routed = endpoints.filter((e) => e.routed?.length).length

  return (
    <div className="console-page p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Discovery</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Everything running on your nodes that Meshploy does not manage. Read from each machine; nothing here is touched.
          </p>
        </div>
      </div>

      <SegmentedControl
        value={view}
        onValueChange={setView}
        options={[
          { value: "endpoints", label: "Endpoints", icon: <Network className="h-3.5 w-3.5" /> },
          { value: "containers", label: "Containers", icon: <Boxes className="h-3.5 w-3.5" /> },
        ]}
      />

      {isPending ? (
        <div className="flex items-center justify-center h-40 gap-2 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span className="text-sm">Looking…</span>
        </div>
      ) : nodes.length === 0 ? (
        <div className="rounded-xl border border-border p-8 text-center text-sm text-muted-foreground/60">
          No node is reporting yet. The host agent runs on the gateway: there, <code className="font-mono">sudo meshploy host start</code>.
        </div>
      ) : (
        <div className="space-y-8">
          {view === "endpoints" && (
            <p className="text-xs text-muted-foreground">
              {endpoints.length} {endpoints.length === 1 ? "endpoint" : "endpoints"} on{" "}
              {nodes.length} {nodes.length === 1 ? "node" : "nodes"}
              {routed > 0 && <>, {routed} of them routed by Meshploy</>}.
            </p>
          )}
          {nodes.map((node) => (
            <NodeSection key={node.node_id} node={node} view={view} />
          ))}
        </div>
      )}

      {(data?.silent?.length ?? 0) > 0 && (
        <section className="space-y-2">
          <h2 className="text-sm font-medium text-foreground">Not reporting</h2>
          <div className="rounded-xl border border-border divide-y divide-border/40">
            {data!.silent.map((s) => (
              <div key={s.node_id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 text-xs">
                <span className="inline-flex items-center gap-1.5 text-foreground/80">
                  <Server className="h-3.5 w-3.5 text-muted-foreground" />
                  {s.name}
                </span>
                <span className="text-muted-foreground">{s.reason}</span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function NodeSection({ node, view }: { node: ApiNodeDiscovery; view: View }) {
  const [routing, setRouting] = useState<ApiEndpoint | null>(null)

  return (
    <section className="space-y-3">
      {/* The machine, named and linked, with its role as a badge - but only
          when the badge says something the name does not. Plenty of gateways
          are called "gateway", and "gateway gateway" is worse than neither. */}
      <div className="flex items-center gap-2">
        <Link
          to="/nodes/$id"
          params={{ id: node.node_id }}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-foreground hover:text-primary"
        >
          <Server className="h-4 w-4 text-muted-foreground" />
          {node.name}
        </Link>
        {node.gateway && node.name.trim().toLowerCase() !== "gateway" && (
          <Badge variant="secondary" className="text-[11px] px-1.5 py-0 h-4">gateway</Badge>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {node.mesh_ip && <code className="font-mono">{node.mesh_ip}</code>}
        {node.runtime && <span>{node.runtime}{node.version ? ` ${node.version}` : ""}</span>}
        {node.checked_at && <span>Checked {formatRelativeTime(new Date(node.checked_at))}</span>}
        {node.stale && (
          <Badge variant="outline" className="text-[11px] px-1.5 py-0 h-4 border-amber-500/20 bg-amber-500/10 text-amber-400">
            Report is stale
          </Badge>
        )}
        {node.hidden > 0 && <span>{node.hidden} of Meshploy&apos;s own hidden</span>}
        {node.error && <span className="text-amber-400">{node.error}</span>}
      </div>

      {view === "endpoints"
        ? <EndpointTable endpoints={node.endpoints} onRoute={setRouting} />
        : <ContainerTable containers={node.containers} />}

      <RouteEndpointDialog endpoint={routing} nodeName={node.name} onClose={() => setRouting(null)} />
    </section>
  )
}

function EndpointTable({ endpoints, onRoute }: { endpoints: ApiEndpoint[]; onRoute: (e: ApiEndpoint) => void }) {
  if (endpoints.length === 0) {
    return (
      <div className="console-data-table rounded-xl border border-border p-8 text-center text-sm text-muted-foreground/50">
        Nothing listening here that Meshploy does not already route.
      </div>
    )
  }
  return (
    <div className="console-data-table rounded-xl border border-border overflow-hidden">
      <Table>
        <TableHeader className="bg-muted/20">
          <TableRow className="border-b border-border/40 hover:bg-transparent">
            <TableHead className={thCls}>Endpoint</TableHead>
            <TableHead className={thCls}>Type</TableHead>
            <TableHead className={thCls}>Held by</TableHead>
            <TableHead className={thCls}>Reach</TableHead>
            <TableHead className={thCls}>Routed</TableHead>
            <TableHead aria-label="Actions" className={thCls} />
          </TableRow>
        </TableHeader>
        <TableBody>
          {endpoints.map((e) => (
            <TableRow key={e.id} className="border-b border-border/30">
              <TableCell className={tdCls}>
                <code className="font-mono text-[11px] text-foreground">{e.address}:{e.port}</code>
                <span className="ml-2 text-[11px] text-muted-foreground/60">{e.protocol}</span>
              </TableCell>
              <TableCell className={`${tdCls} text-xs`}>
                {e.label
                  ? <span className="text-foreground/80">{e.label}</span>
                  : <span className="text-muted-foreground/40">—</span>}
              </TableCell>
              <TableCell className={tdCls}>
                <Holder endpoint={e} />
              </TableCell>
              <TableCell className={tdCls}>
                <Reach endpoint={e} />
              </TableCell>
              <TableCell className={`${tdCls} text-xs`}>
                {e.routed?.length
                  ? (
                    <div className="flex flex-wrap gap-1">
                      {e.routed.map((r) => (
                        <Badge key={r.id} variant="outline" className="text-[11px] px-1.5 py-0 h-4 border-emerald-500/20 bg-emerald-500/10 text-emerald-400">
                          {r.name}
                        </Badge>
                      ))}
                    </div>
                  )
                  : <span className="text-muted-foreground/40">Not routed</span>}
              </TableCell>
              <TableCell className={`${tdCls} text-right`}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs"
                  disabled={!e.routable}
                  title={e.routable ? undefined : e.reason}
                  onClick={() => onRoute(e)}
                >
                  {e.routed?.length ? "Route again" : "Route"}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

/** What holds the port: a container where one owns it, otherwise the process. */
function Holder({ endpoint: e }: { endpoint: ApiEndpoint }) {
  if (e.container) {
    return (
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="outline" className="text-[11px] px-1.5 py-0 h-4 gap-1">
          <Boxes className="h-2.5 w-2.5" />
          {e.container.name}
        </Badge>
        {e.container.compose_project && (
          <span className="text-[11px] text-muted-foreground">{e.container.compose_project}</span>
        )}
        {e.container.host_network && (
          <span className="text-[11px] text-muted-foreground/70">host network</span>
        )}
      </div>
    )
  }
  if (e.via_runtime) {
    return <span className="text-xs text-muted-foreground">a container, through the runtime</span>
  }
  if (e.source === "published") {
    return <span className="text-xs text-muted-foreground">published by the runtime</span>
  }
  return e.process
    ? <code className="font-mono text-[11px] text-muted-foreground">{e.process}</code>
    : <span className="text-muted-foreground/40">—</span>
}

/** Where it can be reached from, which decides what a route would have to do. */
function Reach({ endpoint: e }: { endpoint: ApiEndpoint }) {
  if (!e.routable) {
    return <span className="text-xs text-amber-400" title={e.reason}>Not reachable</span>
  }
  if (e.scope === "host") {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" title="Bound to loopback. On the gateway that is still routable: Caddy and the proxy run on the host's network.">
        <Lock className="h-3 w-3" />
        This machine
      </span>
    )
  }
  if (e.scope === "all") {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
        <Globe className="h-3 w-3" />
        Every interface
      </span>
    )
  }
  return <span className="text-xs text-muted-foreground">One address</span>
}
