import { createFileRoute, useParams } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import { useState, useEffect } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Info, KeyRound, Loader2, Plus, Save, Server, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import {
  services as servicesApi,
  buildConfigs as buildConfigsApi,
  gitIntegrations as gitApi,
  nodes as nodesApi,
  registries as registryApi,
  secrets as secretsApi,
  toNode,
  type ApiNode,
  type ApiSecretAttachment,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Section, Field, NodeCard } from "@/components/services/form-primitives"
import { Input } from "@/components/ui/input"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/config"
)({
  component: ConfigTab,
})

// ─── Env Vars section ─────────────────────────────────────────────────────────

function EnvVarsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const [envVars, setEnvVars] = useState("")

  const { data, isLoading } = useQuery({
    queryKey: ["service-env-vars", orgId, projectId, serviceId],
    queryFn: () => servicesApi.getEnvVars(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  useEffect(() => {
    if (data !== undefined) setEnvVars(data.env_vars)
  }, [data])

  const mutation = useMutation({
    mutationFn: () => servicesApi.update(orgId, projectId, serviceId, { env_vars: envVars }, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["service-env-vars", orgId, projectId, serviceId] })
    },
  })

  return (
    <Section
      title="Environment variables"
      subtitle="One KEY=VALUE pair per line. Values are AES-256 encrypted at rest."
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-4">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <Textarea
          value={envVars}
          onChange={(e) => setEnvVars(e.target.value)}
          placeholder={"DATABASE_URL=postgres://...\nSECRET_KEY=..."}
          className="font-mono text-xs min-h-[160px] resize-y bg-muted/20 border-border/60"
        />
      )}
      {mutation.isError && (
        <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>
      )}
      <div className="flex justify-end">
        <Button size="sm" className="gap-1.5" onClick={() => mutation.mutate()} disabled={mutation.isPending || isLoading}>
          {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save
        </Button>
      </div>
    </Section>
  )
}

// ─── Secrets section ──────────────────────────────────────────────────────────

function SecretsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const [showAdd, setShowAdd] = useState(false)
  const [selectedSecretId, setSelectedSecretId] = useState("")
  const [envKey, setEnvKey] = useState("")

  const { data: attachments = [], isLoading } = useQuery({
    queryKey: ["secret-attachments", orgId, projectId, serviceId],
    queryFn: () => secretsApi.listAttachments(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: projectSecrets = [] } = useQuery({
    queryKey: ["secrets", orgId, projectId],
    queryFn: () => secretsApi.list(orgId, projectId, token),
    enabled: !!orgId && showAdd,
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ["secret-attachments", orgId, projectId, serviceId] })

  const attachMut = useMutation({
    mutationFn: () => secretsApi.attach(orgId, projectId, serviceId, { secret_id: selectedSecretId, env_key: envKey.trim() }, token),
    onSuccess: () => { setShowAdd(false); setSelectedSecretId(""); setEnvKey(""); invalidate() },
  })

  const detachMut = useMutation({
    mutationFn: (attachmentId: string) => secretsApi.detach(orgId, projectId, serviceId, attachmentId, token),
    onSuccess: invalidate,
  })

  const attachedIds = new Set(attachments.map((a) => a.secret_id))
  const availableSecrets = projectSecrets.filter((s) => !attachedIds.has(s.id))

  return (
    <Section
      title="Secrets"
      subtitle="Inject project secrets as environment variables at runtime."
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <div className="space-y-2">
          {attachments.length > 0 && (
            <div className="rounded-lg border border-border/60 overflow-hidden">
              {attachments.map((a, i) => (
                <AttachmentRow
                  key={a.id}
                  attachment={a}
                  last={i === attachments.length - 1}
                  onDetach={() => detachMut.mutate(a.id)}
                  isDetaching={detachMut.isPending && detachMut.variables === a.id}
                />
              ))}
            </div>
          )}

          {showAdd ? (
            <div className="rounded-lg border border-border/60 bg-card p-3 space-y-3">
              <div className="grid grid-cols-2 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-muted-foreground">Secret</label>
                  <Select value={selectedSecretId} onValueChange={(v) => {
                    setSelectedSecretId(v ?? "")
                    const s = projectSecrets.find((ps) => ps.id === v)
                    if (s && !envKey) setEnvKey(s.name)
                  }}>
                    <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
                      <SelectValue placeholder={availableSecrets.length === 0 ? "No secrets available" : "Select a secret…"} />
                    </SelectTrigger>
                    <SelectContent>
                      {availableSecrets.map((s) => (
                        <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-muted-foreground">Env key</label>
                  <input
                    className={inputCls}
                    placeholder="ENV_VAR_NAME"
                    value={envKey}
                    onChange={(e) => setEnvKey(e.target.value)}
                  />
                </div>
              </div>
              <div className="flex gap-2">
                <button
                  onClick={() => attachMut.mutate()}
                  disabled={!selectedSecretId || !envKey.trim() || attachMut.isPending}
                  className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md bg-primary text-primary-foreground disabled:opacity-40 transition-opacity"
                >
                  {attachMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
                  Attach
                </button>
                <button
                  onClick={() => { setShowAdd(false); setSelectedSecretId(""); setEnvKey("") }}
                  className="flex items-center gap-1 text-xs px-3 py-1.5 rounded-md text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-3 w-3" /> Cancel
                </button>
              </div>
              {attachMut.isError && (
                <p className="text-xs text-destructive">{(attachMut.error as Error).message}</p>
              )}
            </div>
          ) : (
            <button
              onClick={() => setShowAdd(true)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <Plus className="h-3.5 w-3.5" /> Attach secret
            </button>
          )}
        </div>
      )}

      {/* Priority note */}
      <div className="flex items-start gap-2 text-xs text-muted-foreground/60 bg-muted/20 rounded-md px-3 py-2">
        <Info className="h-3.5 w-3.5 shrink-0 mt-0.5" />
        <span>Inline env vars take priority — if a secret key matches a variable defined above, the inline value wins.</span>
      </div>
    </Section>
  )
}

function AttachmentRow({
  attachment, last, onDetach, isDetaching,
}: {
  attachment: ApiSecretAttachment
  last: boolean
  onDetach: () => void
  isDetaching: boolean
}) {
  return (
    <div className={cn("flex items-center gap-3 px-3 py-2.5", !last && "border-b border-border/40")}>
      <KeyRound className="h-3 w-3 text-muted-foreground/40 shrink-0" />
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <code className="text-xs font-mono text-muted-foreground truncate">{attachment.secret_name}</code>
        <span className="text-muted-foreground/30 text-xs">→</span>
        <code className="text-xs font-mono text-foreground truncate">{attachment.env_key}</code>
      </div>
      <button
        onClick={onDetach}
        disabled={isDetaching}
        className="text-muted-foreground/30 hover:text-destructive transition-colors disabled:opacity-40 shrink-0"
        title="Detach"
      >
        {isDetaching ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
      </button>
    </div>
  )
}

// ─── Source + Deploy section ──────────────────────────────────────────────────

function SourceDeploySection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()

  // ── Data queries ──────────────────────────────────────────────────────────
  const { data: service, isLoading: svcLoading } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: bc, isLoading: bcLoading } = useQuery({
    queryKey: ["build-config", orgId, projectId, serviceId],
    queryFn: () => buildConfigsApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
    retry: false,
  })

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const workerNodes = rawNodes
    .filter((n) => n.k8s_member && n.status === "online" && n.k3s_role === "agent")
    .map(toNode)
  const builderNodes = rawNodes.filter(
    (n) => n.k8s_member && n.status === "online" && n.k3s_labels?.["meshploy.com/role"] === "builder"
  )

  const { data: gitList = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token),
    enabled: !!orgId,
  })
  const connectedGit = gitList.filter((g) => g.connected)

  const { data: registryList = [] } = useQuery({
    queryKey: ["registry-integrations", orgId],
    queryFn: () => registryApi.list(orgId, token),
    enabled: !!orgId,
  })

  // ── Form state ────────────────────────────────────────────────────────────
  const [form, setForm] = useState({
    source: "git" as "git" | "image",
    image: "",
    gitIntegrationId: "",
    gitRepo: "",
    gitBranch: "main",
    builder: "nixpacks" as "nixpacks" | "railpack" | "dockerfile",
    dockerfilePath: "Dockerfile",
    registryIntegrationId: "",
    builderNodeName: "" as string,
    builderCPURequest: "1000m",
    builderMemoryRequest: "1Gi",
    nodeId: "",
    port: 3000,
    replicas: 1,
    cpuRequest: "100m",
    cpuLimit: "500m",
    memoryRequest: "128Mi",
    memoryLimit: "512Mi",
    showResources: false,
  })
  const patch = (p: Partial<typeof form>) => setForm((f) => ({ ...f, ...p }))

  useEffect(() => {
    if (!service) return
    const isGit = !!bc?.git_repo
    patch({
      source: isGit ? "git" : "image",
      image: service.image ?? "",
      gitIntegrationId: bc?.git_integration_id ?? "",
      gitRepo: bc?.git_repo ?? "",
      gitBranch: bc?.branch ?? "main",
      builder: (bc?.builder as typeof form.builder) ?? "nixpacks",
      dockerfilePath: bc?.dockerfile_path ?? "Dockerfile",
      registryIntegrationId: bc?.registry_integration_id ?? "",
      builderNodeName: bc?.builder_node ?? "",
      builderCPURequest: bc?.builder_cpu_request || "1000m",
      builderMemoryRequest: bc?.builder_memory_request || "1Gi",
      nodeId: service.node_id ?? "",
      port: service.port ?? 3000,
      replicas: service.replicas,
      cpuRequest: service.cpu_request,
      cpuLimit: service.cpu_limit,
      memoryRequest: service.memory_request,
      memoryLimit: service.memory_limit,
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [service, bc])

  const { data: repoList = [], isFetching: reposFetching } = useQuery({
    queryKey: ["git-repos", orgId, form.gitIntegrationId],
    queryFn: () => gitApi.repos(orgId, form.gitIntegrationId, token),
    enabled: !!form.gitIntegrationId,
    staleTime: 5 * 60 * 1000,
  })

  const { data: branchList = [], isFetching: branchesFetching } = useQuery({
    queryKey: ["git-branches", orgId, form.gitIntegrationId, form.gitRepo],
    queryFn: () => gitApi.branches(orgId, form.gitIntegrationId, form.gitRepo, token),
    enabled: !!form.gitIntegrationId && !!form.gitRepo,
    staleTime: 2 * 60 * 1000,
  })

  // ── Save ──────────────────────────────────────────────────────────────────
  const mutation = useMutation({
    mutationFn: async () => {
      // Always update service fields
      const updatedSvc = await servicesApi.update(orgId, projectId, serviceId, {
        image: form.source === "image" ? form.image : "",
        node_id: form.nodeId,
        port: form.port,
        replicas: form.replicas,
        cpu_request: form.cpuRequest,
        cpu_limit: form.cpuLimit,
        memory_request: form.memoryRequest,
        memory_limit: form.memoryLimit,
      }, token)
      // Update build config when git source
      if (form.source === "git") {
        await buildConfigsApi.update(orgId, projectId, serviceId, {
          git_integration_id: form.gitIntegrationId || undefined,
          git_repo: form.gitRepo,
          branch: form.gitBranch,
          builder: form.builder,
          dockerfile_path: form.builder === "dockerfile" ? form.dockerfilePath : undefined,
          registry_integration_id: form.registryIntegrationId || undefined,
          builder_node: form.builderNodeName,
          builder_cpu_request: form.builderCPURequest,
          builder_memory_request: form.builderMemoryRequest,
        }, token)
      }
      return updatedSvc
    },
    onSuccess: (updated) => {
      queryClient.setQueryData(["service", orgId, projectId, serviceId], updated)
      queryClient.invalidateQueries({ queryKey: ["services", orgId, projectId] })
      queryClient.invalidateQueries({ queryKey: ["build-config", orgId, projectId, serviceId] })
    },
  })

  if (svcLoading || bcLoading) return (
    <div className="flex items-center gap-2 text-muted-foreground py-8">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
    </div>
  )

  return (
    <div className="space-y-8">
      {/* ── Source ────────────────────────────────────────────── */}
      <Section title="Source" subtitle="Where should Meshploy pull the code or image from?">
        <div className="flex rounded-lg border border-border/60 overflow-hidden w-fit">
          {(["git", "image"] as const).map((src) => (
            <button
              key={src}
              type="button"
              onClick={() => patch({ source: src })}
              className={cn(
                "px-4 py-2 text-sm transition-colors",
                form.source === src
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              {src === "git" ? "Git repository" : "Docker image"}
            </button>
          ))}
        </div>

        {form.source === "image" ? (
          <Field label="Image">
            <input value={form.image} onChange={(e) => patch({ image: e.target.value })}
              placeholder="nginx:alpine" className={inputCls} />
          </Field>
        ) : (
          <div className="space-y-4">
            <Field label="Git integration">
              <Select value={form.gitIntegrationId} onValueChange={(v) => patch({ gitIntegrationId: v ?? "", gitRepo: "", gitBranch: "" })}>
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue placeholder={connectedGit.length === 0 ? "No connected integrations" : "Select a git integration…"}>
                    {connectedGit.find((g) => g.id === form.gitIntegrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {connectedGit.map((g) => <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>

            <div className="grid grid-cols-2 gap-4">
              <Field label={reposFetching ? "Repository (loading…)" : "Repository"}>
                <Select
                  value={form.gitRepo}
                  onValueChange={(v) => {
                    const repo = repoList.find((r) => r.full_name === v)
                    patch({ gitRepo: v ?? "", gitBranch: repo?.default_branch ?? form.gitBranch })
                  }}
                  disabled={!form.gitIntegrationId || reposFetching}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue placeholder={!form.gitIntegrationId ? "Select an integration first" : reposFetching ? "Loading…" : "Select a repository…"} />
                  </SelectTrigger>
                  <SelectContent>
                    {form.gitRepo && !repoList.find((r) => r.full_name === form.gitRepo) && (
                      <SelectItem value={form.gitRepo}>{form.gitRepo}</SelectItem>
                    )}
                    {repoList.map((r) => <SelectItem key={r.full_name} value={r.full_name}>{r.full_name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>

              <Field label={branchesFetching ? "Branch (loading…)" : "Branch"}>
                <Select
                  value={form.gitBranch}
                  onValueChange={(v) => patch({ gitBranch: v ?? "main" })}
                  disabled={!form.gitIntegrationId || !form.gitRepo || branchesFetching}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue placeholder={!form.gitRepo ? "Select a repo first" : branchesFetching ? "Loading…" : "Select a branch…"} />
                  </SelectTrigger>
                  <SelectContent>
                    {form.gitBranch && !branchList.find((b) => b === form.gitBranch) && (
                      <SelectItem value={form.gitBranch}>{form.gitBranch}</SelectItem>
                    )}
                    {branchList.map((b) => <SelectItem key={b} value={b}>{b}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <Field label="Builder">
                <Select value={form.builder} onValueChange={(v) => patch({ builder: (v ?? "nixpacks") as typeof form.builder })}>
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="nixpacks">Nixpacks</SelectItem>
                    <SelectItem value="railpack">Railpack</SelectItem>
                    <SelectItem value="dockerfile">Dockerfile</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              {form.builder === "dockerfile" && (
                <Field label="Dockerfile path">
                  <input value={form.dockerfilePath} onChange={(e) => patch({ dockerfilePath: e.target.value })} className={inputCls} />
                </Field>
              )}
            </div>

            <Field label="Registry">
              <Select value={form.registryIntegrationId} onValueChange={(v) => patch({ registryIntegrationId: v ?? "" })}>
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue placeholder={registryList.length === 0 ? "No registries — add one in Integrations" : "Select a registry…"}>
                    {registryList.find((r) => r.id === form.registryIntegrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {registryList.map((r) => <SelectItem key={r.id} value={r.id}>{r.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
          </div>
        )}
      </Section>

      {/* ── Build ─────────────────────────────────────────────── */}
      {form.source === "git" && (
        <Section title="Build" subtitle="Configure where and how the build job runs">
          <div className="space-y-2">
            <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
              <Server className="h-3.5 w-3.5" /> Builder node
            </label>
            <div className="flex flex-wrap gap-2">
              <NodeCard label="Auto-schedule" sub="Any builder node" selected={form.builderNodeName === ""} onClick={() => patch({ builderNodeName: "" })} />
              {builderNodes.map((node) => (
                <NodeCard key={node.k8s_node_name} label={node.name} sub={node.tailscale_ip}
                  selected={form.builderNodeName === node.k8s_node_name}
                  onClick={() => patch({ builderNodeName: node.k8s_node_name })} online />
              ))}
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Builder CPU request">
              <input value={form.builderCPURequest} onChange={(e) => patch({ builderCPURequest: e.target.value })} placeholder="1000m" className={inputCls} />
            </Field>
            <Field label="Builder memory request">
              <input value={form.builderMemoryRequest} onChange={(e) => patch({ builderMemoryRequest: e.target.value })} placeholder="1Gi" className={inputCls} />
            </Field>
          </div>
        </Section>
      )}

      {/* ── Deployment ────────────────────────────────────────── */}
      <Section title="Deployment" subtitle="Choose where this service runs and how many replicas to start">
        <div className="space-y-2">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5" /> Target node
          </label>
          <div className="flex flex-wrap gap-2">
            <NodeCard label="Auto-schedule" sub="Let K3s decide" selected={form.nodeId === ""} onClick={() => patch({ nodeId: "" })} />
            {workerNodes.map((node) => (
              <NodeCard key={node.id} label={node.name} sub={node.tailscaleIP}
                selected={form.nodeId === node.id}
                onClick={() => patch({ nodeId: node.id })} online />
            ))}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <Field label="Port">
            <Input type="number" min={1} max={65535} value={form.port}
              onChange={(e) => patch({ port: parseInt(e.target.value) || 3000 })} />
          </Field>
          <Field label="Replicas">
            <Input type="number" min={1} max={20} value={form.replicas}
              onChange={(e) => patch({ replicas: Math.max(1, parseInt(e.target.value) || 1) })} />
          </Field>
        </div>
      </Section>

      {/* ── Resource limits (collapsible) ─────────────────────── */}
      <div className="rounded-lg border border-border/40">
        <button
          type="button"
          onClick={() => patch({ showResources: !form.showResources })}
          className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <span className="font-medium">Resource limits</span>
          <span className={cn("text-xs transition-transform inline-block", form.showResources ? "rotate-180" : "")}>▼</span>
        </button>
        {form.showResources && (
          <div className="px-4 pb-4 pt-0 grid grid-cols-2 gap-4 border-t border-border/40">
            <Field label="CPU request"><input value={form.cpuRequest} onChange={(e) => patch({ cpuRequest: e.target.value })} className={inputCls} /></Field>
            <Field label="CPU limit"><input value={form.cpuLimit} onChange={(e) => patch({ cpuLimit: e.target.value })} className={inputCls} /></Field>
            <Field label="Memory request"><input value={form.memoryRequest} onChange={(e) => patch({ memoryRequest: e.target.value })} className={inputCls} /></Field>
            <Field label="Memory limit"><input value={form.memoryLimit} onChange={(e) => patch({ memoryLimit: e.target.value })} className={inputCls} /></Field>
          </div>
        )}
      </div>

      {mutation.isError && (
        <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>
      )}
      {mutation.isSuccess && (
        <p className="text-xs text-emerald-400">Saved.</p>
      )}
      <div className="flex justify-end">
        <Button size="sm" className="gap-1.5" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
          {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save changes
        </Button>
      </div>
    </div>
  )
}

// ─── Build env vars section ───────────────────────────────────────────────────

function BuildEnvVarsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const [envVars, setEnvVars] = useState("")

  const { data, isLoading, isError } = useQuery({
    queryKey: ["build-env-vars", orgId, projectId, serviceId],
    queryFn: () => buildConfigsApi.getBuildEnvVars(orgId, projectId, serviceId, token),
    enabled: !!orgId,
    retry: false,
  })

  useEffect(() => {
    if (data !== undefined) setEnvVars(data.build_env_vars)
  }, [data])

  const mutation = useMutation({
    mutationFn: () =>
      buildConfigsApi.putBuildEnvVars(orgId, projectId, serviceId, envVars, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["build-env-vars", orgId, projectId, serviceId] })
    },
  })

  if (isError) return null // no build config yet

  return (
    <Section
      title="Build environment variables"
      subtitle="Injected at build time only — not available at runtime. One KEY=VALUE per line. For nixpacks: passed as --env flags (e.g. NIXPACKS_INSTALL_CMD=npm install). For dockerfile: passed as --build-arg."
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-4">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <Textarea
          value={envVars}
          onChange={(e) => setEnvVars(e.target.value)}
          placeholder={"NIXPACKS_INSTALL_CMD=npm install\nNODE_ENV=production"}
          className="font-mono text-xs min-h-[120px] resize-y bg-muted/20 border-border/60"
        />
      )}
      {mutation.isError && (
        <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>
      )}
      {mutation.isSuccess && (
        <p className="text-xs text-emerald-400">Saved.</p>
      )}
      <div className="flex justify-end">
        <Button size="sm" className="gap-1.5" onClick={() => mutation.mutate()} disabled={mutation.isPending || isLoading}>
          {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save
        </Button>
      </div>
    </Section>
  )
}

// ─── Rollback section ─────────────────────────────────────────────────────────

function RollbackSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: bc, isLoading } = useQuery({
    queryKey: ["build-config", orgId, projectId, serviceId],
    queryFn: () => buildConfigsApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
    retry: false,
  })

  const [enabled, setEnabled] = useState(false)
  const [retention, setRetention] = useState("5")

  useEffect(() => {
    if (bc) {
      setEnabled(bc.rollback_enabled ?? false)
      setRetention(String(bc.image_retention ?? 5))
    }
  }, [bc])

  const mutation = useMutation({
    mutationFn: () =>
      buildConfigsApi.update(orgId, projectId, serviceId, {
        rollback_enabled: enabled,
        image_retention: parseInt(retention) || 5,
      }, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["build-config", orgId, projectId, serviceId] }),
  })

  // Only show for application services that have a build config
  if (service?.type !== "application" || (!isLoading && !bc)) return null

  return (
    <Section
      title="Rollback"
      subtitle="Keep previous deployment images so you can roll back instantly without a rebuild."
    >
      <div className="space-y-4">
        <Field label="Enable rollback">
          <div className="flex items-center gap-2">
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              onClick={() => setEnabled((v) => !v)}
              className={cn(
                "relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:outline-none",
                enabled ? "bg-primary" : "bg-input"
              )}
            >
              <span
                className={cn(
                  "pointer-events-none inline-block h-4 w-4 rounded-full bg-background shadow-lg transition-transform",
                  enabled ? "translate-x-4" : "translate-x-0"
                )}
              />
            </button>
            <span className="text-xs text-muted-foreground">
              {enabled ? "Enabled" : "Disabled"}
            </span>
          </div>
        </Field>

        {enabled && (
          <Field label="Images to keep">
            <input
              type="number"
              min={1}
              max={50}
              value={retention}
              onChange={(e) => setRetention(e.target.value)}
              className={cn(inputCls, "w-24")}
            />
          </Field>
        )}

        <div className="flex justify-end">
          <Button size="sm" className="gap-1.5" onClick={() => mutation.mutate()} disabled={mutation.isPending || isLoading}>
            {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Save
          </Button>
        </div>
      </div>
    </Section>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function ConfigTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/config",
  })

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <EnvVarsSection projectId={projectId} serviceId={serviceId} />
      <SecretsSection projectId={projectId} serviceId={serviceId} />
      <BuildEnvVarsSection projectId={projectId} serviceId={serviceId} />
      <SourceDeploySection projectId={projectId} serviceId={serviceId} />
      <RollbackSection projectId={projectId} serviceId={serviceId} />
    </div>
  )
}
