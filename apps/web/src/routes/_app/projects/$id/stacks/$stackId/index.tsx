import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Box, Layers, Variable, ArrowRight } from "lucide-react"
import { stacks } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { MetricTile, ResourcePanel, ResourceFact, StatusPill } from "@/components/layout/resource-workbench"
import { formatRelativeTime } from "@/lib/utils"
export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/")({ component: StackOverview })
function StackOverview() {
  const { id, stackId } = Route.useParams()
  const token = useAuthStore(s => s.token)!
  const org = useOrgStore(s => s.currentOrg?.id)!
  const { data: stack } = useQuery({ queryKey: ["stack", org, id, stackId], queryFn: () => stacks.get(org, id, stackId, token), enabled: !!org })
  const { data: services, isError } = useQuery({ queryKey: ["stack-services", org, id, stackId], queryFn: () => stacks.listServices(org, id, stackId, token), enabled: !!org, refetchInterval: 15000 })
  if (!stack) return null
  return <div className="console-page space-y-6">
    <div className="resource-metrics">
      <MetricTile icon={Box} label="Services" value={services?.length ?? "—"} detail="Managed by this stack" />
      <MetricTile icon={Layers} label="Running" value={services?.filter(s => s.status === "running").length ?? "—"} detail="Current service state" />
      <MetricTile icon={Variable} label="Variables" value={Object.keys(stack.variables ?? {}).length} detail="Stack-level configuration" />
    </div>
    <div className="resource-overview-columns"><div className="space-y-6 min-w-0">
      <ResourcePanel title="Services" description="The applications and databases that make up this stack.">
        {isError ? <p className="resource-empty">Services are unavailable.</p> : !services?.length ? <p className="resource-empty">Apply the stack to create its services.</p> : services.map(s => <Link key={s.id} to="/projects/$id/services/$serviceId/overview" params={{ id, serviceId:s.id }} className="workbench-resource-row"><Box className="size-5 text-muted-foreground" /><div className="flex-1 min-w-0"><p className="text-sm font-medium">{s.name}</p><p className="text-xs text-muted-foreground mt-1 truncate">{s.image || "No image configured"}</p></div><StatusPill status={s.status} /><ArrowRight className="size-4 text-muted-foreground" /></Link>)}
      </ResourcePanel>
      <ResourcePanel title="Stack definition" action={<Link className="text-xs text-primary" to="/projects/$id/stacks/$stackId/editor" params={{ id, stackId }}>Open editor →</Link>}><p className="text-sm text-muted-foreground leading-relaxed">Review and apply the compose definition, synchronize its source, or manage the stack lifecycle in the editor.</p><ResourceFact label="Last applied">{stack.last_applied_at ? formatRelativeTime(new Date(stack.last_applied_at)) : "Never"}</ResourceFact><ResourceFact label="Updated">{formatRelativeTime(new Date(stack.updated_at))}</ResourceFact></ResourcePanel>
    </div><aside className="space-y-6 min-w-0">
      <ResourcePanel title="Source"><ResourceFact label="Managed through">{stack.git_mode ? "Git repository" : "Compose editor"}</ResourceFact>{stack.git_mode && <><ResourceFact label="Repository"><code>{stack.git_repo}</code></ResourceFact><ResourceFact label="Branch"><code>{stack.git_branch || "Default"}</code></ResourceFact><ResourceFact label="Path"><code>{stack.git_path || "/"}</code></ResourceFact><ResourceFact label="Last sync">{stack.git_last_synced_at ? formatRelativeTime(new Date(stack.git_last_synced_at)) : "Never"}</ResourceFact></>}</ResourcePanel>
      <ResourcePanel title="Environment"><p className="text-sm text-muted-foreground leading-relaxed">Stack variables configure the compose definition when it is applied.</p><Link className="inline-flex gap-2 items-center mt-5 text-xs text-primary" to="/projects/$id/stacks/$stackId/variables" params={{ id, stackId }}>Manage variables<ArrowRight className="size-3" /></Link></ResourcePanel>
    </aside></div>
  </div>
}
