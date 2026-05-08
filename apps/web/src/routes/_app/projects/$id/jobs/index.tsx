import { createFileRoute, Link, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Clock, Loader2, Play, Trash2, Zap } from "lucide-react"
import { jobs as jobsApi, type ApiJob } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/jobs/")({
  component: JobsPage,
})

function statusDot(status: string) {
  return cn("h-2 w-2 rounded-full shrink-0", {
    "bg-muted-foreground/30": status === "idle",
    "bg-yellow-400 animate-pulse": status === "pending" || status === "running",
    "bg-emerald-400": status === "success",
    "bg-destructive": status === "failed",
  })
}

function JobsPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/jobs/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["jobs", orgId, projectId],
    queryFn: () => jobsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const deleteMut = useMutation({
    mutationFn: (jobId: string) => jobsApi.delete(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
    },
  })

  const triggerMut = useMutation({
    mutationFn: (jobId: string) => jobsApi.trigger(orgId, projectId, jobId, token),
    onSuccess: (_, jobId) => {
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["job-runs", orgId, projectId, jobId] })
    },
  })

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Jobs</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && <span className="text-xs text-muted-foreground">{list.length}</span>}
        </div>
        <Link to="/projects/$id/new" params={{ id: projectId }} search={{ type: "job" }}>
          <Button size="sm" className="gap-1.5">
            <Zap className="h-3.5 w-3.5" /> New job
          </Button>
        </Link>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Zap className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No jobs yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              Create one-shot jobs or scheduled cron jobs
            </p>
          </div>
          <Link to="/projects/$id/new" params={{ id: projectId }} search={{ type: "job" }} className="mt-1">
            <Button size="sm" className="gap-1.5">
              <Zap className="h-3.5 w-3.5" /> New job
            </Button>
          </Link>
        </div>
      ) : (
        <JobSection
          jobs={list}
          projectId={projectId}
          onDelete={(id) => deleteMut.mutate(id)}
          onTrigger={(id) => triggerMut.mutate(id)}
          deletingId={deleteMut.isPending ? deleteMut.variables : undefined}
          triggeringId={triggerMut.isPending ? triggerMut.variables : undefined}
        />
      )}
    </div>
  )
}

function JobSection({
  jobs, projectId, onDelete, onTrigger, deletingId, triggeringId,
}: {
  jobs: ApiJob[]
  projectId: string
  onDelete: (id: string) => void
  onTrigger: (id: string) => void
  deletingId?: string
  triggeringId?: string
}) {
  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border/40 bg-muted/20">
              <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Name</th>
              <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Image</th>
              <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider w-[140px]">Schedule</th>
              <th className="text-left px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider w-[100px]">Status</th>
              <th className="px-4 py-2.5 w-[80px]" />
            </tr>
          </thead>
          <tbody>
            {jobs.map((job, i) => (
              <JobRow
                key={job.id}
                job={job}
                last={i === jobs.length - 1}
                projectId={projectId}
                onDelete={() => onDelete(job.id)}
                onTrigger={() => onTrigger(job.id)}
                isDeleting={deletingId === job.id}
                isTriggering={triggeringId === job.id}
              />
            ))}
          </tbody>
        </table>
    </div>
  )
}

function JobRow({
  job, last, projectId, onDelete, onTrigger, isDeleting, isTriggering,
}: {
  job: ApiJob
  last: boolean
  projectId: string
  onDelete: () => void
  onTrigger: () => void
  isDeleting: boolean
  isTriggering: boolean
}) {
  const navigate = useNavigate()

  return (
    <>
      <tr
        className={cn(
          "hover:bg-muted/10 transition-colors cursor-pointer",
          !last && "border-b border-border/30"
        )}
        onClick={() => navigate({ to: "/projects/$id/jobs/$jobId", params: { id: projectId, jobId: job.id } })}
      >
        <td className="px-4 py-3">
          <div className="flex items-center gap-2">
            {job.is_cron
              ? <Clock className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" />
              : <Zap className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" />
            }
            <span className="text-xs font-medium">{job.name}</span>
          </div>
        </td>
        <td className="px-4 py-3">
          <code className="text-xs text-muted-foreground/70 font-mono">{job.image}</code>
        </td>
        <td className="px-4 py-3">
          {job.is_cron
            ? <code className="text-xs font-mono text-muted-foreground/70">{job.schedule || "—"}</code>
            : <span className="text-xs text-muted-foreground/40">one-shot</span>
          }
        </td>
        <td className="px-4 py-3">
          <div className="flex items-center gap-1.5">
            <div className={statusDot(job.status)} />
            <span className="text-xs text-muted-foreground/70 capitalize">{job.status}</span>
          </div>
        </td>
        <td className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
          <div className="flex items-center justify-end gap-0.5">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onTrigger}
              disabled={isTriggering}
              title="Run now"
              className="text-muted-foreground/40 hover:text-primary"
            >
              {isTriggering ? <Loader2 className="animate-spin" /> : <Play />}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onDelete}
              disabled={isDeleting}
              title="Delete"
              className="text-muted-foreground/40 hover:text-destructive"
            >
              {isDeleting ? <Loader2 className="animate-spin" /> : <Trash2 />}
            </Button>
          </div>
        </td>
      </tr>
    </>
  )
}
