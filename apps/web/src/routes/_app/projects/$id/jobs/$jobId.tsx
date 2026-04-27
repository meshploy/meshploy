import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Clock,
  Loader2,
  Pencil,
  Play,
  ServerCrash,
  Trash2,
  X,
  Zap,
} from "lucide-react"
import { jobs as jobsApi, type ApiJob, type ApiJobRun } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId")({
  component: JobDetailPage,
})

// ─── Status helpers ───────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const cls = cn("inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full border", {
    "border-border/40 text-muted-foreground":              status === "idle",
    "border-yellow-500/30 text-yellow-400 bg-yellow-400/10": status === "pending" || status === "running",
    "border-emerald-500/30 text-emerald-400 bg-emerald-400/10": status === "success",
    "border-destructive/30 text-destructive bg-destructive/10": status === "failed",
  })
  const dot = cn("h-1.5 w-1.5 rounded-full", {
    "bg-muted-foreground/50":   status === "idle",
    "bg-yellow-400 animate-pulse": status === "pending" || status === "running",
    "bg-emerald-400":            status === "success",
    "bg-destructive":            status === "failed",
  })
  return (
    <span className={cls}>
      <span className={dot} />
      <span className="capitalize">{status}</span>
    </span>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function JobDetailPage() {
  const { id: projectId, jobId } = useParams({ from: "/_app/projects/$id/jobs/$jobId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const { data: job, isLoading, isError } = useQuery({
    queryKey: ["job", orgId, projectId, jobId],
    queryFn: () => jobsApi.get(orgId, projectId, jobId, token),
    enabled: !!orgId,
  })

  const { data: runs = [], isFetching: runsFetching } = useQuery({
    queryKey: ["job-runs", orgId, projectId, jobId],
    queryFn: () => jobsApi.listRuns(orgId, projectId, jobId, token),
    enabled: !!orgId,
    refetchInterval: job?.status === "running" || job?.status === "pending" ? 3000 : false,
  })

  const triggerMut = useMutation({
    mutationFn: () => jobsApi.trigger(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["job-runs", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
    },
  })

  const deleteMut = useMutation({
    mutationFn: () => jobsApi.delete(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({ to: "/projects/$id/jobs", params: { id: projectId } })
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading…</span>
      </div>
    )
  }

  if (isError || !job) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Job not found</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6 max-w-3xl">
      {/* ── Header ── */}
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <button
            onClick={() => navigate({ to: "/projects/$id/jobs", params: { id: projectId } })}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors mb-2"
          >
            <ChevronLeft className="h-3.5 w-3.5" /> Jobs
          </button>
          <div className="flex items-center gap-2.5">
            {job.is_cron
              ? <Clock className="h-4 w-4 text-muted-foreground/50" />
              : <Zap className="h-4 w-4 text-muted-foreground/50" />
            }
            <h1 className="text-base font-semibold">{job.name}</h1>
            <span className="text-xs text-muted-foreground/50 border border-border/40 px-1.5 py-px rounded font-mono">
              {job.is_cron ? "cron job" : "job"}
            </span>
            <StatusBadge status={job.status} />
          </div>
          <p className="text-xs text-muted-foreground/60 font-mono pl-6">{job.k8s_name}</p>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Button
            size="sm"
            onClick={() => triggerMut.mutate()}
            disabled={triggerMut.isPending}
            className="gap-1.5"
          >
            {triggerMut.isPending
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Play className="h-3.5 w-3.5" />
            }
            Run now
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => deleteMut.mutate()}
            disabled={deleteMut.isPending}
            className="text-muted-foreground hover:text-destructive gap-1.5"
          >
            {deleteMut.isPending
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Trash2 className="h-3.5 w-3.5" />
            }
            Delete
          </Button>
        </div>
      </div>

      {/* ── Config card ── */}
      <ConfigCard job={job} orgId={orgId} projectId={projectId} token={token} />

      {/* ── Run history ── */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Run history</h2>
          {runsFetching && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!runsFetching && <span className="text-xs text-muted-foreground">{runs.length}</span>}
        </div>

        {runs.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border/60 py-10 flex flex-col items-center gap-2">
            <p className="text-sm text-muted-foreground">No runs yet</p>
            <p className="text-xs text-muted-foreground/50">Click "Run now" to trigger the first run.</p>
          </div>
        ) : (
          <div className="rounded-lg border border-border/60 overflow-hidden">
            {runs.map((run, i) => (
              <RunRow key={run.id} run={run} last={i === runs.length - 1} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// ─── Config card ─────────────────────────────────────────────────────────────

function ConfigCard({
  job, orgId, projectId, token,
}: {
  job: ApiJob
  orgId: string
  projectId: string
  token: string
}) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<string | null>(null) // field name being edited
  const [draft, setDraft] = useState("")

  const updateMut = useMutation({
    mutationFn: (body: Record<string, string>) =>
      jobsApi.update(orgId, projectId, job.id, body, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, job.id] })
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      setEditing(null)
    },
  })

  function startEdit(field: string, current: string) {
    setEditing(field)
    setDraft(current)
  }

  function commitEdit(field: string) {
    if (!draft.trim()) return
    updateMut.mutate({ [field]: draft.trim() })
  }

  function EditableRow({ label, field, value, mono = false }: {
    label: string; field: string; value: string; mono?: boolean
  }) {
    const isMe = editing === field
    return (
      <div className="flex items-center justify-between py-2.5 border-b border-border/30 last:border-0 gap-4">
        <span className="text-xs text-muted-foreground/60 w-32 shrink-0">{label}</span>
        {isMe ? (
          <div className="flex items-center gap-1.5 flex-1 min-w-0">
            <input
              autoFocus
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitEdit(field)
                if (e.key === "Escape") setEditing(null)
              }}
              className={cn(inputCls, "h-7 text-xs flex-1", mono && "font-mono")}
            />
            <button
              onClick={() => commitEdit(field)}
              disabled={!draft.trim() || updateMut.isPending}
              className="text-muted-foreground hover:text-foreground disabled:opacity-40"
            >
              {updateMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
            </button>
            <button onClick={() => setEditing(null)} className="text-muted-foreground hover:text-foreground">
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-2 flex-1 min-w-0 justify-between">
            <span className={cn("text-xs truncate", mono ? "font-mono text-foreground" : "text-foreground/80")}>
              {value || <span className="text-muted-foreground/40 italic">not set</span>}
            </span>
            <button
              onClick={() => startEdit(field, value)}
              className="p-1 text-muted-foreground/30 hover:text-muted-foreground transition-colors shrink-0"
            >
              <Pencil className="h-3 w-3" />
            </button>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <div className="px-4 py-2.5 bg-muted/20 border-b border-border/40">
        <span className="text-xs font-medium text-muted-foreground">Configuration</span>
      </div>
      <div className="px-4">
        <EditableRow label="Image" field="image" value={job.image} mono />
        <EditableRow label="Command" field="command" value={job.command} mono />
        {job.is_cron && (
          <>
            <EditableRow label="Schedule" field="schedule" value={job.schedule} mono />
            <div className="flex items-center justify-between py-2.5 border-b border-border/30 gap-4">
              <span className="text-xs text-muted-foreground/60 w-32 shrink-0">Concurrency</span>
              <span className="text-xs text-foreground/80">{job.concurrency_policy}</span>
            </div>
          </>
        )}
        <div className="flex items-center justify-between py-2.5 border-b border-border/30 gap-4">
          <span className="text-xs text-muted-foreground/60 w-32 shrink-0">CPU</span>
          <span className="text-xs font-mono text-foreground/80">{job.cpu_request} / {job.cpu_limit}</span>
        </div>
        <div className="flex items-center justify-between py-2.5 gap-4">
          <span className="text-xs text-muted-foreground/60 w-32 shrink-0">Memory</span>
          <span className="text-xs font-mono text-foreground/80">{job.memory_request} / {job.memory_limit}</span>
        </div>
      </div>
    </div>
  )
}

// ─── Run row ─────────────────────────────────────────────────────────────────

function RunRow({ run, last }: { run: ApiJobRun; last: boolean }) {
  const [open, setOpen] = useState(false)

  const duration = run.started_at && run.finished_at
    ? Math.round((new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000)
    : null

  const dot = cn("h-2 w-2 rounded-full shrink-0", {
    "bg-muted-foreground/30": run.status === "idle",
    "bg-yellow-400 animate-pulse": run.status === "pending" || run.status === "running",
    "bg-emerald-400": run.status === "success",
    "bg-destructive": run.status === "failed",
  })

  return (
    <div className={cn(!last && "border-b border-border/30")}>
      <button
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-muted/10 transition-colors text-left"
        onClick={() => setOpen((v) => !v)}
      >
        <div className={dot} />
        <span className="text-xs capitalize text-muted-foreground/70 w-16 shrink-0">{run.status}</span>
        <span className="text-xs text-muted-foreground/50 flex-1">
          {run.started_at
            ? new Date(run.started_at).toLocaleString()
            : new Date(run.created_at).toLocaleString()
          }
        </span>
        {duration !== null && (
          <span className="text-xs text-muted-foreground/40 shrink-0">{duration}s</span>
        )}
        <ChevronRight className={cn("h-3.5 w-3.5 text-muted-foreground/30 transition-transform shrink-0", open && "rotate-90")} />
      </button>

      {open && (
        <div className="px-4 pb-3">
          {run.log ? (
            <pre className="text-xs font-mono text-muted-foreground/70 bg-muted/20 rounded-md px-3 py-2.5 overflow-x-auto whitespace-pre-wrap max-h-64 overflow-y-auto">
              {run.log}
            </pre>
          ) : (
            <p className="text-xs text-muted-foreground/40 italic px-1">No log output.</p>
          )}
        </div>
      )}
    </div>
  )
}
