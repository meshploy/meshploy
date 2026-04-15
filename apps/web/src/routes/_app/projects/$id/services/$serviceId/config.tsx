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
  toNode,
  type ApiNode,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/config"
)({
  component: ConfigTab,
})

const inputCls =
  "w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"

function Section({
  title,
  subtitle,
  children,
}: {
  title: string
  subtitle?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-4 pb-6 border-b border-border/40 last:border-0 last:pb-0">
      <div>
        <p className="text-sm font-medium">{title}</p>
        {subtitle && <p className="text-xs text-muted-foreground mt-0.5">{subtitle}</p>}
      </div>
      {children}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      {children}
    </div>
  )
}

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

// ─── Resources section ────────────────────────────────────────────────────────

function ResourcesSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()

  const { data: service, isLoading } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const workerNodes = rawNodes
    .filter((n) => n.k8s_member && n.status === "online" && n.k3s_role === "agent")
    .map(toNode)

  const [form, setForm] = useState({
    replicas: 1,
    cpuRequest: "100m",
    cpuLimit: "500m",
    memoryRequest: "128Mi",
    memoryLimit: "512Mi",
    nodeId: "",
  })

  useEffect(() => {
    if (service) {
      setForm({
        replicas: service.replicas,
        cpuRequest: service.cpu_request,
        cpuLimit: service.cpu_limit,
        memoryRequest: service.memory_request,
        memoryLimit: service.memory_limit,
        nodeId: service.node_id ?? "",
      })
    }
  }, [service])

  const mutation = useMutation({
    mutationFn: () =>
      servicesApi.update(orgId, projectId, serviceId, {
        replicas: form.replicas,
        cpu_request: form.cpuRequest,
        cpu_limit: form.cpuLimit,
        memory_request: form.memoryRequest,
        memory_limit: form.memoryLimit,
        node_id: form.nodeId,
      }, token),
    onSuccess: (updated) => {
      queryClient.setQueryData(["service", orgId, projectId, serviceId], updated)
      queryClient.invalidateQueries({ queryKey: ["services", orgId, projectId] })
    },
  })

  if (isLoading) return (
    <Section title="Scaling & resources">
      <div className="flex items-center gap-2 text-muted-foreground py-4">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
      </div>
    </Section>
  )

  return (
    <Section title="Scaling & resources" subtitle="Changes take effect on next deployment.">
      <div className="grid grid-cols-2 gap-4">
        <Field label="Replicas">
          <input
            type="number" min={1} max={20}
            value={form.replicas}
            onChange={(e) => setForm((f) => ({ ...f, replicas: Math.max(1, parseInt(e.target.value) || 1) }))}
            className={inputCls}
          />
        </Field>
        <Field label="Node">
          <Select value={form.nodeId} onValueChange={(v) => setForm((f) => ({ ...f, nodeId: v ?? "" }))}>
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue placeholder="Auto-schedule">
                {form.nodeId ? workerNodes.find((n) => n.id === form.nodeId)?.name ?? form.nodeId : "Auto-schedule"}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">Auto-schedule</SelectItem>
              {workerNodes.map((n) => (
                <SelectItem key={n.id} value={n.id}>{n.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="CPU request"><input value={form.cpuRequest} onChange={(e) => setForm((f) => ({ ...f, cpuRequest: e.target.value }))} className={inputCls} /></Field>
        <Field label="CPU limit"><input value={form.cpuLimit} onChange={(e) => setForm((f) => ({ ...f, cpuLimit: e.target.value }))} className={inputCls} /></Field>
        <Field label="Memory request"><input value={form.memoryRequest} onChange={(e) => setForm((f) => ({ ...f, memoryRequest: e.target.value }))} className={inputCls} /></Field>
        <Field label="Memory limit"><input value={form.memoryLimit} onChange={(e) => setForm((f) => ({ ...f, memoryLimit: e.target.value }))} className={inputCls} /></Field>
      </div>
      {mutation.isError && (
        <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>
      )}
      <div className="flex justify-end">
        <Button size="sm" className="gap-1.5" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
          {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save
        </Button>
      </div>
    </Section>
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

// ─── Build source section ─────────────────────────────────────────────────────

function BuildSourceSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()

  const { data: bc, isLoading, isError } = useQuery({
    queryKey: ["build-config", orgId, projectId, serviceId],
    queryFn: () => buildConfigsApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
    retry: false, // 404 = no build config, don't retry
  })

  const { data: gitList = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token),
    enabled: !!orgId,
  })
  const connectedGit = gitList.filter((g) => g.connected)

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const builderNodes = rawNodes.filter(
    (n) => n.k8s_member && n.status === "online" && n.k3s_labels?.["meshploy.com/role"] === "builder"
  )

  const [form, setForm] = useState({
    gitIntegrationId: "",
    gitRepo: "",
    branch: "main",
    builder: "nixpacks" as "nixpacks" | "railpack" | "dockerfile",
    dockerfilePath: "Dockerfile",
    builderNode: "",  // "" = auto-schedule, k8s_node_name = pin to node
    builderCPURequest: "",
    builderMemoryRequest: "",
  })

  useEffect(() => {
    if (bc) {
      setForm({
        gitIntegrationId: bc.git_integration_id ?? "",
        gitRepo: bc.git_repo,
        branch: bc.branch,
        builder: bc.builder as "nixpacks" | "railpack" | "dockerfile",
        dockerfilePath: bc.dockerfile_path,
        builderNode: bc.builder_node ?? "",
        builderCPURequest: bc.builder_cpu_request ?? "",
        builderMemoryRequest: bc.builder_memory_request ?? "",
      })
    }
  }, [bc])

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

  const mutation = useMutation({
    mutationFn: () =>
      buildConfigsApi.update(orgId, projectId, serviceId, {
        git_repo: form.gitRepo,
        branch: form.branch,
        builder: form.builder,
        dockerfile_path: form.builder === "dockerfile" ? form.dockerfilePath : undefined,
        git_integration_id: form.gitIntegrationId || undefined,
        builder_node: form.builderNode,
        builder_cpu_request: form.builderCPURequest,
        builder_memory_request: form.builderMemoryRequest,
      }, token),
    onSuccess: (updated) => {
      queryClient.setQueryData(["build-config", orgId, projectId, serviceId], updated)
    },
  })

  if (isLoading) return (
    <Section title="Build source">
      <div className="flex items-center gap-2 text-muted-foreground py-4">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
      </div>
    </Section>
  )

  if (isError) return (
    <Section title="Build source" subtitle="No build config yet. This service deploys from a pre-built image.">
      <p className="text-xs text-muted-foreground">
        Build config will be available after switching this service to a git source.
      </p>
    </Section>
  )

  return (
    <Section title="Build source" subtitle="Repository and build settings.">
      <div className="space-y-4">
        <Field label="Git integration">
          <Select
            value={form.gitIntegrationId}
            onValueChange={(v) => setForm((f) => ({ ...f, gitIntegrationId: v ?? "" }))}
          >
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue placeholder={
                connectedGit.length === 0
                  ? "No connected integrations"
                  : "Select integration to change repo…"
              }>
                {connectedGit.find((g) => g.id === form.gitIntegrationId)?.name}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {connectedGit.map((g) => (
                <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <div className="grid grid-cols-2 gap-4">
          <Field label={reposFetching ? "Repository (loading…)" : "Repository"}>
            <Select
              value={form.gitRepo}
              onValueChange={(v) => {
                const repo = repoList.find((r) => r.full_name === v)
                setForm((f) => ({ ...f, gitRepo: v ?? "", branch: repo?.default_branch ?? f.branch }))
              }}
              disabled={!form.gitIntegrationId || reposFetching}
            >
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue placeholder={
                  !form.gitIntegrationId ? "Select an integration to change…" :
                  reposFetching ? "Loading…" :
                  repoList.length === 0 ? "No accessible repositories" :
                  "Select a repository…"
                } />
              </SelectTrigger>
              <SelectContent>
                {/* Ensure current value always renders in trigger even when list hasn't loaded */}
                {form.gitRepo && !repoList.find((r) => r.full_name === form.gitRepo) && (
                  <SelectItem value={form.gitRepo}>{form.gitRepo}</SelectItem>
                )}
                {repoList.map((r) => (
                  <SelectItem key={r.full_name} value={r.full_name}>{r.full_name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field label={branchesFetching ? "Branch (loading…)" : "Branch"}>
            <Select
              value={form.branch}
              onValueChange={(v) => setForm((f) => ({ ...f, branch: v ?? "main" }))}
              disabled={!form.gitIntegrationId || !form.gitRepo || branchesFetching}
            >
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue placeholder={
                  !form.gitRepo ? "Select a repo first" :
                  branchesFetching ? "Loading…" :
                  "Select a branch…"
                } />
              </SelectTrigger>
              <SelectContent>
                {/* Ensure current value always renders in trigger even when list hasn't loaded */}
                {form.branch && !branchList.find((b) => b === form.branch) && (
                  <SelectItem value={form.branch}>{form.branch}</SelectItem>
                )}
                {branchList.map((b) => (
                  <SelectItem key={b} value={b}>{b}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <Field label="Builder">
            <Select value={form.builder} onValueChange={(v) => setForm((f) => ({ ...f, builder: (v ?? "nixpacks") as typeof f.builder }))}>
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
              <input value={form.dockerfilePath} onChange={(e) => setForm((f) => ({ ...f, dockerfilePath: e.target.value }))} className={inputCls} />
            </Field>
          )}
        </div>

        {/* Builder node picker */}
        <div className="space-y-2">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3 w-3" />
            Builder node
          </label>
          <div className="grid grid-cols-3 gap-2">
            <button
              type="button"
              onClick={() => setForm((f) => ({ ...f, builderNode: "" }))}
              className={cn(
                "flex flex-col gap-1 rounded-lg border-2 p-3 text-left transition-all",
                form.builderNode === ""
                  ? "border-primary bg-primary/5"
                  : "border-border/60 bg-card hover:border-border/80"
              )}
            >
              <span className="text-sm font-medium">Auto-schedule</span>
              <span className="text-[11px] text-muted-foreground">Any builder node</span>
            </button>
            {builderNodes.map((node) => (
              <button
                type="button"
                key={node.k8s_node_name}
                onClick={() => setForm((f) => ({ ...f, builderNode: node.k8s_node_name }))}
                className={cn(
                  "flex flex-col gap-1 rounded-lg border-2 p-3 text-left transition-all",
                  form.builderNode === node.k8s_node_name
                    ? "border-primary bg-primary/5"
                    : "border-border/60 bg-card hover:border-border/80"
                )}
              >
                <div className="flex items-center gap-1.5">
                  <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                  <span className="text-sm font-medium truncate">{node.name}</span>
                </div>
                <span className="text-[11px] text-muted-foreground font-mono">{node.tailscale_ip}</span>
              </button>
            ))}
          </div>
          <div className="grid grid-cols-2 gap-4 mt-3">
            <Field label="Builder CPU request">
              <input
                value={form.builderCPURequest}
                onChange={(e) => setForm((f) => ({ ...f, builderCPURequest: e.target.value }))}
                placeholder="1000m"
                className={inputCls}
              />
            </Field>
            <Field label="Builder memory request">
              <input
                value={form.builderMemoryRequest}
                onChange={(e) => setForm((f) => ({ ...f, builderMemoryRequest: e.target.value }))}
                placeholder="1Gi"
                className={inputCls}
              />
            </Field>
          </div>
        </div>
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

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <EnvVarsSection projectId={projectId} serviceId={serviceId} />
      <BuildEnvVarsSection projectId={projectId} serviceId={serviceId} />
      <ResourcesSection projectId={projectId} serviceId={serviceId} />
      <BuildSourceSection projectId={projectId} serviceId={serviceId} />
    </div>
  )
}
