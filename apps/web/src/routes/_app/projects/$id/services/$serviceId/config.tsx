import { createFileRoute, useParams } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import { useState, useEffect } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Save, Server } from "lucide-react"
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
  toNode,
  type ApiNode,
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
      <Button className="w-full gap-2" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
        {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
        Save changes
      </Button>
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

// ─── Page ─────────────────────────────────────────────────────────────────────

function ConfigTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/config",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const isDatabase = service?.type === "database"

  return (
    <div className="p-6 max-w-2xl space-y-6">
      {!isDatabase && (
        <>
          <BuildEnvVarsSection projectId={projectId} serviceId={serviceId} />
          <SourceDeploySection projectId={projectId} serviceId={serviceId} />
        </>
      )}
      <EnvVarsSection projectId={projectId} serviceId={serviceId} />
    </div>
  )
}
