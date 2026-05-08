import { createFileRoute, Link, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, Check, ChevronDown, ChevronRight, Clock, Loader2, Play, RotateCcw, ServerCrash, Trash2, Zap } from "lucide-react"
import CodeMirror from "@uiw/react-codemirror"
import { StreamLanguage } from "@codemirror/language"
import { shell } from "@codemirror/legacy-modes/mode/shell"
import { jobs as jobsApi, type ApiJob, type ApiJobRun, type CreateJobBody } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId")({
  component: JobDetailPage,
})

// ─── Status badge ─────────────────────────────────────────────────────────────

const STATUS_STYLES: Record<string, string> = {
  idle:    "bg-muted/30 text-muted-foreground border-border/40",
  pending: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  running: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  success: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  failed:  "bg-destructive/10 text-destructive border-destructive/20",
}

const STATUS_DOT: Record<string, string> = {
  idle:    "bg-muted-foreground/50",
  pending: "bg-amber-400 animate-pulse",
  running: "bg-amber-400 animate-pulse",
  success: "bg-emerald-400",
  failed:  "bg-destructive",
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
      <div className="flex items-center justify-center py-24 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading…</span>
      </div>
    )
  }

  if (isError || !job) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Job not found</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col min-h-full">
      {/* ── Sub-header ── */}
      <div className="border-b border-border/40 bg-muted/10">
        <div className="px-6 pt-4 pb-0">
          <Link
            to="/projects/$id/jobs"
            params={{ id: projectId }}
            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors mb-3"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to jobs
          </Link>

          <div className="flex items-start justify-between gap-4 mb-3">
            <div className="flex items-start gap-2.5">
              {job.is_cron
                ? <Clock className="h-4 w-4 text-muted-foreground/60 mt-0.5 shrink-0" />
                : <Zap className="h-4 w-4 text-muted-foreground/60 mt-0.5 shrink-0" />
              }
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{job.name}</span>
                  <span className="text-xs text-muted-foreground/50 border border-border/40 px-1.5 py-px rounded font-mono">
                    {job.is_cron ? "cron" : "job"}
                  </span>
                  <Badge className={cn("text-[10px] px-1.5 py-0 h-5 border gap-1", STATUS_STYLES[job.status] ?? STATUS_STYLES.idle)}>
                    <span className={cn("h-1.5 w-1.5 rounded-full", STATUS_DOT[job.status] ?? STATUS_DOT.idle)} />
                    {job.status}
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground/50 font-mono mt-0.5">{job.k8s_name}</p>
              </div>
            </div>

            <div className="flex items-center gap-2 shrink-0">
              <Button
                size="sm"
                onClick={() => triggerMut.mutate()}
                disabled={triggerMut.isPending}
                className="gap-1.5 h-7 text-xs"
              >
                {triggerMut.isPending
                  ? <Loader2 className="h-3 w-3 animate-spin" />
                  : <Play className="h-3 w-3" />
                }
                Run now
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => deleteMut.mutate()}
                disabled={deleteMut.isPending}
                className="text-muted-foreground hover:text-destructive gap-1.5 h-7 text-xs"
              >
                {deleteMut.isPending
                  ? <Loader2 className="h-3 w-3 animate-spin" />
                  : <Trash2 className="h-3 w-3" />
                }
                Delete
              </Button>
            </div>
          </div>

          {/* ── Tabs ── */}
          <nav className="flex items-center -mb-px">
            {(["runs", "config"] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={cn(
                  "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap",
                  activeTab === tab
                    ? "text-foreground border-foreground/25"
                    : "text-muted-foreground hover:text-foreground border-transparent hover:border-border/60"
                )}
              >
                {tab === "runs" ? "Runs" : "Configuration"}
                {tab === "runs" && runs.length > 0 && (
                  <span className="ml-1.5 text-muted-foreground/50">{runs.length}</span>
                )}
              </button>
            ))}
          </nav>
        </div>
      </div>

      {/* ── Tab content ── */}
      {activeTab === "runs" ? (
        <div className="p-6">
          <RunsTab runs={runs} isFetching={runsFetching} orgId={orgId} projectId={projectId} jobId={jobId} token={token} />
        </div>
      ) : (
        <div className="p-6 max-w-2xl space-y-8">
          <ConfigTab
            key={job.updated_at}
            job={job}
            orgId={orgId}
            projectId={projectId}
            token={token}
          />
        </div>
      )}
    </div>
  )
}

// ─── Runs tab ─────────────────────────────────────────────────────────────────

const RUN_STATUS_TEXT: Record<string, string> = {
  idle:    "text-muted-foreground",
  pending: "text-amber-400",
  running: "text-amber-400",
  success: "text-emerald-400",
  failed:  "text-destructive",
}

function RunsTab({
  runs, isFetching, orgId, projectId, jobId, token,
}: {
  runs: ApiJobRun[]
  isFetching: boolean
  orgId: string
  projectId: string
  jobId: string
  token: string
}) {
  if (isFetching && runs.length === 0) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (runs.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
        <Zap className="h-7 w-7 text-muted-foreground/30" />
        <div className="text-center">
          <p className="text-sm text-muted-foreground">No runs yet</p>
          <p className="text-xs text-muted-foreground/60 mt-0.5">Click "Run now" to trigger the first run.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
      {runs.map((run) => (
        <RunRow key={run.id} run={run} orgId={orgId} projectId={projectId} jobId={jobId} token={token} />
      ))}
    </div>
  )
}

// ─── Config tab ───────────────────────────────────────────────────────────────

const CRON_PRESETS = [
  { label: "Every 5 min", value: "*/5 * * * *" },
  { label: "Hourly",      value: "0 * * * *"   },
  { label: "Daily",       value: "0 0 * * *"   },
  { label: "Weekly",      value: "0 0 * * 0"   },
  { label: "Monthly",     value: "0 0 1 * *"   },
]

const CONCURRENCY_OPTIONS = [
  { value: "allow",   label: "Allow — multiple runs can overlap" },
  { value: "forbid",  label: "Forbid — skip if already running"  },
  { value: "replace", label: "Replace — cancel running, start new" },
]

const shellExtension = StreamLanguage.define(shell)

function ConfigTab({
  job, orgId, projectId, token,
}: { job: ApiJob; orgId: string; projectId: string; token: string }) {
  const qc = useQueryClient()

  const [image, setImage]             = useState(job.image)
  const [command, setCommand]         = useState(job.command)
  const [cpuRequest, setCpuRequest]   = useState(job.cpu_request)
  const [cpuLimit, setCpuLimit]       = useState(job.cpu_limit)
  const [memRequest, setMemRequest]   = useState(job.memory_request)
  const [memLimit, setMemLimit]       = useState(job.memory_limit)
  const [isCron, setIsCron]           = useState(job.is_cron)
  const [schedule, setSchedule]       = useState(job.schedule ?? "")
  const [concurrency, setConcurrency] = useState(job.concurrency_policy ?? "allow")
  const [historyLimit, setHistoryLimit] = useState(String(job.history_limit ?? 5))
  const [envVars, setEnvVars]         = useState(job.env_vars)
  const [saved, setSaved]             = useState(false)

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
      is_cron:        isCron,
      image,
      command,
      cpu_request:    cpuRequest,
      cpu_limit:      cpuLimit,
      memory_request: memRequest,
      memory_limit:   memLimit,
      env_vars:       envVars || undefined,
    }
    if (isCron) {
      body.schedule           = schedule
      body.concurrency_policy = concurrency
      body.history_limit      = parseInt(historyLimit, 10) || 5
    }
    updateMut.mutate(body)
  }

  return (
    <div className="space-y-8">
      {/* Container */}
      <Section title="Container" subtitle="Image and script to execute">
        <Field label="Image" required>
          <input
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="alpine:latest"
            className={cn(inputCls, "font-mono text-xs")}
          />
        </Field>
        <Field label="Script">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={command}
              height="200px"
              theme="dark"
              extensions={[shellExtension]}
              onChange={(val) => setCommand(val)}
              placeholder={"#!/bin/sh\n\necho 'Hello World'"}
              style={{ fontSize: 12 }}
              basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
            />
          </div>
          <p className="text-xs text-muted-foreground/40">
            Executed via <code className="font-mono">sh -c</code>. Use a shebang to select a different runtime.
          </p>
        </Field>
      </Section>

      {/* Scheduling */}
      <div className="rounded-lg border border-border/40 overflow-hidden">
        <button
          type="button"
          onClick={() => setIsCron((v) => !v)}
          className="w-full flex items-center justify-between px-4 py-3 hover:bg-muted/20 transition-colors"
        >
          <div className="text-left">
            <p className="text-sm font-medium text-foreground">Run on a schedule</p>
            <p className="text-xs text-muted-foreground mt-0.5">Repeat this job on a cron expression</p>
          </div>
          <div className={cn("w-9 h-5 rounded-full transition-colors relative shrink-0", isCron ? "bg-primary" : "bg-muted")}>
            <div className={cn("absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform", isCron ? "translate-x-4" : "translate-x-0.5")} />
          </div>
        </button>

        {isCron && (
          <div className="border-t border-border/40 px-4 pb-4 pt-4 space-y-4">
            <Field label="Cron expression" required>
              <div className="flex flex-wrap gap-1.5 mb-2">
                {CRON_PRESETS.map((p) => (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => setSchedule(p.value)}
                    className={cn(
                      "px-2.5 py-1 text-xs rounded-md border transition-colors",
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
                className={cn(inputCls, "font-mono text-xs")}
              />
              <p className="text-xs text-muted-foreground/40">5-field cron: minute hour day month weekday</p>
            </Field>

            <div className="grid grid-cols-2 gap-4">
              <Field label="Concurrency policy">
                <select
                  value={concurrency}
                  onChange={(e) => setConcurrency(e.target.value)}
                  className={cn(inputCls, "text-xs")}
                >
                  {CONCURRENCY_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </Field>
              <Field label="History limit">
                <input
                  type="number"
                  min={1}
                  max={50}
                  value={historyLimit}
                  onChange={(e) => setHistoryLimit(e.target.value)}
                  className={cn(inputCls, "text-xs")}
                />
              </Field>
            </div>
          </div>
        )}
      </div>

      {/* Resources */}
      <Section title="Resources" subtitle="CPU and memory requests and limits">
        <div className="grid grid-cols-2 gap-4">
          <Field label="CPU request">
            <input value={cpuRequest} onChange={(e) => setCpuRequest(e.target.value)} placeholder="100m" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
          <Field label="CPU limit">
            <input value={cpuLimit} onChange={(e) => setCpuLimit(e.target.value)} placeholder="500m" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
          <Field label="Memory request">
            <input value={memRequest} onChange={(e) => setMemRequest(e.target.value)} placeholder="128Mi" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
          <Field label="Memory limit">
            <input value={memLimit} onChange={(e) => setMemLimit(e.target.value)} placeholder="512Mi" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
        </div>
      </Section>

      {/* Environment */}
      <Section title="Environment" subtitle="Variables injected at runtime. One KEY=VALUE per line.">
        <Field label="Env vars">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={envVars}
              height="140px"
              theme="dark"
              onChange={(val) => setEnvVars(val)}
              placeholder={"DATABASE_URL=postgres://...\nAPI_KEY=secret"}
              style={{ fontSize: 12 }}
              basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
            />
          </div>
        </Field>
      </Section>

      {/* Save */}
      <div className="flex items-center gap-3">
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

        {/* Left: status + timestamp + duration */}
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

        {/* Right: actions + expand */}
        <div className="flex items-center gap-0.5 shrink-0">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => rerunMut.mutate()}
            disabled={rerunMut.isPending}
            title="Re-run"
            className="text-muted-foreground/50 hover:text-foreground"
          >
            {rerunMut.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => deleteMut.mutate()}
            disabled={deleteMut.isPending}
            title="Delete record"
            className="text-muted-foreground/50 hover:text-destructive"
          >
            {deleteMut.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setOpen((v) => !v)}
            title="View log"
            className="text-muted-foreground/50 hover:text-foreground"
          >
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
