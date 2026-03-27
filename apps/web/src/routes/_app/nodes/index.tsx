import { createFileRoute } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ServerCrash } from "lucide-react"
import { NodesTable } from "@/components/nodes/nodes-table"
import { nodes as nodesApi, toNode } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/nodes/")({
  component: NodesPage,
})

function NodesPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
    select: (raw) => raw.map(toNode),
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
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Nodes</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {online} of {nodeList.length} nodes online
          </p>
        </div>
      </div>
      <NodesTable nodes={nodeList} />
    </div>
  )
}
