import { SiPostgresql, SiMysql, SiRedis, SiMongodb } from "@icons-pack/react-simple-icons"
import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState, useEffect } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import {
  Box,
  ChevronLeft,
  ChevronDown,
  Clock,
  Database,
  Globe,
  Loader2,
  Server,
  Zap,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  projects as projectsApi,
  gitIntegrations as gitApi,
  nodes as nodesApi,
  services as servicesApi,
  registries as registryApi,
  routes as routesApi,
  domains as domainsApi,
  toNode,
  type CreateServiceBody,
  type ApiNode,
  type ApiService,
  type ApiDomain,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Section, Field, NodeCard } from "@/components/services/form-primitives"
import { Input } from "@/components/ui/input"

// ─── Route ───────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/_app/projects/$id/new")({
  validateSearch: (search: Record<string, unknown>) => ({
    type: (search.type as ResourceType | undefined) ?? "service",
  }),
  component: NewResourcePage,
})

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = "service" | "route" | "job" | "cron-job" | "database"
type AppSource = "git" | "image"
type Builder = "nixpacks" | "railpack" | "dockerfile"

interface FormState {
  source: AppSource
  name: string
  image: string
  gitIntegrationId: string
  gitRepo: string
  gitBranch: string
  builder: Builder
  registryIntegrationId: string
  nodeId: string | null
  builderNodeName: string | null  // k8s_node_name; null = auto-schedule
  builderCPURequest: string
  builderMemoryRequest: string
  port: number
  replicas: number
  cpuRequest: string
  cpuLimit: string
  memoryRequest: string
  memoryLimit: string
  showResources: boolean
}

const INITIAL: FormState = {
  source: "git",
  name: "",
  image: "",
  gitIntegrationId: "",
  gitRepo: "",
  gitBranch: "",
  builder: "nixpacks",
  registryIntegrationId: "",
  nodeId: null,
  builderNodeName: null,
  builderCPURequest: "1000m",
  builderMemoryRequest: "1Gi",
  port: 3000,
  replicas: 1,
  cpuRequest: "100m",
  cpuLimit: "500m",
  memoryRequest: "128Mi",
  memoryLimit: "512Mi",
  showResources: false,
}

// ─── Sidebar resource types ───────────────────────────────────────────────────

const RESOURCE_TYPES: {
  type: ResourceType
  icon: typeof Box
  label: string
  soon?: boolean
}[] = [
  { type: "service",  icon: Box,      label: "Service"  },
  { type: "database", icon: Database, label: "Database" },
  { type: "route",    icon: Globe,    label: "Route"    },
  { type: "job",      icon: Zap,      label: "Job",      soon: true },
  { type: "cron-job", icon: Clock,    label: "Cron Job", soon: true },
]

// ─── Page ─────────────────────────────────────────────────────────────────────

function NewResourcePage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/new" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()

  const { type: initialType } = Route.useSearch()
  const [resourceType, setResourceType] = useState<ResourceType>(initialType)
  const [form, setForm] = useState<FormState>(INITIAL)

  const patch = (partial: Partial<FormState>) =>
    setForm((s) => ({ ...s, ...partial }))

  const { data: project } = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const createMutation = useMutation({
    mutationFn: () => {
      const body: CreateServiceBody = {
        name: form.name,
        port: form.port,
        replicas: form.replicas,
        cpu_request: form.cpuRequest || undefined,
        cpu_limit: form.cpuLimit || undefined,
        memory_request: form.memoryRequest || undefined,
        memory_limit: form.memoryLimit || undefined,
        node_id: form.nodeId ?? undefined,
      }
      if (form.source === "image") {
        body.image = form.image
      } else {
        body.git_integration_id       = form.gitIntegrationId || undefined
        body.git_repo                 = form.gitRepo
        body.branch                   = form.gitBranch
        body.builder                  = form.builder
        body.registry_integration_id  = form.registryIntegrationId || undefined
        body.builder_node             = form.builderNodeName ?? ""
        body.builder_cpu_request      = form.builderCPURequest || undefined
        body.builder_memory_request   = form.builderMemoryRequest || undefined
      }
      return servicesApi.create(orgId!, projectId, body, token)
    },
    onSuccess: (service) => {
      navigate({
        to: "/projects/$id/services/$serviceId/deployments",
        params: { id: projectId, serviceId: service.id },
      })
    },
  })

  const canCreate =
    form.name.trim().length > 0 &&
    (form.source === "image"
      ? form.image.trim().length > 0
      : form.gitIntegrationId.length > 0 &&
        form.gitRepo.length > 0 &&
        form.gitBranch.length > 0 &&
        form.registryIntegrationId.length > 0)

  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top bar */}
      <div className="sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <button
            onClick={() => navigate({ to: "/projects/$id/services", params: { id: projectId } })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            {project?.name ?? "Project"}
          </button>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-sm font-medium">Create new</span>
        </div>
      </div>

      <div className="flex flex-1">
        {/* ─── Sidebar ─────────────────────────────────────────────── */}
        <aside className="w-52 shrink-0 border-r border-border/40 py-6 px-3">
          <p className="text-[11px] font-medium text-muted-foreground/60 uppercase tracking-wider px-2 mb-2">
            Resource type
          </p>
          <nav className="space-y-0.5">
            {RESOURCE_TYPES.map(({ type, icon: Icon, label, soon }) => (
              <button
                key={type}
                onClick={() => !soon && setResourceType(type)}
                disabled={soon}
                className={cn(
                  "w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm transition-colors text-left",
                  resourceType === type && !soon
                    ? "bg-primary/10 text-primary font-medium"
                    : soon
                    ? "text-muted-foreground/40 cursor-not-allowed"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/40"
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="flex-1">{label}</span>
                {soon && (
                  <span className="text-[9px] font-mono border border-border/40 px-1 py-px rounded text-muted-foreground/40">
                    soon
                  </span>
                )}
              </button>
            ))}
          </nav>
        </aside>

        {/* ─── Form ────────────────────────────────────────────────── */}
        <main className="flex-1 py-8 px-8 max-w-2xl">
          {resourceType === "service" ? (
            <ServiceForm
              form={form}
              patch={patch}
              canCreate={canCreate}
              isPending={createMutation.isPending}
              error={createMutation.error as Error | null}
              onCreate={() => createMutation.mutate()}
            />
          ) : resourceType === "database" ? (
            <DatabaseForm projectId={projectId} />
          ) : resourceType === "route" ? (
            <RouteForm projectId={projectId} />
          ) : (
            <ComingSoonForm type={resourceType} />
          )}
        </main>
      </div>
    </div>
  )
}

// ─── Service form ─────────────────────────────────────────────────────────────

function ServiceForm({
  form,
  patch,
  canCreate,
  isPending,
  error,
  onCreate,
}: {
  form: FormState
  patch: (p: Partial<FormState>) => void
  canCreate: boolean
  isPending: boolean
  error: Error | null
  onCreate: () => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: gitList = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token),
    enabled: !!orgId,
  })

  const connectedGit = gitList.filter((g) => g.connected)

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

  const { data: registryList = [] } = useQuery({
    queryKey: ["registry-integrations", orgId],
    queryFn: () => registryApi.list(orgId, token),
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

  const builderNodes = rawNodes.filter(
    (n) => n.k8s_member && n.status === "online" && n.k3s_labels?.["meshploy.com/role"] === "builder"
  )

  return (
    <div className="space-y-8">
      {/* ── Section: Basic info ──────────────────────────────── */}
      <Section title="Basic info" subtitle="Give your service a name">
        <Field label="Service name" required>
          <input
            value={form.name}
            onChange={(e) => patch({ name: e.target.value })}
            placeholder="my-api"
            className={inputCls}
            autoFocus
          />
        </Field>
      </Section>

      {/* ── Section: Source ──────────────────────────────────── */}
      <Section title="Source" subtitle="Where should Meshploy pull the code or image from?">
        {/* Source toggle */}
        <div className="flex rounded-lg border border-border/60 overflow-hidden w-fit">
          {(["git", "image"] as AppSource[]).map((src) => (
            <button
              key={src}
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
          <Field label="Image" required>
            <input
              value={form.image}
              onChange={(e) => patch({ image: e.target.value })}
              placeholder="nginx:alpine"
              className={inputCls}
            />
          </Field>
        ) : (
          <div className="space-y-4">
            <Field label="Git integration" required>
              <Select
                value={form.gitIntegrationId}
                onValueChange={(v) => patch({ gitIntegrationId: v ?? "", gitRepo: "", gitBranch: "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      connectedGit.length === 0
                        ? "No connected integrations — add one in Integrations"
                        : "Select a git integration…"
                    }
                  >
                    {connectedGit.find((g) => g.id === form.gitIntegrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {connectedGit.map((g) => (
                    <SelectItem key={g.id} value={g.id}>
                      {g.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <Field label={reposFetching ? "Repository (loading…)" : "Repository"} required>
              <Select
                value={form.gitRepo}
                onValueChange={(v) => {
                  const repo = repoList.find((r) => r.full_name === v)
                  patch({ gitRepo: v ?? "", gitBranch: repo?.default_branch ?? "" })
                }}
                disabled={!form.gitIntegrationId || reposFetching}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      !form.gitIntegrationId
                        ? "Select an integration first"
                        : reposFetching
                        ? "Loading repositories…"
                        : repoList.length === 0
                        ? "No accessible repositories"
                        : "Select a repository…"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {repoList.map((r) => (
                    <SelectItem key={r.full_name} value={r.full_name}>
                      {r.full_name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <div className="grid grid-cols-2 gap-4">
              <Field label={branchesFetching ? "Branch (loading…)" : "Branch"} required>
                <Select
                  value={form.gitBranch}
                  onValueChange={(v) => patch({ gitBranch: v ?? "" })}
                  disabled={!form.gitRepo || branchesFetching}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue
                      placeholder={
                        !form.gitRepo
                          ? "Select a repo first"
                          : branchesFetching
                          ? "Loading branches…"
                          : "Select a branch…"
                      }
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {branchList.map((b) => (
                      <SelectItem key={b} value={b}>{b}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>

              <Field label="Builder">
                <Select
                  value={form.builder}
                  onValueChange={(v) => patch({ builder: (v ?? "nixpacks") as Builder })}
                >
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
            </div>

            <Field label="Registry" required>
              <Select
                value={form.registryIntegrationId}
                onValueChange={(v) => patch({ registryIntegrationId: v ?? "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      registryList.length === 0
                        ? "No registries — add one in Integrations"
                        : "Select a registry to push the built image…"
                    }
                  >
                    {registryList.find((r) => r.id === form.registryIntegrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {registryList.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      {r.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>
        )}
      </Section>

      {/* ── Section: Build ───────────────────────────────────── */}
      {form.source === "git" && (
        <Section title="Build" subtitle="Configure where and how the build job runs">
          <div className="space-y-2">
            <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
              <Server className="h-3.5 w-3.5" /> Builder node
            </label>
            <div className="flex flex-wrap gap-2">
              <NodeCard
                label="Auto-schedule"
                sub="Any builder node"
                selected={form.builderNodeName === null}
                onClick={() => patch({ builderNodeName: null })}
              />
              {builderNodes.map((node) => (
                <NodeCard
                  key={node.k8s_node_name}
                  label={node.name}
                  sub={node.tailscale_ip}
                  selected={form.builderNodeName === node.k8s_node_name}
                  onClick={() => patch({ builderNodeName: node.k8s_node_name })}
                  online
                />
              ))}
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Builder CPU request">
              <input
                value={form.builderCPURequest}
                onChange={(e) => patch({ builderCPURequest: e.target.value })}
                placeholder="1000m"
                className={inputCls}
              />
            </Field>
            <Field label="Builder memory request">
              <input
                value={form.builderMemoryRequest}
                onChange={(e) => patch({ builderMemoryRequest: e.target.value })}
                placeholder="1Gi"
                className={inputCls}
              />
            </Field>
          </div>
        </Section>
      )}

      {/* ── Section: Deployment ──────────────────────────────── */}
      <Section title="Deployment" subtitle="Choose where this service runs and how many replicas to start">
        {/* Node selection */}
        <div className="space-y-2">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5" /> Target node
          </label>
          <div className="flex flex-wrap gap-2">
            <NodeCard
              label="Auto-schedule"
              sub="Let K3s decide"
              selected={form.nodeId === null}
              onClick={() => patch({ nodeId: null })}
            />
            {workerNodes.map((node) => (
              <NodeCard
                key={node.id}
                label={node.name}
                sub={node.tailscaleIP}
                selected={form.nodeId === node.id}
                onClick={() => patch({ nodeId: node.id })}
                online
              />
            ))}
          </div>
        </div>

        {/* Port + Replicas */}
        <div className="grid grid-cols-2 gap-4">
          <Field label="Port" required>
            <Input
              type="number"
              min={1}
              max={65535}
              value={form.port}
              onChange={(e) => patch({ port: parseInt(e.target.value) || 3000 })}
              placeholder="3000"
            />
          </Field>
          <Field label="Replicas">
            <Input
              type="number"
              min={1}
              max={20}
              value={form.replicas}
              onChange={(e) => patch({ replicas: Math.max(1, parseInt(e.target.value) || 1) })}
            />
          </Field>
        </div>
      </Section>

      {/* ── Section: Resources (advanced, collapsible) ───────── */}
      <div className="rounded-lg border border-border/40">
        <button
          onClick={() => patch({ showResources: !form.showResources })}
          className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <span className="font-medium">Resource limits</span>
          <ChevronDown
            className={cn(
              "h-4 w-4 transition-transform",
              form.showResources ? "rotate-180" : ""
            )}
          />
        </button>
        {form.showResources && (
          <div className="px-4 pb-4 pt-0 grid grid-cols-2 gap-4 border-t border-border/40">
            <Field label="CPU request">
              <input value={form.cpuRequest} onChange={(e) => patch({ cpuRequest: e.target.value })} className={inputCls} />
            </Field>
            <Field label="CPU limit">
              <input value={form.cpuLimit} onChange={(e) => patch({ cpuLimit: e.target.value })} className={inputCls} />
            </Field>
            <Field label="Memory request">
              <input value={form.memoryRequest} onChange={(e) => patch({ memoryRequest: e.target.value })} className={inputCls} />
            </Field>
            <Field label="Memory limit">
              <input value={form.memoryLimit} onChange={(e) => patch({ memoryLimit: e.target.value })} className={inputCls} />
            </Field>
          </div>
        )}
      </div>

      {/* ── Error ────────────────────────────────────────────── */}
      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
          <p className="text-xs text-destructive">{error.message}</p>
        </div>
      )}

      {/* ── Submit ───────────────────────────────────────────── */}
      <Button
        className="w-full gap-2"
        disabled={!canCreate || isPending}
        onClick={onCreate}
      >
        {isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
        Create service
      </Button>
    </div>
  )
}

// ─── Database form ────────────────────────────────────────────────────────────

type DbEngine = "postgres" | "mysql" | "redis" | "mongodb"

function DbEngineLogo({ engine, className }: { engine: DbEngine; className?: string }) {
  const icons = {
    postgres: SiPostgresql,
    mysql: SiMysql,
    redis: SiRedis,
    mongodb: SiMongodb,
  }
  const Icon = icons[engine]
  return <Icon className={className} />
}

const ENGINE_OPTIONS: { value: DbEngine; label: string; versions: string[]; defaultPort: number }[] = [
  { value: "postgres", label: "PostgreSQL", versions: ["18", "17", "16", "15", "14", "13"], defaultPort: 5432 },
  { value: "mysql",    label: "MySQL",      versions: ["8.0", "5.7"],           defaultPort: 3306 },
  { value: "redis",    label: "Redis",      versions: ["7", "6"],               defaultPort: 6379 },
  { value: "mongodb",  label: "MongoDB",    versions: ["7", "6"],               defaultPort: 27017 },
]

interface DbFormState {
  name: string
  engine: DbEngine
  version: string
  storageGB: number
  nodeId: string | null
  dbName: string
  dbUser: string
  dbPassword: string
}

const DB_INITIAL: DbFormState = {
  name: "",
  engine: "postgres",
  version: "16",
  storageGB: 10,
  nodeId: null,
  dbName: "",
  dbUser: "",
  dbPassword: "",
}

function DatabaseForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()

  const [dbf, setDbf] = useState<DbFormState>(DB_INITIAL)
  const patchDbf = (p: Partial<DbFormState>) => setDbf((s) => ({ ...s, ...p }))

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const workerNodes = rawNodes
    .filter((n) => n.k8s_member && n.status === "online" && n.k3s_role === "agent")
    .map(toNode)

  const selectedEngine = ENGINE_OPTIONS.find((e) => e.value === dbf.engine)!

  const createMutation = useMutation({
    mutationFn: () =>
      servicesApi.create(orgId, projectId, {
        name: dbf.name,
        type: "database",
        engine: dbf.engine,
        version: dbf.version,
        storage_gb: dbf.storageGB,
        node_id: dbf.nodeId ?? undefined,
        db_name: dbf.dbName || undefined,
        db_user: dbf.dbUser || undefined,
        db_password: dbf.dbPassword || undefined,
      }, token),
    onSuccess: (service) => {
      navigate({ to: "/projects/$id/databases", params: { id: projectId } })
    },
  })

  const canCreate = dbf.name.trim().length > 0

  return (
    <div className="space-y-8">
      <Section title="Basic info" subtitle="Give your database a name">
        <Field label="Database name" required>
          <input
            value={dbf.name}
            onChange={(e) => patchDbf({ name: e.target.value })}
            placeholder="my-postgres"
            className={inputCls}
            autoFocus
          />
        </Field>
      </Section>

      <Section title="Engine" subtitle="Choose the database engine and version">
        <div className="grid grid-cols-2 gap-3 mb-4">
          {ENGINE_OPTIONS.map((eng) => (
            <button
              key={eng.value}
              onClick={() => patchDbf({ engine: eng.value, version: eng.versions[0] })}
              className={cn(
                "flex items-center gap-2.5 px-3 py-2.5 rounded-lg border text-left transition-colors",
                dbf.engine === eng.value
                  ? "border-primary/50 bg-primary/10 text-foreground"
                  : "border-border/60 bg-muted/10 text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              <DbEngineLogo engine={eng.value} className="h-4 w-4 shrink-0" />
              <span className="text-sm font-medium">{eng.label}</span>
            </button>
          ))}
        </div>

        <Field label="Version">
          <Select value={dbf.version} onValueChange={(v) => patchDbf({ version: v ?? selectedEngine.versions[0] })}>
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {selectedEngine.versions.map((v) => (
                <SelectItem key={v} value={v}>{v}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </Section>

      <Section title="Credentials" subtitle="Leave blank to use the service name as db/user and auto-generate a password.">
        <div className="grid grid-cols-2 gap-4">
          <Field label="Database name">
            <input
              value={dbf.dbName}
              onChange={(e) => patchDbf({ dbName: e.target.value })}
              placeholder={dbf.name || "my-postgres"}
              className={inputCls}
            />
          </Field>
          <Field label="Username">
            <input
              value={dbf.dbUser}
              onChange={(e) => patchDbf({ dbUser: e.target.value })}
              placeholder={dbf.name || "my-postgres"}
              className={inputCls}
            />
          </Field>
        </div>
        <Field label="Password">
          <input
            value={dbf.dbPassword}
            onChange={(e) => patchDbf({ dbPassword: e.target.value })}
            placeholder="Auto-generated if left blank"
            className={inputCls}
            type="text"
          />
        </Field>
      </Section>

      <Section title="Storage" subtitle="Persistent volume size for this database">
        <Field label="Storage (GiB)">
          <Input
            type="number"
            min={1}
            max={1000}
            value={dbf.storageGB}
            onChange={(e) => patchDbf({ storageGB: Math.max(1, parseInt(e.target.value) || 10) })}
            className="h-9 text-sm"
          />
        </Field>
      </Section>

      <Section title="Placement" subtitle="Choose which node this database runs on">
        <div className="flex flex-wrap gap-2">
          <NodeCard
            label="Auto-schedule"
            sub="Let K3s decide"
            selected={dbf.nodeId === null}
            onClick={() => patchDbf({ nodeId: null })}
          />
          {workerNodes.map((node) => (
            <NodeCard
              key={node.id}
              label={node.name}
              sub={node.tailscaleIP}
              selected={dbf.nodeId === node.id}
              onClick={() => patchDbf({ nodeId: node.id })}
              online
            />
          ))}
        </div>
      </Section>

      {createMutation.isError && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
          <p className="text-xs text-destructive">{(createMutation.error as Error).message}</p>
        </div>
      )}

      <Button
        className="w-full gap-2"
        disabled={!canCreate || createMutation.isPending}
        onClick={() => createMutation.mutate()}
      >
        {createMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
        Create database
      </Button>
    </div>
  )
}

// ─── Route form ───────────────────────────────────────────────────────────────

type RouteZone = "public" | "internal"
type DomainMode = "subdomain" | "custom"
type TargetMode = "service" | "node"

interface RouteFormState {
  zone: RouteZone
  domainId: string
  domainMode: DomainMode
  subdomain: string
  customHostname: string
  targetMode: TargetMode
  serviceId: string
  nodeId: string
  port: string
}

const ROUTE_INITIAL: RouteFormState = {
  zone: "public",
  domainId: "",
  domainMode: "subdomain",
  subdomain: "",
  customHostname: "",
  targetMode: "service",
  serviceId: "",
  nodeId: "",
  port: "",
}

function RouteForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()

  const [rf, setRf] = useState<RouteFormState>(ROUTE_INITIAL)
  const patchRf = (p: Partial<RouteFormState>) => setRf((s) => ({ ...s, ...p }))

  const { data: domainList = [] } = useQuery<ApiDomain[]>({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const { data: serviceList = [] } = useQuery<ApiService[]>({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })

  const verifiedDomains = domainList.filter((d) => d.verified)
  const allNodes = rawNodes.filter((n) => n.status === "online")
  const gatewayNode = rawNodes.find((n) => n.k3s_role === "server")

  // Auto-select first verified domain
  useEffect(() => {
    if (verifiedDomains.length > 0 && !rf.domainId) {
      patchRf({ domainId: verifiedDomains[0].id })
    }
  }, [verifiedDomains.length]) // eslint-disable-line react-hooks/exhaustive-deps

  const selectedDomain = verifiedDomains.find((d) => d.id === rf.domainId)

  const hostnamePreview = (() => {
    if (rf.zone === "public" && rf.domainMode === "custom") {
      return rf.customHostname || "your-domain.com"
    }
    if (!selectedDomain || !rf.subdomain) return null
    if (rf.zone === "internal") {
      return `${rf.subdomain}.${selectedDomain.internal_subdomain}.${selectedDomain.base_domain}`
    }
    return `${rf.subdomain}.${selectedDomain.base_domain}`
  })()

  const canCreate =
    (rf.zone === "public" && rf.domainMode === "custom"
      ? rf.customHostname.trim().length > 0
      : rf.domainId.length > 0 && rf.subdomain.trim().length > 0) &&
    (rf.targetMode === "service"
      ? rf.serviceId.length > 0
      : rf.nodeId.length > 0 && rf.port.trim().length > 0)

  const createMutation = useMutation({
    mutationFn: () => {
      const body: Parameters<typeof routesApi.create>[2] = {
        zone: rf.zone,
      }
      if (rf.zone === "public" && rf.domainMode === "custom") {
        body.hostname = rf.customHostname
      } else {
        body.domain_id = rf.domainId
        body.subdomain = rf.subdomain
      }
      if (rf.targetMode === "service") {
        body.service_id = rf.serviceId
      } else {
        body.node_id = rf.nodeId
        body.port = parseInt(rf.port, 10)
      }
      return routesApi.create(orgId, projectId, body, token)
    },
    onSuccess: () => {
      navigate({ to: "/projects/$id/routes", params: { id: projectId } })
    },
  })

  return (
    <div className="space-y-8">
      {/* ── Section: Zone ───────────────────────────────────── */}
      <Section title="Zone" subtitle="Where is this route exposed?">
        <div className="flex rounded-lg border border-border/60 overflow-hidden w-fit">
          {(["public", "internal"] as RouteZone[]).map((z) => (
            <button
              key={z}
              onClick={() => patchRf({ zone: z, domainMode: "subdomain" })}
              className={cn(
                "px-4 py-2 text-sm capitalize transition-colors",
                rf.zone === z
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              {z}
            </button>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          {rf.zone === "public"
            ? "Accessible from the internet via your public domain."
            : "Only accessible within the mesh network via an internal subdomain."}
        </p>
      </Section>

      {/* ── Section: Domain ─────────────────────────────────── */}
      <Section
        title="Domain"
        subtitle={
          rf.zone === "internal"
            ? "Choose a domain and subdomain for internal routing."
            : "Choose a subdomain on a verified domain, or point a custom domain."
        }
      >
        {rf.zone === "public" && (
          <div className="flex rounded-lg border border-border/60 overflow-hidden w-fit mb-4">
            {(["subdomain", "custom"] as DomainMode[]).map((m) => (
              <button
                key={m}
                onClick={() => patchRf({ domainMode: m })}
                className={cn(
                  "px-4 py-2 text-sm transition-colors",
                  rf.domainMode === m
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
                )}
              >
                {m === "subdomain" ? "Subdomain" : "Custom domain"}
              </button>
            ))}
          </div>
        )}

        {rf.zone === "public" && rf.domainMode === "custom" ? (
          <div className="space-y-4">
            <Field label="Hostname" required>
              <input
                value={rf.customHostname}
                onChange={(e) => patchRf({ customHostname: e.target.value })}
                placeholder="app.example.com"
                className={inputCls}
              />
            </Field>
            {gatewayNode?.public_ip && (
              <div className="rounded-md border border-border/40 bg-muted/10 px-3 py-2.5 space-y-1">
                <p className="text-xs font-medium text-muted-foreground">DNS instruction</p>
                <p className="text-xs text-muted-foreground">
                  Create an <span className="font-mono text-foreground">A</span> record pointing{" "}
                  <span className="font-mono text-foreground">{rf.customHostname || "your-domain.com"}</span> →{" "}
                  <span className="font-mono text-foreground">{gatewayNode.public_ip}</span>
                </p>
              </div>
            )}
          </div>
        ) : (
          <div className="space-y-4">
            {verifiedDomains.length === 0 && (
              <p className="text-xs text-muted-foreground">No verified domains — add one in Domains first.</p>
            )}
            <Field label="Subdomain" required>
              <div className="flex items-center gap-0">
                <input
                  value={rf.subdomain}
                  onChange={(e) => patchRf({ subdomain: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "") })}
                  placeholder="api"
                  className={cn(inputCls, "rounded-r-none")}
                  disabled={!rf.domainId}
                />
                {selectedDomain && (
                  <span className="h-9 flex items-center px-3 text-sm text-muted-foreground bg-muted/20 border border-l-0 border-border/60 rounded-r-md whitespace-nowrap">
                    {rf.zone === "internal"
                      ? `.${selectedDomain.internal_subdomain}.${selectedDomain.base_domain}`
                      : `.${selectedDomain.base_domain}`}
                  </span>
                )}
              </div>
            </Field>

            {hostnamePreview && rf.subdomain && (
              <p className="text-xs text-muted-foreground">
                Route will be created for{" "}
                <span className="font-mono text-foreground">{hostnamePreview}</span>
              </p>
            )}
          </div>
        )}
      </Section>

      {/* ── Section: Target ─────────────────────────────────── */}
      <Section
        title="Target"
        subtitle="Where should traffic be forwarded?"
      >
        <div className="flex rounded-lg border border-border/60 overflow-hidden w-fit mb-4">
          {(["service", "node"] as TargetMode[]).map((m) => (
            <button
              key={m}
              onClick={() => patchRf({ targetMode: m, serviceId: "", nodeId: "", port: "" })}
              className={cn(
                "px-4 py-2 text-sm capitalize transition-colors",
                rf.targetMode === m
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              {m === "service" ? "Service" : "Node + port"}
            </button>
          ))}
        </div>

        {rf.targetMode === "service" ? (
          <div className="space-y-3">
            <Field label="Service" required>
              <Select
                value={rf.serviceId}
                onValueChange={(v) => patchRf({ serviceId: v ?? "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      serviceList.length === 0
                        ? "No services in this project"
                        : "Select a service…"
                    }
                  >
                    {serviceList.find((s) => s.id === rf.serviceId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {serviceList.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                      <span className="ml-2 text-muted-foreground text-xs">:{s.port}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            {rf.serviceId && (() => {
              const svc = serviceList.find((s) => s.id === rf.serviceId)
              return svc ? (
                <div className="rounded-md border border-border/40 bg-muted/10 px-3 py-2 flex items-center gap-3">
                  <span className="text-xs text-muted-foreground">Port</span>
                  <span className="font-mono text-xs text-foreground">{svc.port}</span>
                  <span className="text-xs text-muted-foreground ml-auto">
                    Configured on the service · change in service settings
                  </span>
                </div>
              ) : null
            })()}
          </div>
        ) : (
          <div className="grid grid-cols-[1fr_120px] gap-4">
            <Field label="Node" required>
              <Select
                value={rf.nodeId}
                onValueChange={(v) => patchRf({ nodeId: v ?? "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      allNodes.length === 0
                        ? "No online nodes"
                        : "Select a node…"
                    }
                  >
                    {allNodes.find((n) => n.id === rf.nodeId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {allNodes.map((n) => (
                    <SelectItem key={n.id} value={n.id}>
                      {n.name}
                      <span className="ml-2 text-muted-foreground text-xs">{n.tailscale_ip}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Port" required>
              <Input
                type="number"
                min={1}
                max={65535}
                value={rf.port}
                onChange={(e) => patchRf({ port: e.target.value })}
                placeholder="8080"
              />
            </Field>
          </div>
        )}
      </Section>

      {/* ── Error ────────────────────────────────────────────── */}
      {createMutation.error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
          <p className="text-xs text-destructive">{(createMutation.error as Error).message}</p>
        </div>
      )}

      {/* ── Submit ───────────────────────────────────────────── */}
      <Button
        className="w-full gap-2"
        disabled={!canCreate || createMutation.isPending}
        onClick={() => createMutation.mutate()}
      >
        {createMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
        Create route
      </Button>
    </div>
  )
}

// ─── Coming soon placeholder ──────────────────────────────────────────────────

function ComingSoonForm({ type }: { type: ResourceType }) {
  const label = RESOURCE_TYPES.find((r) => r.type === type)?.label ?? type
  return (
    <div className="flex flex-col items-center justify-center h-64 gap-3 text-center">
      <p className="text-sm text-muted-foreground">{label} creation is coming soon.</p>
      <span className="text-[10px] font-mono text-muted-foreground/50 border border-border/40 px-2 py-0.5 rounded-full">
        coming soon
      </span>
    </div>
  )
}

