import { createFileRoute, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Loader2 } from "lucide-react"
import CodeMirror from "@uiw/react-codemirror"
import { envLanguage, envTheme } from "@/lib/env-lang"
import { StreamLanguage } from "@codemirror/language"
import { shell } from "@codemirror/legacy-modes/mode/shell"
import { jobs as jobsApi, type ApiJob, type CreateJobBody } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { CronScheduleBlock } from "@/components/jobs/cron-schedule-block"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId/config")({
  component: ConfigPage,
})

const shellExtension = StreamLanguage.define(shell)

function ConfigPage() {
  const { id: projectId, jobId } = useParams({ from: "/_app/projects/$id/jobs/$jobId/config" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: job } = useQuery({
    queryKey: ["job", orgId, projectId, jobId],
    queryFn: () => jobsApi.get(orgId, projectId, jobId, token),
    enabled: !!orgId,
  })

  if (!job) return null

  return (
    <div className="p-6 max-w-2xl space-y-8">
      <ConfigForm key={job.updated_at} job={job} orgId={orgId} projectId={projectId} token={token} />
    </div>
  )
}

function ConfigForm({ job, orgId, projectId, token }: { job: ApiJob; orgId: string; projectId: string; token: string }) {
  const qc = useQueryClient()

  const [image, setImage]               = useState(job.image)
  const [command, setCommand]           = useState(job.command)
  const [cpuRequest, setCpuRequest]     = useState(job.cpu_request)
  const [cpuLimit, setCpuLimit]         = useState(job.cpu_limit)
  const [memRequest, setMemRequest]     = useState(job.memory_request)
  const [memLimit, setMemLimit]         = useState(job.memory_limit)
  const [isCron, setIsCron]             = useState(job.is_cron)
  const [schedule, setSchedule]         = useState(job.schedule ?? "")
  const [concurrency, setConcurrency]   = useState(job.concurrency_policy ?? "allow")
  const [historyLimit, setHistoryLimit] = useState(String(job.history_limit ?? 5))
  const [envVars, setEnvVars]           = useState(job.env_vars)
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

      <CronScheduleBlock
        enabled={isCron}
        onToggle={() => setIsCron((v) => !v)}
        schedule={schedule}
        onScheduleChange={setSchedule}
        concurrency={concurrency}
        onConcurrencyChange={setConcurrency}
        historyLimit={historyLimit}
        onHistoryLimitChange={setHistoryLimit}
      />

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

      <Section title="Environment" subtitle="Variables injected at runtime. One KEY=VALUE per line.">
        <Field label="Env vars">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={envVars}
              height="140px"
              theme="dark"
              extensions={[envLanguage, envTheme]}
              onChange={(val) => setEnvVars(val)}
              placeholder={"DATABASE_URL=postgres://...\nAPI_KEY=secret"}
              style={{ fontSize: 12 }}
              basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
            />
          </div>
        </Field>
      </Section>

      <div className="flex items-center gap-3">
        <Button onClick={handleSave} disabled={updateMut.isPending} size="sm" className="gap-1.5">
          {updateMut.isPending
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : saved ? <Check className="h-3.5 w-3.5" /> : null
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
