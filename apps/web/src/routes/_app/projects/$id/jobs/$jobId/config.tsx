import { ConfigSaveBar, useConfigDraft, useConfigSave } from "@/components/layout/config-save-bar"
import { GroupAttachments } from "@/components/variables/group-attachments"
import { FormLayout } from "@/components/layout/form-layout"
import { ResourceIntro } from "@/components/layout/resource-workbench"
import { createFileRoute, useParams } from "@tanstack/react-router"
import { useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import CodeMirror from "@uiw/react-codemirror"
import { envLanguage, envTheme } from "@/lib/env-lang"
import { StreamLanguage } from "@codemirror/language"
import { shell } from "@codemirror/legacy-modes/mode/shell"
import { jobs as jobsApi, type ApiJob, type CreateJobBody } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
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
    <ConfigSaveBar key={job.id}><div className="console-page pb-24"><ResourceIntro title="Execution configuration" description="Define what runs, when it runs, and the resources available to it." /><FormLayout><div className="space-y-6">
      <ConfigForm job={job} orgId={orgId} projectId={projectId} token={token} />
    </div></FormLayout></div></ConfigSaveBar>
  )
}

function ConfigForm({ job, orgId, projectId, token }: { job: ApiJob; orgId: string; projectId: string; token: string }) {
  const qc = useQueryClient()

  // One draft for the page: the save bar reads its dirty state, and a refetch
  // while you are typing syncs only when you have nothing unsaved.
  const draft = useConfigDraft({
    image: job.image,
    command: job.command,
    cpuRequest: job.cpu_request,
    cpuLimit: job.cpu_limit,
    memRequest: job.memory_request,
    memLimit: job.memory_limit,
    isCron: job.is_cron,
    schedule: job.schedule ?? "",
    concurrency: job.concurrency_policy ?? "allow",
    historyLimit: String(job.history_limit ?? 5),
    envVars: job.env_vars,
  })
  const { value: form } = draft
  const patch = (p: Partial<typeof form>) => draft.setValue((v) => ({ ...v, ...p }))

  useEffect(() => {
    draft.sync({
      image: job.image,
      command: job.command,
      cpuRequest: job.cpu_request,
      cpuLimit: job.cpu_limit,
      memRequest: job.memory_request,
      memLimit: job.memory_limit,
      isCron: job.is_cron,
      schedule: job.schedule ?? "",
      concurrency: job.concurrency_policy ?? "allow",
      historyLimit: String(job.history_limit ?? 5),
      envVars: job.env_vars,
    })
  }, [job])

  const updateMut = useMutation({
    mutationFn: () => {
      const body: Partial<CreateJobBody> = {
        is_cron:        form.isCron,
        image:          form.image,
        command:        form.command,
        cpu_request:    form.cpuRequest,
        cpu_limit:      form.cpuLimit,
        memory_request: form.memRequest,
        memory_limit:   form.memLimit,
        env_vars:       form.envVars || undefined,
      }
      if (form.isCron) {
        body.schedule           = form.schedule
        body.concurrency_policy = form.concurrency
        body.history_limit      = parseInt(form.historyLimit, 10) || 5
      }
      return jobsApi.update(orgId, projectId, job.id, body, token)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, job.id] })
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
    },
  })

  useConfigSave("Execution configuration", draft, () => updateMut.mutateAsync())

  return (
    <div className="space-y-8">
      <Section title="Environment" subtitle="Variables injected at runtime. One KEY=VALUE per line.">
        <Field label="Env vars">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={form.envVars}
              height="140px"
              theme="dark"
              extensions={[envLanguage, envTheme]}
              onChange={(val) => patch({ envVars: val })}
              placeholder={"DATABASE_URL=postgres://...\nAPI_KEY=secret"}
              style={{ fontSize: 12 }}
              basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
            />
          </div>
        </Field>
      </Section>

      <GroupAttachments owner={{ kind: "job", id: job.id }} projectId={projectId} />

      <Section title="Container" subtitle="Image and script to execute">
        <Field label="Image" required>
          <input
            value={form.image}
            onChange={(e) => patch({ image: e.target.value })}
            placeholder="alpine:latest"
            className={cn(inputCls, "font-mono text-xs")}
          />
        </Field>
        <Field label="Script">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={form.command}
              height="200px"
              theme="dark"
              extensions={[shellExtension]}
              onChange={(val) => patch({ command: val })}
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
        enabled={form.isCron}
        onToggle={() => patch({ isCron: !form.isCron })}
        schedule={form.schedule}
        onScheduleChange={(v) => patch({ schedule: v })}
        concurrency={form.concurrency}
        onConcurrencyChange={(v) => patch({ concurrency: v })}
        historyLimit={form.historyLimit}
        onHistoryLimitChange={(v) => patch({ historyLimit: v })}
      />

      <Section title="Resources" subtitle="CPU and memory requests and limits">
        <div className="grid grid-cols-2 gap-4">
          <Field label="CPU request">
            <input value={form.cpuRequest} onChange={(e) => patch({ cpuRequest: e.target.value })} placeholder="100m" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
          <Field label="CPU limit">
            <input value={form.cpuLimit} onChange={(e) => patch({ cpuLimit: e.target.value })} placeholder="500m" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
          <Field label="Memory request">
            <input value={form.memRequest} onChange={(e) => patch({ memRequest: e.target.value })} placeholder="128Mi" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
          <Field label="Memory limit">
            <input value={form.memLimit} onChange={(e) => patch({ memLimit: e.target.value })} placeholder="512Mi" className={cn(inputCls, "font-mono text-xs")} />
          </Field>
        </div>
      </Section>


      {updateMut.isError && (
        <p className="text-xs text-destructive">{(updateMut.error as Error).message}</p>
      )}
    </div>
  )
}
