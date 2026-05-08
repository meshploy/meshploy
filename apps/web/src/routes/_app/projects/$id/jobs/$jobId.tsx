import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useState, useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  ArrowLeft,
  Check,
  ChevronRight,
  Clock,
  Loader2,
  Play,
  ServerCrash,
  Trash2,
  Zap,
} from "lucide-react"
import {
  jobs as jobsApi,
  type ApiJob,
  type ApiJobRun,
  type CreateJobBody,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"
import { useNavigate } from "@tanstack/react-router"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId")({
  component: JobDetailPage,
})

// ─── Status badge ─────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const cls = cn("inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full border", {
    "border-border/40 text-muted-foreground":                       status === "idle",
    "border-yellow-500/30 text-yellow-400 bg-yellow-400/10":        status === "pending" || status === "running",
    "border-emerald-500/30 text-emerald-400 bg-emerald-400/10":     status === "success",
    "border-destructive/30 text-destructive bg-destructive/10":     status === "failed",
  })
  const dot = cn("h-1.5 w-1.5 rounded-full", {
    "bg-muted-foreground/50":         status === "idle",
    "bg-yellow-400 animate-pulse":    status === "pending" || status === "running",
    "bg-emerald-400":                 status === "success",
    "bg-destructive":                 status === "failed",
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
  const [activeTab, setActiveTab] = useState<"runs" | "config">("runs")

  const { data: job, isLoading, isError } = useQuery({
    queryKey: ["job", orgId, projectId, jobId],
    queryFn: () => jobsApi.get(orgId, projectId, jobId, token),
    enabled: !!orgId,
  })

  const { data: runs = [], isFetching: runsFetching } = useQuery({
    queryKey: ["job-runs", orgId, projectId, jobId],
    queryFn: () => jobsApi.listRuns(orgId, projectId, jobId, token),
    enabled: !!orgId && activeTab === "runs",
    refetchInterval: job?.status === "running" || job?.status === "pending" ? 3000 : false,
  })

  const triggerMut = useMutation({
    mutationFn: () => jobsApi.trigger(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["job-runs", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      setActiveTab("runs")
    },
  })

  const deleteMut = useMutation({
    mutationFn: () => jobsApi.delete(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
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
    <div className="p-6 space-y-5 max-w-3xl">
      {/* ── Header ── */}
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <Link
            to="/projects/$id/jobs"
            params={{ id: projectId }}
            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors mb-2"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to jobs
          </Link>
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

      {/* ── Tabs ── */}
      <div className="flex items-center gap-0 border-b border-border/40 -mb-2">
        {(["runs", "config"] as const).map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={cn(
              "px-4 py-2.5 text-sm border-b-2 -mb-px transition-colors",
              activeTab === tab
                ? "border-foreground text-foreground font-medium"
                : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            {tab === "runs" ? "Runs" : "Configuration"}
            {tab === "runs" && runs.length > 0 && (
              <span className="ml-1.5 text-xs text-muted-foreground/50">{runs.length}</span>
            )}
          </button>
        ))}
      </div>

      {/* ── Tab content ── */}
      {activeTab === "runs" ? (
        <RunsTab runs={runs} isFetching={runsFetching} />
      ) : (
        <ConfigTab
          key={job.updated_at}
          job={job}
          orgId={orgId}
          projectId={projectId}
          token={token}
        />
      )}
    </div>
  )
}

// ─── Runs tab ─────────────────────────────────────────────────────────────────

function RunsTab({ runs, isFetching }: { runs: ApiJobRun[]; isFetching: boolean }) {
  return (
    <div className="space-y-3">
      {isFetching && runs.length === 0 && (
        <div className="flex items-center justify-center h-24 gap-2 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
        </div>
      )}
      {!isFetching && runs.length === 0 && (
        <div className="rounded-lg border border-dashed border-border/60 py-12 flex flex-col items-center gap-2">
          <p className="text-sm text-muted-foreground">No runs yet</p>
          <p className="text-xs text-muted-foreground/50">Click "Run now" to trigger the first run.</p>
        </div>
      )}
      {runs.length > 0 && (
        <div className="rounded-lg border border-border/60 overflow-hidden">
          {runs.map((run, i) => (
            <RunRow key={run.id} run={run} last={i === runs.length - 1} />
          ))}
        </div>
      )}
    </div>
  )
}

// ─── Config tab ───────────────────────────────────────────────────────────────

const CRON_PRESETS = [
  { label: "Every 5 min", value: "*/5 * * * *" },
  { label: "Hourly",      value: "0 * * * *" },
  { label: "Daily",       value: "0 0 * * *" },
  { label: "Weekly",      value: "0 0 * * 0" },
  { label: "Monthly",     value: "0 0 1 * *" },
]

const CONCURRENCY_OPTIONS = [
  { value: "allow",   label: "Allow" },
  { value: "forbid",  label: "Forbid" },
  { value: "replace", label: "Replace" },
]

function ConfigTab({
  job, orgId, projectId, token,
}: { job: ApiJob; orgId: string; projectId: string; token: string }) {
  const qc = useQueryClient()

  const [image, setImage]               = useState(job.image)
  const [command, setCommand]           = useState(job.command)
  const [cpuRequest, setCpuRequest]     = useState(job.cpu_request)
  const [cpuLimit, setCpuLimit]         = useState(job.cpu_limit)
  const [memRequest, setMemRequest]     = useState(job.memory_request)
  const [memLimit, setMemLimit]         = useState(job.memory_limit)
  const [envVars, setEnvVars]           = useState<string>((job as ApiJob & { env_vars?: string }).env_vars ?? "")
  const [schedule, setSchedule]         = useState(job.schedule ?? "")
  const [concurrency, setConcurrency]   = useState(job.concurrency_policy ?? "allow")
  const [historyLimit, setHistoryLimit] = useState(String(job.history_limit ?? 5))
  const [saved, setSaved]               = useState(false)

  const updateMut = useMutation({
    mutationFn: (body: Partial<CreateJobBody>) =>
      jobsApi.update(orgId, projectId, job.id, body, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, job.id] })
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    },
  })

  function handleSave() {
    const body: Partial<CreateJobBody> = {
      image,
      command,
      cpu_request: cpuRequest,
      cpu_limit: cpuLimit,
      memory_request: memRequest,
      memory_limit: memLimit,
    }
    if (envVars) body.env_vars = envVars
    if (job.is_cron) {
      body.schedule           = schedule
      body.concurrency_policy = concurrency
      body.history_limit      = parseInt(historyLimit, 10) || 5
    }
    updateMut.mutate(body)
  }

  const labelCls = "text-xs text-muted-foreground/70 mb-1.5 block"
  const sectionCls = "space-y-4"

  return (
    <div className="space-y-6">
      {/* Container */}
      <section className={sectionCls}>
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Container</h3>
        <div>
          <label className={labelCls}>Image</label>
          <input
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="alpine:latest"
            className={cn(inputCls, "text-xs font-mono")}
          />
        </div>
        <div>
          <label className={labelCls}>Script</label>
          <textarea
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            rows={8}
            placeholder={"#!/bin/sh\n\n# Your script here\necho 'Hello World'"}
            spellCheck={false}
            className={cn(
              inputCls,
              "text-xs font-mono resize-y min-h-[120px] leading-relaxed whitespace-pre"
            )}
          />
          <p className="text-xs text-muted-foreground/40 mt-1">
            Executed via <code className="font-mono">sh -c</code>. Use a shebang for other runtimes.
          </p>
        </div>
      </section>

      {/* Scheduling (cron only) */}
      {job.is_cron && (
        <section className={sectionCls}>
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Scheduling</h3>
          <div>
            <label className={labelCls}>Cron expression</label>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {CRON_PRESETS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => setSchedule(p.value)}
                  className={cn(
                    "px-2.5 py-1 text-xs rounded border transition-colors",
                    schedule === p.value
                      ? "border-foreground/40 bg-foreground/10 text-foreground"
                      : "border-border/40 text-muted-foreground hover:text-foreground hover:border-foreground/30"
                  )}
                >
                  {p.label}
                </button>
              ))}
            </div>
            <input
              value={schedule}
              onChange={(e) => setSchedule(e.target.value)}
              placeholder="*/5 * * * *"
              className={cn(inputCls, "text-xs font-mono")}
            />
            <p className="text-xs text-muted-foreground/40 mt-1">
              Standard 5-field cron expression (minute hour day month weekday).
            </p>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls}>Concurrency policy</label>
              <select
                value={concurrency}
                onChange={(e) => setConcurrency(e.target.value)}
                className={cn(inputCls, "text-xs")}
              >
                {CONCURRENCY_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
            </div>
            <div>
              <label className={labelCls}>History limit</label>
              <input
                type="number"
                min={1}
                max={50}
                value={historyLimit}
                onChange={(e) => setHistoryLimit(e.target.value)}
                className={cn(inputCls, "text-xs")}
              />
            </div>
          </div>
        </section>
      )}

      {/* Resources */}
      <section className={sectionCls}>
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Resources</h3>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className={labelCls}>CPU request</label>
            <input value={cpuRequest} onChange={(e) => setCpuRequest(e.target.value)} placeholder="100m" className={cn(inputCls, "text-xs font-mono")} />
          </div>
          <div>
            <label className={labelCls}>CPU limit</label>
            <input value={cpuLimit} onChange={(e) => setCpuLimit(e.target.value)} placeholder="500m" className={cn(inputCls, "text-xs font-mono")} />
          </div>
          <div>
            <label className={labelCls}>Memory request</label>
            <input value={memRequest} onChange={(e) => setMemRequest(e.target.value)} placeholder="128Mi" className={cn(inputCls, "text-xs font-mono")} />
          </div>
          <div>
            <label className={labelCls}>Memory limit</label>
            <input value={memLimit} onChange={(e) => setMemLimit(e.target.value)} placeholder="512Mi" className={cn(inputCls, "text-xs font-mono")} />
          </div>
        </div>
      </section>

      {/* Environment */}
      <section className={sectionCls}>
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Environment</h3>
        <div>
          <label className={labelCls}>Variables</label>
          <textarea
            value={envVars}
            onChange={(e) => setEnvVars(e.target.value)}
            rows={5}
            placeholder={"DATABASE_URL=postgres://...\nAPI_KEY=secret"}
            spellCheck={false}
            className={cn(inputCls, "text-xs font-mono resize-y min-h-[80px] leading-relaxed")}
          />
          <p className="text-xs text-muted-foreground/40 mt-1">One <code className="font-mono">KEY=VALUE</code> per line.</p>
        </div>
      </section>

      {/* Save */}
      <div className="flex items-center gap-3 pt-1">
        <Button onClick={handleSave} disabled={updateMut.isPending} size="sm" className="gap-1.5">
          {updateMut.isPending
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : saved
              ? <Check className="h-3.5 w-3.5" />
              : null
          }
          {saved ? "Saved" : "Save changes"}
        </Button>
        {updateMut.isError && (
          <p className="text-xs text-destructive">Failed to save. Please try again.</p>
        )}
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
    "bg-muted-foreground/30":      run.status === "idle",
    "bg-yellow-400 animate-pulse": run.status === "pending" || run.status === "running",
    "bg-emerald-400":              run.status === "success",
    "bg-destructive":              run.status === "failed",
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
