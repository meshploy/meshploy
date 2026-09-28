import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, GitCommitHorizontal, Loader2 } from "lucide-react"
import { ResourceFact, ResourceIntro, ResourcePanel } from "@/components/layout/resource-workbench"
import { RunStatus, StepStatus } from "@/components/stacks/rollout-status"
import { stacks as stacksApi, type RolloutStep, type StackRun } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { livePoll } from "@/lib/live-poll"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/rollouts/$runId")({
  component: RolloutPage,
})

// One Sync or Apply: its services in the order compose starts them, each with
// its own deployment's log, and what the apply said.
function RolloutPage() {
  const { id: projectId, stackId, runId } = useParams({ from: "/_app/projects/$id/stacks/$stackId/rollouts/$runId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: run, isLoading } = useQuery({
    queryKey: ["stack-run", orgId, projectId, stackId, runId],
    queryFn: () => stacksApi.run(orgId!, projectId, stackId, runId, token),
    enabled: !!orgId,
    refetchInterval: livePoll<StackRun>((d) => d.status === "running"),
  })

  if (isLoading || !run) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const layers = new Map<number, RolloutStep[]>()
  for (const step of run.rollout) layers.set(step.layer, [...(layers.get(step.layer) ?? []), step])
  const ordered = [...layers.entries()].sort(([a], [b]) => a - b)
  const r = run.result
  const lists: [string, string[] | null][] = [["Created", r.created], ["Updated", r.updated], ["Unlinked", r.deleted]]

  return (
    <div className="console-page space-y-6">
      <Link to="/projects/$id/stacks/$stackId/rollouts" params={{ id: projectId, stackId }}
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-3.5 w-3.5" />All rollouts
      </Link>
      <ResourceIntro title={`${run.kind === "sync" ? "Sync" : "Apply"} ${formatRelativeTime(new Date(run.created_at))}`}
        description={run.status === "running"
          ? "Rolling out: each layer starts once the one before it is up. This page follows it."
          : "Each layer started once the one before it was up; a failed layer stops what depends on it."}
        action={<RunStatus status={run.status} />} />

      <ResourcePanel title="Rollout" description={ordered.length === 0 ? "Nothing needed rolling out: the cluster already matched the file." : undefined}>
        {ordered.map(([layer, steps]) => (
          <div key={layer} className="py-3 first:pt-0 last:pb-0 border-b border-border last:border-b-0">
            <p className="text-[11px] uppercase tracking-wide text-muted-foreground mb-2">
              {layerHeading(layer)}
            </p>
            <div className="space-y-2">
              {steps.map((step) => (
                <div key={step.service_id} className="flex flex-wrap items-center justify-between gap-2">
                  <Link to="/projects/$id/services/$serviceId" params={{ id: projectId, serviceId: step.service_id }}
                    className="text-sm font-medium hover:text-primary">{step.name}</Link>
                  <div className="flex items-center gap-3">
                    <StepStatus status={step.status} />
                    {step.deployment_id && (
                      <Link to="/projects/$id/services/$serviceId/deployments/$deploymentId"
                        params={{ id: projectId, serviceId: step.service_id, deploymentId: step.deployment_id }}
                        className="text-xs text-primary hover:underline">Log</Link>
                    )}
                  </div>
                  {step.error && <p className="w-full text-xs text-destructive">{step.error}</p>}
                </div>
              ))}
            </div>
          </div>
        ))}
      </ResourcePanel>

      <ResourcePanel title="What the apply said">
        {run.commit && (
          <ResourceFact label="Commit">
            <span className="inline-flex items-center gap-1 font-mono"><GitCommitHorizontal className="h-3.5 w-3.5" />{run.commit.slice(0, 12)}</span>
          </ResourceFact>
        )}
        {lists.filter(([, v]) => (v ?? []).length > 0).map(([label, v]) => (
          <ResourceFact key={label} label={label}>{(v ?? []).join(", ")}</ResourceFact>
        ))}
        {(r.errors ?? []).map((e) => <p key={e} className="text-xs text-destructive py-1">{e}</p>)}
        {(r.warnings ?? []).map((w) => <p key={w} className="text-xs text-amber-400/90 py-1">{w}</p>)}
        {!run.commit && lists.every(([, v]) => (v ?? []).length === 0) && !(r.errors ?? []).length && !(r.warnings ?? []).length && (
          <p className="text-xs text-muted-foreground">No changes to the stack's services.</p>
        )}
      </ResourcePanel>
    </div>
  )
}

const ORDINALS = ["First", "Second", "Third", "Fourth", "Fifth", "Sixth", "Seventh", "Eighth", "Ninth", "Tenth"]

// A layer as a person counts it: the first, then the second once the first is
// up. Counted from zero inside, which read as "after layer 1" on the second.
function layerHeading(layer: number): string {
  const name = ORDINALS[layer] ?? `Layer ${layer + 1}`
  if (layer === 0) return name
  const before = (ORDINALS[layer - 1] ?? `layer ${layer}`).toLowerCase()
  return `${name}, once the ${before} is up`
}
