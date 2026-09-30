import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { GitCommitHorizontal, Layers, Loader2, RefreshCw } from "lucide-react"
import { MetricTile, ResourceIntro } from "@/components/layout/resource-workbench"
import { RunStatus } from "@/components/stacks/rollout-status"
import { StackBuildsSetting } from "@/components/stacks/stack-builds-setting"
import { StackImagesSetting } from "@/components/stacks/stack-images-setting"
import { ConfigSaveBar } from "@/components/layout/config-save-bar"
import { stacks as stacksApi, type StackRun } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { livePoll } from "@/lib/live-poll"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/rollouts/")({
  component: RolloutsTab,
})

// Each Sync and Apply of the stack, the way a service lists its deployments:
// a rollout goes on after the request, a layer at a time, so this is where it
// is followed.
function RolloutsTab() {
  const { id: projectId, stackId } = useParams({ from: "/_app/projects/$id/stacks/$stackId/rollouts/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: runs = [], isLoading } = useQuery({
    queryKey: ["stack-runs", orgId, projectId, stackId],
    queryFn: () => stacksApi.runs(orgId!, projectId, stackId, token),
    enabled: !!orgId,
    refetchInterval: livePoll<StackRun[]>((d) => d.some((r) => r.status === "running")),
  })

  // The save bar wraps the whole page, so it floats at the bottom of the
  // window and its room is at the end of the page, not between the settings
  // and the rollouts.
  return (
    <ConfigSaveBar>
    <div className="console-page space-y-6">
      <ResourceIntro title="Rollouts"
        description="Every Sync and Apply, and how its rollout went: the services it rolled out, in the order compose starts them." />
      <div className="resource-metrics">
        <MetricTile icon={Layers} label="Rollouts" value={isLoading ? "…" : runs.length} detail="Syncs and applies kept" />
        <MetricTile icon={Loader2} label="In progress" value={isLoading ? "…" : runs.filter((r) => r.status === "running").length}
          detail="Waiting on a layer or building" />
        <MetricTile icon={RefreshCw} label="Last" value={<span className="text-xl">{runs[0] ? formatRelativeTime(new Date(runs[0].created_at)) : "Never"}</span>}
          detail={runs[0] ? (runs[0].kind === "sync" ? "Sync" : "Apply") : "Sync or Apply from the top"} />
      </div>

      {/* How a rollout builds, and what its builds keep: settings, above the
          rollouts they shape, not in the middle of a list. */}
      {orgId && (
        <div className="space-y-3">
          <StackBuildsSetting orgId={orgId} projectId={projectId} stackId={stackId} token={token} />
          <StackImagesSetting orgId={orgId} projectId={projectId} stackId={stackId} token={token} />
        </div>
      )}

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : runs.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 text-center">
          <p className="text-sm text-muted-foreground">No rollouts yet</p>
          <p className="text-xs text-muted-foreground/60 mt-0.5">Sync or Apply, at the top of the page, and it is followed here.</p>
        </div>
      ) : (
        <div className="console-record-list rounded-xl border border-border overflow-hidden divide-y divide-border/40">
          {runs.map((run) => {
            const total = run.rollout.length
            const done = run.rollout.filter((s) => s.status === "succeeded").length
            const failed = run.rollout.filter((s) => s.status === "failed").length
            return (
              <Link key={run.id} to="/projects/$id/stacks/$stackId/rollouts/$runId"
                params={{ id: projectId, stackId, runId: run.id }} className="execution-history-row">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0 flex-1">
                  <RunStatus status={run.status} />
                  <span className="text-xs font-medium">{run.kind === "sync" ? "Sync" : "Apply"}</span>
                  {run.commit && (
                    <span className="inline-flex items-center gap-1 text-xs font-mono text-muted-foreground">
                      <GitCommitHorizontal className="h-3.5 w-3.5" />{run.commit.slice(0, 7)}
                    </span>
                  )}
                  <span className="text-xs text-muted-foreground">{formatRelativeTime(new Date(run.created_at))}</span>
                </div>
                <span className="text-xs text-muted-foreground shrink-0">
                  {total === 0 ? "nothing to roll out" : `${done} of ${total} done${failed ? `, ${failed} failed` : ""}`}
                </span>
              </Link>
            )
          })}
        </div>
      )}
    </div>
    </ConfigSaveBar>
  )
}
