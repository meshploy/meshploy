import { OptionSelect } from "@/components/layout/option-select"
import { MetricTile } from "@/components/layout/resource-workbench"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { createFileRoute, Link } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ServerCrash, Server, Cpu, CheckCircle2, Plus } from "lucide-react"
import { NodesTable } from "@/components/nodes/nodes-table"
import { nodes as nodesApi, toNode } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { HelpButton } from "@/help/help-button"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/nodes/")({
  component: NodesPage,
})

function NodesPage() {
  const [search, setSearch] = useState("")
  const [status, setStatus] = useState("all")
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
    select: (raw) => raw.map(toNode),
    refetchInterval: 15_000,
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading nodes…</span>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Failed to load nodes</p>
        <p className="text-xs text-muted-foreground/60">{(error as Error).message}</p>
      </div>
    )
  }

  const nodeList = data ?? []
  const online = nodeList.filter((n) => n.status === "online").length

  return (
    <div className="console-page p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight flex items-center gap-2" aria-labelledby="page-title"><span id="page-title">Nodes</span><HelpButton topic="nodes" label="How nodes work" /></h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {online} of {nodeList.length} nodes online
          </p>
        </div>
      </div>
      <div className="resource-metrics">
        <MetricTile icon={Server} label="Online" value={online} unit={`/ ${nodeList.length}`} detail="Mesh connectivity"/>
        <MetricTile icon={CheckCircle2} label="Cluster ready" value={nodeList.filter(n=>n.k8sReady).length} detail="Nodes reporting Kubernetes readiness"/>
        <MetricTile icon={Cpu} label="Online CPU capacity" value={nodeList.filter(n=>n.status === "online" && n.k8sMember).reduce((sum,n)=>sum+(n.cpuCores || 0),0)} unit="cores" detail="Cluster nodes' hardware, not current utilization"/>
      </div>
      <div className="flex flex-wrap items-center gap-3"><Input aria-label="Search nodes" placeholder="Search by name or mesh IP…" value={search} onChange={e=>setSearch(e.target.value)} className="h-10 max-w-md"/><OptionSelect label="Filter node status" value={status} onChange={setStatus} options={[{"value": "all", "label": "All statuses"}, {"value": "online", "label": "Online"}, {"value": "offline", "label": "Not online"}]} /><Button variant="outline" className="sm:ml-auto" render={<Link to="/cluster"/>}><Plus className="size-4"/>Connect a node</Button></div>
      <NodesTable nodes={nodeList.filter(n => `${n.name} ${n.tailscaleIP}`.toLowerCase().includes(search.toLowerCase()) && (status === "all" || (status === "online" ? n.status === "online" : n.status !== "online")))} />
    </div>
  )
}
