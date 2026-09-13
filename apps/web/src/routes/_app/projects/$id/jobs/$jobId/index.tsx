import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Clock, Check, Play, ArrowRight } from "lucide-react"
import { jobs } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { MetricTile, ResourcePanel, ResourceFact, StatusPill } from "@/components/layout/resource-workbench"
import { formatRelativeTime } from "@/lib/utils"
export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId/")({ component: JobOverview })
function JobOverview() {
  const { id, jobId } = Route.useParams()
  const token = useAuthStore(s => s.token)!
  const org = useOrgStore(s => s.currentOrg?.id)!
  const { data: job } = useQuery({ queryKey: ["job", org, id, jobId], queryFn: () => jobs.get(org, id, jobId, token), enabled: !!org })
  const { data: runs, isError } = useQuery({ queryKey: ["job-runs", org, id, jobId], queryFn: () => jobs.listRuns(org, id, jobId, token), enabled: !!org, refetchInterval: 10000 })
  if (!job) return null
  const latest = runs?.slice().sort((a,b) => Date.parse(b.created_at)-Date.parse(a.created_at))[0]
  return <div className="console-page space-y-6">
    <div className="resource-metrics">
      <MetricTile icon={Play} label="Recorded runs" value={runs?.length ?? "—"} detail="Retained execution history" />
      <MetricTile icon={Check} label="Successful runs" value={runs?.filter(r => r.status === "success").length ?? "—"} detail="Within retained history" />
      <MetricTile icon={Clock} label="Trigger" value={<span className="text-xl">{job.is_cron ? "Scheduled" : "On demand"}</span>} detail={job.is_cron ? job.schedule : "Use Run now to start an execution"} />
    </div>
    <div className="resource-overview-columns"><div className="space-y-6 min-w-0">
      <ResourcePanel title="Latest run" action={latest && <StatusPill status={latest.status} />}>
        {isError ? <p className="resource-empty">Run history is unavailable.</p> : latest ? <><ResourceFact label="Execution"><code>{latest.k8s_job_name || latest.id.slice(0,8)}</code></ResourceFact><ResourceFact label="Started">{latest.started_at ? formatRelativeTime(new Date(latest.started_at)) : "Waiting to start"}</ResourceFact><pre className="resource-log-preview mt-5">{latest.log || "No log output recorded yet."}</pre></> : <p className="resource-empty">This job has not run yet.</p>}
        <Link to="/projects/$id/jobs/$jobId/runs" params={{ id, jobId }} className="inline-flex items-center gap-2 text-primary text-xs mt-5">View run history<ArrowRight className="size-3" /></Link>
      </ResourcePanel>
      <ResourcePanel title="Execution"><ResourceFact label="Image"><code>{job.image}</code></ResourceFact><ResourceFact label="Command"><code>{job.command || "Image default"}</code></ResourceFact><ResourceFact label="CPU limit">{job.cpu_limit || "Not set"}</ResourceFact><ResourceFact label="Memory limit">{job.memory_limit || "Not set"}</ResourceFact></ResourcePanel>
    </div><aside className="space-y-6 min-w-0">
      <ResourcePanel title="Schedule and retention"><ResourceFact label="Schedule"><code>{job.is_cron ? job.schedule : "Manual"}</code></ResourceFact><ResourceFact label="Concurrency">{job.concurrency_policy || "Default"}</ResourceFact><ResourceFact label="History limit">{job.history_limit} runs</ResourceFact><ResourceFact label="Last run">{job.last_run_at ? formatRelativeTime(new Date(job.last_run_at)) : "Never"}</ResourceFact></ResourcePanel>
      <ResourcePanel title="Configuration"><p className="text-sm leading-relaxed text-muted-foreground">Manage the container image, command, environment and resource limits used for new executions.</p><Link to="/projects/$id/jobs/$jobId/config" params={{ id, jobId }} className="inline-flex items-center gap-2 text-primary text-xs mt-5">Edit configuration<ArrowRight className="size-3" /></Link></ResourcePanel>
    </aside></div>
  </div>
}
