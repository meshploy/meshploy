import { createFileRoute, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { ChevronDown, Loader2, RotateCcw, Trash2, Zap } from "lucide-react"
import { jobs as jobsApi, type ApiJobRun } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId/runs")({
  component: RunsPage,
})

const RUN_STATUS_TEXT: Record<string, string> = {
  idle:    "text-muted-foreground",
  pending: "text-amber-400",
  running: "text-amber-400",
  success: "text-emerald-400",
  failed:  "text-destructive",
}

function RunsPage() {
  const { id: projectId, jobId } = useParams({ from: "/_app/projects/$id/jobs/$jobId/runs" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: job } = useQuery({
    queryKey: ["job", orgId, projectId, jobId],
    queryFn: () => jobsApi.get(orgId, projectId, jobId, token),
    enabled: !!orgId,
  })

  const { data: runs = [], isFetching } = useQuery({
    queryKey: ["job-runs", orgId, projectId, jobId],
    queryFn: () => jobsApi.listRuns(orgId, projectId, jobId, token),
    enabled: !!orgId,
    refetchInterval: job?.status === "running" || job?.status === "pending" ? 3000 : false,
  })

  if (isFetching && runs.length === 0) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="p-6">
      {runs.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Zap className="h-7 w-7 text-muted-foreground/30" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No runs yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Click "Run now" to trigger the first run.</p>
          </div>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {runs.map((run) => (
            <RunRow key={run.id} run={run} orgId={orgId} projectId={projectId} jobId={jobId} token={token} />
          ))}
        </div>
      )}
    </div>
  )
}

function RunRow({
  run, orgId, projectId, jobId, token,
}: {
  run: ApiJobRun
  orgId: string
  projectId: string
  jobId: string
  token: string
}) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)

  const duration = run.started_at && run.finished_at
    ? Math.round((new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000)
    : null

  const dot = cn("h-1.5 w-1.5 rounded-full shrink-0", {
    "bg-muted-foreground/40":     run.status === "idle",
    "bg-amber-400 animate-pulse": run.status === "pending" || run.status === "running",
    "bg-emerald-400":             run.status === "success",
    "bg-destructive":             run.status === "failed",
  })

  const rerunMut = useMutation({
    mutationFn: () => jobsApi.trigger(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["job-runs", orgId, projectId, jobId] })
    },
  })

  const deleteMut = useMutation({
    mutationFn: () => jobsApi.deleteRun(orgId, projectId, jobId, run.id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["job-runs", orgId, projectId, jobId] }),
  })

  return (
    <div>
      <div className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors">
        <span className={dot} />
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <span className={cn("text-xs font-medium capitalize shrink-0", RUN_STATUS_TEXT[run.status] ?? "text-muted-foreground")}>
            {run.status}
          </span>
          <span className="text-xs text-muted-foreground shrink-0">
            {run.started_at
              ? new Date(run.started_at).toLocaleString()
              : new Date(run.created_at).toLocaleString()
            }
          </span>
          {duration !== null && (
            <span className="text-xs text-muted-foreground/50 shrink-0">{duration}s</span>
          )}
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
          <Button variant="ghost" size="icon-sm" onClick={() => rerunMut.mutate()} disabled={rerunMut.isPending}
            title="Re-run" className="text-muted-foreground/50 hover:text-foreground">
            {rerunMut.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={() => deleteMut.mutate()} disabled={deleteMut.isPending}
            title="Delete record" className="text-muted-foreground/50 hover:text-destructive">
            {deleteMut.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={() => setOpen((v) => !v)}
            title="View log" className="text-muted-foreground/50 hover:text-foreground">
            <ChevronDown className={cn("transition-transform", open && "rotate-180")} />
          </Button>
        </div>
      </div>
      {open && (
        <div className="px-4 pb-3 border-t border-border/30">
          {run.log ? (
            <pre className="text-xs font-mono text-muted-foreground/70 bg-muted/20 rounded-md px-3 py-2.5 mt-2 overflow-x-auto whitespace-pre-wrap max-h-64 overflow-y-auto">
              {run.log}
            </pre>
          ) : (
            <p className="text-xs text-muted-foreground/40 italic px-1 pt-2">No log output.</p>
          )}
        </div>
      )}
    </div>
  )
}
