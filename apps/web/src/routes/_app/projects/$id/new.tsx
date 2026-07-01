import { SiPostgresql, SiMysql, SiRedis, SiMongodb, SiClickhouse } from "@icons-pack/react-simple-icons"
import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState, useEffect } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  AlertTriangle,
  Box,
  ChevronLeft,
  ChevronDown,
  CornerDownRight,
  Database,
  Eye,
  EyeOff,
  Globe,
  HardDrive,
  Info,
  Layers,
  Loader2,
  Plus,
  Server,
  Trash2,
  Zap,
} from "lucide-react"
import { cn } from "@/lib/utils"
import CodeMirror from "@uiw/react-codemirror"
import { envLanguage, envTheme } from "@/lib/env-lang"
import { StreamLanguage } from "@codemirror/language"
import { shell } from "@codemirror/legacy-modes/mode/shell"
import { StackEditor } from "@/components/stacks/stack-editor"
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
  nodes as nodesApi,
  services as servicesApi,
  routes as routesApi,
  domains as domainsApi,
  jobs as jobsApi,
  stacks as stacksApi,
  templates as templatesApi,
  type TemplateManifest,
  volumes as volumesApi,
  variableGroups as groupsApi,
  gitIntegrations as gitApi,
  toNode,
  type CreateServiceBody,
  type ApiNode,
  type ApiService,
  type ApiDomain,
  type ApiVolume,
  type ApiDbRoute,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Section, Field, NodeCard } from "@/components/services/form-primitives"
import { Input } from "@/components/ui/input"
import { CronScheduleBlock } from "@/components/jobs/cron-schedule-block"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { SourceFields, type SourceState } from "@/components/services/source-fields"

// ─── Route ───────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/_app/projects/$id/new")({
  validateSearch: (search: Record<string, unknown>) => ({
    type: (search.type as ResourceType | undefined) ?? "service",
  }),
  component: NewResourcePage,
})

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = "service" | "route" | "job" | "database" | "stack" | "volume" | "variable-group"
type AppSource = "git" | "image"
type Builder = "railpack" | "dockerfile"

interface PortRow {
  name: string      // e.g. "http", "grpc"
  port: number
  isHTTP: boolean
  isPrimary: boolean
  isPublic: boolean
}

interface FormState {
  source: AppSource
  name: string
  image: string
  imageVisibility: "public" | "private"
  pullRegistryIntegrationId: string
  gitVisibility: "public" | "private"
  gitIntegrationId: string
  gitRepo: string
  gitBranch: string
  builder: Builder
  dockerfilePath: string
  registryIntegrationId: string
  nodeId: string | null
  builderNodeName: string | null  // k8s_node_name; null = auto-schedule
  builderCPURequest: string
  builderMemoryRequest: string
  ports: PortRow[]
  replicas: number
  cpuRequest: string
  cpuLimit: string
  memoryRequest: string
  memoryLimit: string
  showResources: boolean
  volumeAttachment: { volumeId: string; mountPath: string } | null
}

const INITIAL: FormState = {
  source: "git",
  name: "",
  image: "",
  imageVisibility: "public",
  pullRegistryIntegrationId: "",
  gitVisibility: "private",
  gitIntegrationId: "",
  gitRepo: "",
  gitBranch: "",
  builder: "railpack",
  dockerfilePath: "Dockerfile",
  registryIntegrationId: "",
  nodeId: null,
  builderNodeName: null,
  builderCPURequest: "1000m",
  builderMemoryRequest: "1Gi",
  ports: [{ name: "http", port: 3000, isHTTP: true, isPrimary: true, isPublic: true }],
  replicas: 1,
  cpuRequest: "100m",
  cpuLimit: "500m",
  memoryRequest: "128Mi",
  memoryLimit: "512Mi",
  showResources: false,
  volumeAttachment: null,
}

// ─── Sidebar resource types ───────────────────────────────────────────────────

const RESOURCE_TYPES: {
  type: ResourceType
  icon: typeof Box
  label: string
  soon?: boolean
  divider?: boolean
}[] = [
  { type: "service",  icon: Box,       label: "Service"  },
  { type: "database", icon: Database,  label: "Database" },
  { type: "route",    icon: Globe,     label: "Route"    },
  { type: "stack",    icon: Layers,    label: "Stack"    },
  { type: "volume",   icon: HardDrive, label: "Volume"   },
  { type: "variable-group", icon: Layers,    label: "Variable Group", divider: true },
  { type: "job",            icon: Zap,       label: "Job"             },
]

// ─── Page ─────────────────────────────────────────────────────────────────────

function NewResourcePage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/new" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()
  const qc = useQueryClient()

  const { type: resourceType } = Route.useSearch()
  const [form, setForm] = useState<FormState>(INITIAL)

  const patch = (partial: Partial<FormState>) =>
    setForm((s) => ({ ...s, ...partial }))

  const { data: project } = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const createMutation = useMutation({
    mutationFn: async () => {
      const body: CreateServiceBody = {
        name: form.name,
        ports: form.ports.map((p) => ({
          name: p.name,
          port: p.port,
          is_http: p.isHTTP,
          is_primary: p.isPrimary,
          is_public: p.isPublic,
        })),
        replicas: form.replicas,
        cpu_request: form.cpuRequest || undefined,
        cpu_limit: form.cpuLimit || undefined,
        memory_request: form.memoryRequest || undefined,
        memory_limit: form.memoryLimit || undefined,
        node_id: form.nodeId ?? undefined,
      }
      if (form.source === "image") {
        body.image = form.image
        if (form.imageVisibility === "private" && form.pullRegistryIntegrationId) {
          body.pull_registry_integration_id = form.pullRegistryIntegrationId
        }
      } else {
        if (form.gitVisibility === "private") {
          body.git_integration_id = form.gitIntegrationId || undefined
        }
        body.git_repo                 = form.gitRepo
        body.branch                   = form.gitBranch
        body.builder                  = form.builder
        body.dockerfile_path          = form.builder === "dockerfile" ? form.dockerfilePath : undefined
        body.registry_integration_id  = form.registryIntegrationId || undefined
        body.builder_node             = form.builderNodeName ?? ""
        body.builder_cpu_request      = form.builderCPURequest || undefined
        body.builder_memory_request   = form.builderMemoryRequest || undefined
      }
      const service = await servicesApi.create(orgId!, projectId, body, token)
      if (form.volumeAttachment) {
        await volumesApi.attach(orgId!, projectId, form.volumeAttachment.volumeId, { service_id: service.id, mount_path: form.volumeAttachment.mountPath }, token)
      }
      return service
    },
    onSuccess: (service) => {
      qc.invalidateQueries({ queryKey: ["services", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({
        to: "/projects/$id/services/$serviceId/deployments",
        params: { id: projectId, serviceId: service.id },
      })
    },
  })

  const canCreate =
    form.name.trim().length > 0 &&
    (form.source === "image"
      ? form.image.trim().length > 0 &&
        (form.imageVisibility === "public" || form.pullRegistryIntegrationId.length > 0)
      : form.gitRepo.length > 0 &&
        form.gitBranch.length > 0 &&
        form.registryIntegrationId.length > 0 &&
        (form.gitVisibility === "public" || form.gitIntegrationId.length > 0))

  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top bar */}
      <div className="sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <Button
            variant="ghost"
            onClick={() => navigate({ to: "/projects/$id/services", params: { id: projectId } })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            {project?.name ?? "Project"}
          </Button>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-sm font-medium">Create new</span>
        </div>
      </div>

      <div className="flex flex-1">
        {/* ─── Sidebar ─────────────────────────────────────────────── */}
        <aside className="w-52 shrink-0 border-r border-border/40 py-6 px-3 sticky top-14 h-[calc(100vh-3.5rem)] overflow-y-auto">
          <p className="text-[11px] font-medium text-muted-foreground/60 uppercase tracking-wider px-2 mb-2">
            Resource type
          </p>
          <nav className="space-y-0.5">
            {RESOURCE_TYPES.map(({ type, icon: Icon, label, soon, divider }) => (
              <div key={type}>
                {divider && <div className="my-1.5 border-t border-border/30" />}
                <Button
                  variant="ghost"
                  onClick={() => !soon && navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type }, replace: true })}
                  disabled={soon}
                  className={cn(
                    "w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm transition-colors text-left",
                    resourceType === type && !soon
                      ? "bg-primary/10 text-primary hover:bg-primary/10 hover:text-primary dark:hover:bg-primary/10"
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
                </Button>
              </div>
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
              projectId={projectId}
            />
          ) : resourceType === "stack" ? (
            <StackForm projectId={projectId} />
          ) : resourceType === "database" ? (
            <DatabaseForm projectId={projectId} />
          ) : resourceType === "route" ? (
            <RouteForm projectId={projectId} />
          ) : resourceType === "job" ? (
            <JobForm projectId={projectId} />
          ) : resourceType === "volume" ? (
            <VolumeForm projectId={projectId} />
          ) : resourceType === "variable-group" ? (
            <VariableGroupForm projectId={projectId} />
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
  projectId,
}: {
  form: FormState
  patch: (p: Partial<FormState>) => void
  canCreate: boolean
  isPending: boolean
  error: Error | null
  onCreate: () => void
  projectId: string
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  // Local state for the add-volume picker
  const [pendingVolumeId, setPendingVolumeId] = useState("")
  const [pendingMountPath, setPendingMountPath] = useState("")

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

  const { data: projectVolumes = [] } = useQuery<ApiVolume[]>({
    queryKey: ["volumes", orgId, projectId],
    queryFn: () => volumesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const readyVolumes = projectVolumes.filter((v) => v.mounts?.length === 0)

  function attachVolume() {
    if (!pendingVolumeId || !pendingMountPath.trim()) return
    patch({ volumeAttachment: { volumeId: pendingVolumeId, mountPath: pendingMountPath.trim() } })
  }

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
        <SourceFields
          value={form as SourceState}
          onChange={patch}
        />
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

        {/* Ports */}
        <Field label="Ports" required>
          <div className="space-y-2">
            {form.ports.map((p, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input
                  placeholder="name"
                  className="w-24 text-xs"
                  value={p.name}
                  onChange={(e) => {
                    const next = [...form.ports]
                    next[i] = { ...next[i], name: e.target.value }
                    patch({ ports: next })
                  }}
                />
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  placeholder="port"
                  className="w-24 text-xs"
                  value={p.port}
                  onChange={(e) => {
                    const next = [...form.ports]
                    next[i] = { ...next[i], port: parseInt(e.target.value) || 3000 }
                    patch({ ports: next })
                  }}
                />
                <label className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={p.isHTTP}
                    onChange={(e) => {
                      const next = [...form.ports]
                      next[i] = { ...next[i], isHTTP: e.target.checked }
                      patch({ ports: next })
                    }}
                    className="rounded"
                  />
                  HTTP
                </label>
                <label className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={p.isPublic}
                    onChange={(e) => {
                      const next = [...form.ports]
                      next[i] = { ...next[i], isPublic: e.target.checked }
                      patch({ ports: next })
                    }}
                    className="rounded"
                  />
                  Public
                </label>
                <label className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer select-none">
                  <input
                    type="radio"
                    name="primary-port"
                    checked={p.isPrimary}
                    onChange={() => {
                      const next = form.ports.map((r, j) => ({ ...r, isPrimary: j === i }))
                      patch({ ports: next })
                    }}
                    className="rounded"
                  />
                  Primary
                </label>
                {form.ports.length > 1 && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-7 w-7 text-muted-foreground hover:text-destructive shrink-0"
                    onClick={() => patch({ ports: form.ports.filter((_, j) => j !== i) })}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs text-muted-foreground gap-1.5 px-2"
              onClick={() => patch({ ports: [...form.ports, { name: "", port: 8080, isHTTP: false, isPrimary: false, isPublic: false }] })}
            >
              <Plus className="h-3.5 w-3.5" />
              Add port
            </Button>
          </div>
        </Field>

        {/* Replicas */}
        <Field label="Replicas">
          <Input
            type="number"
            min={1}
            max={20}
            value={form.replicas}
            onChange={(e) => patch({ replicas: Math.max(1, parseInt(e.target.value) || 1) })}
          />
        </Field>

        {/* Resource limits (collapsible) */}
        <div className="rounded-lg border border-border/40 -mx-0">
          <Button
            variant="ghost"
            onClick={() => patch({ showResources: !form.showResources })}
            className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <span className="font-medium">Resource limits</span>
            <ChevronDown className={cn("h-4 w-4 transition-transform", form.showResources ? "rotate-180" : "")} />
          </Button>
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
      </Section>


      {/* ── Section: Volume ───────────────────────────────────── */}
      <Section title="Volume" subtitle="Mount a persistent volume into this service.">
        {form.volumeAttachment ? (
          <div className="flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5">
            <div className="flex items-center gap-2 text-xs min-w-0">
              <HardDrive className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
              <span className="text-muted-foreground truncate">
                {projectVolumes.find((v) => v.id === form.volumeAttachment!.volumeId)?.name ?? form.volumeAttachment.volumeId}
              </span>
              <span className="text-muted-foreground/50">→</span>
              <code className="font-mono text-foreground truncate">{form.volumeAttachment.mountPath}</code>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => patch({ volumeAttachment: null })}
              className="ml-3 text-muted-foreground/40 hover:text-destructive transition-colors shrink-0"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ) : readyVolumes.length === 0 ? (
          <p className="text-xs text-muted-foreground/60">
            {projectVolumes.length === 0
              ? "No volumes in this project yet. Create one from the Volumes tab."
              : "All volumes are already attached to other services."}
          </p>
        ) : (
          <div className="space-y-3">
            <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2.5">
              <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
              <p className="text-xs text-amber-300/80">
                Attaching a volume scales this service down to 1 replica. To scale out, detach the volume first.
              </p>
            </div>
            <div className="flex gap-2">
              <Select value={pendingVolumeId} onValueChange={(v) => setPendingVolumeId(v ?? "")}>
                <SelectTrigger className={cn(inputCls, "flex-1")}>
                  <SelectValue placeholder="Select a volume…" />
                </SelectTrigger>
                <SelectContent>
                  {readyVolumes.map((v) => (
                    <SelectItem key={v.id} value={v.id}>{v.name} ({v.storage_gb} GB)</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <input
                className={cn(inputCls, "flex-1 font-mono")}
                placeholder="/data"
                value={pendingMountPath}
                onChange={(e) => setPendingMountPath(e.target.value)}
              />
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={!pendingVolumeId || !pendingMountPath.trim()}
                onClick={attachVolume}
              >
                <Plus className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}
      </Section>

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

type DbEngine = "postgres" | "mysql" | "redis" | "mongodb" | "dragonfly" | "clickhouse"

function DbEngineLogo({ engine, className }: { engine: DbEngine; className?: string }) {
  if (engine === "dragonfly") return (
    <svg viewBox="190 0 220 150" className={className} fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M293.371 120.52L295.001 143.333C295.001 143.333 295.834 146.667 300.001 146.667C304.167 146.667 305.001 143.333 305.001 143.333L306.63 120.52C306.57 120.545 306.509 120.57 306.447 120.595C304.969 121.186 302.877 121.667 300.001 121.667C297.125 121.667 295.033 121.186 293.555 120.595C293.492 120.57 293.431 120.545 293.371 120.52Z" fill="url(#df0)"/>
      <path d="M290.071 79.1411L293.334 116.667C293.334 116.667 295.001 118.333 300.001 118.333C305.001 118.333 306.668 116.667 306.668 116.667L309.931 79.1409C307.884 80.3319 304.562 81.6667 300.001 81.6667C295.438 81.6667 292.117 80.3315 290.071 79.1411Z" fill="url(#df0)"/>
      <path d="M288.725 42.2741C288.189 42.6515 287.764 43.187 287.523 43.8296L284.163 52.7901C283.626 54.2199 283.597 55.7904 284.08 57.2391L289.503 73.507C289.83 74.4888 290.378 75.3913 291.247 75.9534C292.77 76.9385 295.688 78.3333 300.001 78.3333C304.313 78.3333 307.231 76.9385 308.754 75.9534C309.623 75.3913 310.171 74.4888 310.498 73.507L315.921 57.2391C316.404 55.7904 316.375 54.2199 315.838 52.7901L312.478 43.8296C312.237 43.187 311.812 42.6515 311.276 42.2741C308.308 45.0015 304.349 46.6667 300.001 46.6667C295.652 46.6667 291.693 45.0015 288.725 42.2741Z" fill="url(#df0)"/>
      <path d="M286.667 30C286.667 28.0055 285.484 26.0403 283.724 25.1001C279.525 22.8554 276.667 18.428 276.667 13.3333C276.667 5.96954 282.637 0 290.001 0C291.287 0 292.531 0.182103 293.707 0.521932C297.56 1.63468 302.441 1.63467 306.294 0.52193C307.471 0.182102 308.715 0 310.001 0C317.365 0 323.334 5.96954 323.334 13.3333C323.334 18.428 320.477 22.8554 316.277 25.1001C314.518 26.0403 313.334 28.0055 313.334 30C313.334 37.3638 307.365 43.3333 300.001 43.3333C292.637 43.3333 286.667 37.3638 286.667 30Z" fill="url(#df0)"/>
      <path d="M215.001 50H281.334C278.334 55 280.509 58.193 281.667 61.6667L219.391 79.5014C215.414 80.6216 211.158 79.1811 208.679 75.876L204.76 70.6504C202.488 67.6202 202.122 63.5661 203.816 60.1782L206.141 55.5275C207.822 52.1671 211.244 50.032 215.001 50Z" fill="url(#df0)"/>
      <path d="M282.501 46.6667L206.667 44.8876C202.434 44.7111 198.73 42.2702 197.484 38.2209L193.777 26.2459C193.481 25.285 193.337 24.3042 193.334 23.3333C193.326 20.2745 194.724 17.3136 197.234 15.3814L204.884 9.49196C207.751 7.28419 211.588 6.79807 214.916 8.22088C223.481 11.7497 274.875 33.9566 283.709 37.7748C284.467 38.1024 284.844 38.9237 284.641 39.7242C284.498 40.2938 284.328 40.9799 284.167 41.6667C283.698 43.6708 282.501 46.6667 282.501 46.6667Z" fill="url(#df0)"/>
      <path d="M385.001 50H318.667C321.667 55 319.492 58.193 318.334 61.6667L380.61 79.5014C384.587 80.6216 388.843 79.1811 391.322 75.876L395.241 70.6504C397.514 67.6202 397.879 63.5661 396.185 60.1783L393.86 55.5275C392.18 52.1671 388.758 50.032 385.001 50Z" fill="url(#df0)"/>
      <path d="M317.501 46.6667L393.334 44.8876C397.567 44.7112 401.271 42.2702 402.517 38.2209L406.225 26.2459C406.52 25.285 406.665 24.3043 406.667 23.3333C406.676 20.2745 405.277 17.3136 402.767 15.3814L395.118 9.49198C392.25 7.28421 388.413 6.79809 385.086 8.22091C376.52 11.7497 325.126 33.9566 316.293 37.7748C315.535 38.1024 315.158 38.9237 315.36 39.7242C315.504 40.2938 315.673 40.9799 315.834 41.6667C316.303 43.6708 317.501 46.6667 317.501 46.6667Z" fill="url(#df0)"/>
      <defs>
        <linearGradient id="df0" x1="300" y1="0" x2="300" y2="147" gradientUnits="userSpaceOnUse">
          <stop stopColor="#5A3EE0"/>
          <stop offset="1" stopColor="#3E74E0"/>
        </linearGradient>
      </defs>
    </svg>
  )
  const icons: Record<string, React.ComponentType<{ className?: string }>> = {
    postgres: SiPostgresql,
    mysql: SiMysql,
    redis: SiRedis,
    mongodb: SiMongodb,
    clickhouse: SiClickhouse,
  }
  const Icon = icons[engine]
  return Icon ? <Icon className={className} /> : null
}

const ENGINE_OPTIONS: { value: DbEngine; label: string; versions: string[]; defaultPort: number }[] = [
  { value: "postgres",   label: "PostgreSQL",  versions: ["17", "16", "15", "14", "13"], defaultPort: 5432 },
  { value: "mysql",      label: "MySQL",       versions: ["8.0", "5.7"],                 defaultPort: 3306 },
  { value: "redis",      label: "Redis",       versions: ["7", "6"],                     defaultPort: 6379 },
  { value: "mongodb",    label: "MongoDB",     versions: ["7", "6"],                     defaultPort: 27017 },
  { value: "dragonfly",  label: "Dragonfly",   versions: ["latest"],                     defaultPort: 6379 },
  { value: "clickhouse", label: "ClickHouse",  versions: ["24", "23"],                   defaultPort: 9000 },
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
  const qc = useQueryClient()

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
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["services", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
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
        <div className="grid grid-cols-3 gap-3 mb-4">
          {ENGINE_OPTIONS.map((eng) => (
            <Button
              key={eng.value}
              variant="ghost"
              onClick={() => patchDbf({ engine: eng.value, version: eng.versions[0] })}
              className={cn(
                "flex items-center gap-2.5 px-3 py-2.5 rounded-lg border text-left transition-colors",
                dbf.engine === eng.value
                  ? "border-primary/50 bg-primary/10 text-foreground hover:bg-primary/10 hover:text-foreground dark:hover:bg-primary/10"
                  : "border-border/60 bg-muted/10 text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              <DbEngineLogo engine={eng.value} className="h-4 w-4 shrink-0" />
              <span className="text-sm font-medium">{eng.label}</span>
            </Button>
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

const RESERVED_SUBDOMAINS = new Set([
  // Meshploy infrastructure
  "console", "api", "mesh", "headscale", "preview", "internal",
  // Standard DNS / internet conventions
  "www", "mail", "smtp", "mx", "ns", "ns1", "ns2",
  // Bare wildcard
  "*",
])

type RouteZone = "public" | "internal"
type DomainMode = "subdomain" | "custom"
type TargetMode = "service" | "node" | "redirect"

interface TargetRow {
  id: number
  targetMode: TargetMode
  serviceId: string
  servicePortId: string
  nodeId: string
  port: string
  path: string
  stripPath: boolean
  redirectRouteId: string
  redirectCode: "301" | "302"
}

let _targetRowId = 0
const mkTargetRow = (): TargetRow => ({
  id: ++_targetRowId,
  targetMode: "service",
  serviceId: "",
  servicePortId: "",
  nodeId: "",
  port: "",
  path: "/",
  stripPath: false,
  redirectRouteId: "",
  redirectCode: "301",
})

interface RouteFormState {
  zone: RouteZone
  domainId: string
  domainMode: DomainMode
  subdomain: string
  customHostname: string
  targets: TargetRow[]
}

const ROUTE_INITIAL: RouteFormState = {
  zone: "public",
  domainId: "",
  domainMode: "subdomain",
  subdomain: "",
  customHostname: "",
  targets: [mkTargetRow()],
}

function RouteForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [rf, setRf] = useState<RouteFormState>(ROUTE_INITIAL)
  const patchRf = (p: Partial<RouteFormState>) => setRf((s) => ({ ...s, ...p }))

  const { data: domainList = [] } = useQuery<ApiDomain[]>({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const { data: allServices = [] } = useQuery<ApiService[]>({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })
  const serviceList = allServices.filter((s) => s.type === "application")

  const { data: existingRoutes = [] } = useQuery<ApiDbRoute[]>({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId, projectId, token),
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

  const patchTarget = (id: number, patch: Partial<TargetRow>) =>
    patchRf({ targets: rf.targets.map((t) => (t.id === id ? { ...t, ...patch } : t)) })
  const addTarget = () => patchRf({ targets: [...rf.targets, mkTargetRow()] })
  const removeTarget = (id: number) =>
    patchRf({ targets: rf.targets.length > 1 ? rf.targets.filter((t) => t.id !== id) : rf.targets })

  const subdomainReserved =
    rf.domainMode !== "custom" &&
    rf.subdomain.trim().length > 0 &&
    (RESERVED_SUBDOMAINS.has(rf.subdomain) ||
      (selectedDomain != null &&
        (rf.subdomain === selectedDomain.internal_subdomain ||
          rf.subdomain === selectedDomain.preview_subdomain)))

  const subdomainFormatError =
    rf.domainMode !== "custom" &&
    rf.subdomain.includes("*") &&
    !/^\*\.[a-z0-9-]+$/.test(rf.subdomain)
      ? "Wildcard must be in the format *.label (e.g. *.my-app)"
      : null

  const domainValid =
    rf.zone === "public" && rf.domainMode === "custom"
      ? rf.customHostname.trim().length > 0
      : rf.domainId.length > 0 &&
        rf.subdomain.trim().length > 0 &&
        !subdomainReserved &&
        !subdomainFormatError
  const targetsValid = rf.targets.every((t) =>
    t.path.trim().startsWith("/") &&
    (t.targetMode === "service"
      ? t.serviceId.length > 0
      : t.targetMode === "redirect"
      ? t.redirectRouteId.length > 0
      : t.nodeId.length > 0 && t.port.trim().length > 0)
  )
  const canCreate = domainValid && targetsValid && rf.targets.length > 0

  const createMutation = useMutation({
    mutationFn: () => {
      const body: Parameters<typeof routesApi.create>[2] = {
        zone: rf.zone,
        targets: rf.targets.map((t) => ({
          path: t.path || "/",
          strip_path: t.stripPath,
          ...(t.targetMode === "service"
            ? { service_id: t.serviceId, ...(t.servicePortId ? { service_port_id: t.servicePortId } : {}) }
            : t.targetMode === "redirect"
            ? { redirect_route_id: t.redirectRouteId, redirect_code: parseInt(t.redirectCode, 10) }
            : { node_id: t.nodeId, port: parseInt(t.port, 10) }),
        })),
      }
      if (rf.zone === "public" && rf.domainMode === "custom") {
        body.hostname = rf.customHostname
      } else {
        body.domain_id = rf.domainId
        body.subdomain = rf.subdomain
      }
      return routesApi.create(orgId, projectId, body, token)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({ to: "/projects/$id/routes", params: { id: projectId } })
    },
  })

  return (
    <div className="space-y-8">
      {/* ── Section: Zone ───────────────────────────────────── */}
      <Section title="Zone" subtitle="Where is this route exposed?">
        <SegmentedControl
          value={rf.zone}
          onValueChange={(v) => {
            const newZone = v as RouteZone
            patchRf({
              zone: newZone,
              domainMode: "subdomain",
              targets: newZone === "internal"
                ? rf.targets.map((t) => t.targetMode === "redirect" ? { ...t, targetMode: "service" as TargetMode, redirectRouteId: "", redirectCode: "301" as const } : t)
                : rf.targets,
            })
          }}
          options={[
            { value: "public",   label: "Public" },
            { value: "internal", label: "Internal" },
          ]}
          className="text-sm"
        />
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
          <SegmentedControl
            value={rf.domainMode}
            onValueChange={(v) => patchRf({ domainMode: v as DomainMode })}
            options={[
              { value: "subdomain", label: "Subdomain" },
              { value: "custom",    label: "Custom domain" },
            ]}
            className="text-sm mb-4"
          />
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
                  onChange={(e) => patchRf({ subdomain: e.target.value.toLowerCase().replace(/[^a-z0-9\-.*]/g, "") })}
                  placeholder="api or *.my-app"
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

            {subdomainReserved && (
              <p className="text-xs text-destructive">
                Subdomain <span className="font-mono">{rf.subdomain}</span> is reserved and cannot be used.
              </p>
            )}
            {subdomainFormatError && (
              <p className="text-xs text-destructive">{subdomainFormatError}</p>
            )}
            {hostnamePreview && rf.subdomain && !subdomainReserved && !subdomainFormatError && (
              <p className="text-xs text-muted-foreground">
                Route will be created for{" "}
                <span className="font-mono text-foreground">{hostnamePreview}</span>
              </p>
            )}
          </div>
        )}
      </Section>

      {/* ── Section: Targets ────────────────────────────────── */}
      <Section title="Targets" subtitle="Map path prefixes to services or nodes. Longest path matches first.">
        <div className="space-y-3">
          {rf.targets.map((t) => (
            <TargetRowField
              key={t.id}
              row={t}
              zone={rf.zone}
              serviceList={serviceList}
              nodeList={allNodes}
              routeList={existingRoutes}
              onChange={(patch) => patchTarget(t.id, patch)}
              onRemove={rf.targets.length > 1 ? () => removeTarget(t.id) : undefined}
            />
          ))}
          <Button
            variant="ghost"
            onClick={addTarget}
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <Plus className="h-3.5 w-3.5" />
            Add another target
          </Button>
        </div>
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

// ─── TargetRowField ───────────────────────────────────────────────────────────

function TargetRowField({
  row,
  zone,
  serviceList,
  nodeList,
  routeList,
  onChange,
  onRemove,
}: {
  row: TargetRow
  zone: RouteZone
  serviceList: ApiService[]
  nodeList: ApiNode[]
  routeList: ApiDbRoute[]
  onChange: (patch: Partial<TargetRow>) => void
  onRemove?: () => void
}) {
  const modeOptions = [
    { value: "service",  label: "Service" },
    { value: "node",     label: "Node + port" },
    ...(zone !== "internal" ? [{ value: "redirect", label: "Redirect" }] : []),
  ]

  return (
    <div className="rounded-md border border-border/60 bg-muted/10 p-3 space-y-3">
      {/* Row 1: type toggle + path + strip path + remove */}
      <div className="flex items-center gap-2">
        <SegmentedControl
          value={row.targetMode}
          onValueChange={(v) => onChange({ targetMode: v as TargetMode, serviceId: "", servicePortId: "", nodeId: "", port: "", redirectRouteId: "", redirectCode: "301" })}
          options={modeOptions}
          className="text-xs shrink-0"
        />
        <input
          value={row.path}
          onChange={(e) => onChange({ path: e.target.value })}
          placeholder="/path"
          className={cn(inputCls, "font-mono text-xs w-28 shrink-0")}
        />
        {row.targetMode !== "redirect" && (
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground select-none cursor-pointer ml-auto shrink-0">
            <input
              type="checkbox"
              checked={row.stripPath}
              onChange={(e) => onChange({ stripPath: e.target.checked })}
              className="accent-primary"
            />
            Strip path
          </label>
        )}
        {row.targetMode === "redirect" && <div className="ml-auto" />}
        {onRemove && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onRemove}
            className="text-muted-foreground hover:text-destructive transition-colors shrink-0"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>

      {/* Row 2: service, node+port, or redirect */}
      {row.targetMode === "service" ? (
        <div className="space-y-2">
          <Select
            value={row.serviceId}
            onValueChange={(v) => onChange({ serviceId: v ?? "", servicePortId: "" })}
          >
            <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
              <SelectValue placeholder={serviceList.length === 0 ? "No services in this project" : "Select a service…"}>
                {serviceList.find((s) => s.id === row.serviceId)?.name}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {serviceList.map((s) => (
                <SelectItem key={s.id} value={s.id}>
                  {s.name}
                  <span className="ml-2 text-muted-foreground text-xs">:{(s.ports?.find((p) => p.is_primary) ?? s.ports?.[0])?.port ?? "?"}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {(() => {
            const svc = serviceList.find((s) => s.id === row.serviceId)
            const publicHTTP = svc?.ports?.filter((p) => p.is_public && p.is_http) ?? []
            if (publicHTTP.length < 2) return null
            return (
              <Select
                value={row.servicePortId || "__primary__"}
                onValueChange={(v) => onChange({ servicePortId: !v || v === "__primary__" ? "" : v })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
                  <SelectValue placeholder="Port (primary)">
                    {row.servicePortId
                      ? publicHTTP.find((p) => p.id === row.servicePortId)?.name ?? "Port"
                      : "Primary port"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__primary__">Primary port</SelectItem>
                  {publicHTTP.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                      <span className="ml-2 text-muted-foreground text-xs">:{p.port}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )
          })()}
        </div>
      ) : row.targetMode === "redirect" ? (
        <div className="flex items-center gap-2">
          <CornerDownRight className="h-3.5 w-3.5 text-amber-400 shrink-0" />
          <Select value={row.redirectRouteId} onValueChange={(v) => onChange({ redirectRouteId: v ?? "" })}>
            <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
              <SelectValue placeholder={routeList.length === 0 ? "No routes in this project yet" : "Select a route to redirect to…"}>
                {routeList.find((r) => r.id === row.redirectRouteId)?.hostname}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {routeList.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.hostname}
                  <span className="ml-2 text-muted-foreground text-xs">{r.zone}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <SegmentedControl
            value={row.redirectCode}
            onValueChange={(v) => onChange({ redirectCode: v as "301" | "302" })}
            options={[
              { value: "301", label: "301" },
              { value: "302", label: "302" },
            ]}
            className="text-xs shrink-0"
          />
        </div>
      ) : (
        <div className="grid grid-cols-[1fr_100px] gap-2">
          <Select value={row.nodeId} onValueChange={(v) => onChange({ nodeId: v ?? "" })}>
            <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
              <SelectValue placeholder={nodeList.length === 0 ? "No online nodes" : "Select a node…"}>
                {nodeList.find((n) => n.id === row.nodeId)?.name}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {nodeList.map((n) => (
                <SelectItem key={n.id} value={n.id}>
                  {n.name}
                  <span className="ml-2 text-muted-foreground text-xs">{n.tailscale_ip}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            type="number"
            min={1}
            max={65535}
            value={row.port}
            onChange={(e) => onChange({ port: e.target.value })}
            placeholder="8080"
          />
        </div>
      )}
    </div>
  )
}

// ─── Job / Cron Job form ─────────────────────────────────────────────────────

interface JobFormState {
  name: string
  image: string
  command: string
  isScheduled: boolean
  schedule: string
  concurrencyPolicy: string
  historyLimit: number
  envVars: string
  nodeId: string | null
  showResources: boolean
  cpuRequest: string
  cpuLimit: string
  memoryRequest: string
  memoryLimit: string
}

const CRON_PRESETS = [
  { label: "Every 5 min", value: "*/5 * * * *" },
  { label: "Hourly",      value: "0 * * * *"   },
  { label: "Daily",       value: "0 0 * * *"   },
  { label: "Weekly",      value: "0 0 * * 0"   },
  { label: "Monthly",     value: "0 0 1 * *"   },
]


const JOB_INITIAL: JobFormState = {
  name: "",
  image: "",
  command: "",
  isScheduled: false,
  schedule: "",
  concurrencyPolicy: "allow",
  historyLimit: 5,
  envVars: "",
  nodeId: null,
  showResources: false,
  cpuRequest: "100m",
  cpuLimit: "500m",
  memoryRequest: "128Mi",
  memoryLimit: "512Mi",
}

function JobForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [jf, setJf] = useState<JobFormState>(JOB_INITIAL)
  const patch = (p: Partial<JobFormState>) => setJf((s) => ({ ...s, ...p }))

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const workerNodes = rawNodes
    .filter((n) => n.k8s_member && n.status === "online" && n.k3s_role === "agent")
    .map(toNode)

  const createMutation = useMutation({
    mutationFn: () =>
      jobsApi.create(orgId, projectId, {
        name: jf.name,
        is_cron: jf.isScheduled,
        image: jf.image,
        command: jf.command || undefined,
        schedule: jf.isScheduled ? jf.schedule : undefined,
        concurrency_policy: jf.isScheduled ? jf.concurrencyPolicy : undefined,
        history_limit: jf.isScheduled ? jf.historyLimit : undefined,
        cpu_request: jf.cpuRequest || undefined,
        cpu_limit: jf.cpuLimit || undefined,
        memory_request: jf.memoryRequest || undefined,
        memory_limit: jf.memoryLimit || undefined,
        env_vars: jf.envVars || undefined,
        node_id: jf.nodeId ?? undefined,
      }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({ to: "/projects/$id/jobs", params: { id: projectId } })
    },
  })

  const canCreate =
    jf.name.trim().length > 0 &&
    jf.image.trim().length > 0 &&
    (!jf.isScheduled || jf.schedule.trim().length > 0)

  return (
    <div className="space-y-8">
      <Section title="Basic info" subtitle="Give your job a name">
        <Field label="Name" required>
          <input
            value={jf.name}
            onChange={(e) => patch({ name: e.target.value })}
            placeholder={jf.isScheduled ? "nightly-cleanup" : "db-migrate"}
            className={inputCls}
            autoFocus
          />
        </Field>
      </Section>

      <Section title="Image" subtitle="Docker image to run">
        <Field label="Image" required>
          <input
            value={jf.image}
            onChange={(e) => patch({ image: e.target.value })}
            placeholder="alpine:latest"
            className={inputCls}
          />
        </Field>
        <Field label="Script">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={jf.command}
              height="160px"
              theme="dark"
              extensions={[StreamLanguage.define(shell)]}
              onChange={(val) => patch({ command: val })}
              placeholder={"#!/bin/sh\nset -e\n\necho 'Running job…'"}
              style={{ fontSize: 13 }}
              basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
            />
          </div>
        </Field>
      </Section>

      <CronScheduleBlock
        enabled={jf.isScheduled}
        onToggle={() => patch({ isScheduled: !jf.isScheduled })}
        schedule={jf.schedule}
        onScheduleChange={(v) => patch({ schedule: v })}
        concurrency={jf.concurrencyPolicy}
        onConcurrencyChange={(v) => patch({ concurrencyPolicy: v })}
        historyLimit={String(jf.historyLimit)}
        onHistoryLimitChange={(v) => patch({ historyLimit: parseInt(v) || 5 })}
      />

      <Section title="Environment" subtitle="Variables injected at runtime. One KEY=VALUE per line.">
        <Field label="Env vars">
          <div className="rounded-md overflow-hidden border border-border/60">
            <CodeMirror
              value={jf.envVars}
              height="120px"
              theme="dark"
              extensions={[envLanguage, envTheme]}
              onChange={(val) => patch({ envVars: val })}
              placeholder={"DATABASE_URL=postgres://...\nDEBUG=true"}
              style={{ fontSize: 13 }}
              basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
            />
          </div>
        </Field>
      </Section>

      <Section title="Placement" subtitle="Choose which node this job runs on">
        <div className="flex flex-wrap gap-2">
          <NodeCard
            label="Auto-schedule"
            sub="Let K3s decide"
            selected={jf.nodeId === null}
            onClick={() => patch({ nodeId: null })}
          />
          {workerNodes.map((node) => (
            <NodeCard
              key={node.id}
              label={node.name}
              sub={node.tailscaleIP}
              selected={jf.nodeId === node.id}
              onClick={() => patch({ nodeId: node.id })}
              online
            />
          ))}
        </div>
      </Section>

      <div className="rounded-lg border border-border/40">
        <Button
          variant="ghost"
          onClick={() => patch({ showResources: !jf.showResources })}
          className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <span className="font-medium">Resource limits</span>
          <ChevronDown className={cn("h-4 w-4 transition-transform", jf.showResources ? "rotate-180" : "")} />
        </Button>
        {jf.showResources && (
          <div className="px-4 pb-4 pt-0 grid grid-cols-2 gap-4 border-t border-border/40">
            <Field label="CPU request">
              <input value={jf.cpuRequest} onChange={(e) => patch({ cpuRequest: e.target.value })} className={inputCls} />
            </Field>
            <Field label="CPU limit">
              <input value={jf.cpuLimit} onChange={(e) => patch({ cpuLimit: e.target.value })} className={inputCls} />
            </Field>
            <Field label="Memory request">
              <input value={jf.memoryRequest} onChange={(e) => patch({ memoryRequest: e.target.value })} className={inputCls} />
            </Field>
            <Field label="Memory limit">
              <input value={jf.memoryLimit} onChange={(e) => patch({ memoryLimit: e.target.value })} className={inputCls} />
            </Field>
          </div>
        )}
      </div>

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
        Create {jf.isScheduled ? "cron job" : "job"}
      </Button>
    </div>
  )
}

// ─── Stack form ───────────────────────────────────────────────────────────────

const DEFAULT_STACK_SPEC = `services:
  web:
    image: ""
    x-meshploy:
      source:
        git: ""
        branch: main
      build:
        builder: railpack
      deploy:
        replicas: 1
        port: 3000
`

type StackSourceMode = "raw" | "git" | "template"
type StackGitVisibility = "public" | "private"
type StackFetchMode = "file" | "repo"

function StackForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [name, setName] = useState("")
  const [spec, setSpec] = useState(DEFAULT_STACK_SPEC)

  // Git source state
  const [sourceMode, setSourceMode] = useState<StackSourceMode>("raw")
  const [gitVisibility, setGitVisibility] = useState<StackGitVisibility>("public")
  const [gitIntegrationId, setGitIntegrationId] = useState("")
  const [gitRepo, setGitRepo] = useState("")
  const [gitBranch, setGitBranch] = useState("main")
  const [gitPath, setGitPath] = useState("docker-compose.yml")
  const [fetchMode, setFetchMode] = useState<StackFetchMode>("file")

  const { data: gitList = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId!, token),
    enabled: !!orgId && sourceMode === "git" && gitVisibility === "private",
  })
  const connectedGit = gitList.filter((g) => g.connected)

  const { data: repoList = [], isFetching: reposFetching } = useQuery({
    queryKey: ["git-repos", orgId, gitIntegrationId],
    queryFn: () => gitApi.repos(orgId!, gitIntegrationId, token),
    enabled: !!gitIntegrationId,
    staleTime: 5 * 60 * 1000,
  })

  const { data: branchList = [], isFetching: branchesFetching } = useQuery({
    queryKey: ["git-branches", orgId, gitIntegrationId, gitRepo],
    queryFn: () => gitApi.branches(orgId!, gitIntegrationId, gitRepo, token),
    enabled: !!gitIntegrationId && !!gitRepo,
    staleTime: 2 * 60 * 1000,
  })

  // Template source state
  const [templateId, setTemplateId] = useState("")
  const [promptValues, setPromptValues] = useState<Record<string, string>>({})

  const { data: templateList = [] } = useQuery({
    queryKey: ["templates"],
    queryFn: () => templatesApi.list(token),
    enabled: sourceMode === "template",
  })

  const { data: templateDetail } = useQuery({
    queryKey: ["template", templateId],
    queryFn: () => templatesApi.get(templateId, token),
    enabled: sourceMode === "template" && !!templateId,
  })

  // Prefill the editor with the template's compose when a template is selected.
  useEffect(() => {
    if (templateDetail) setSpec(templateDetail.compose)
  }, [templateDetail])

  const promptVars = templateDetail?.manifest.variables.filter((v) => v.prompt) ?? []
  const generatedVars = templateDetail?.manifest.variables.filter((v) => v.generate) ?? []

  function buildCreateBody() {
    if (sourceMode === "git") {
      return {
        name: name.trim(),
        git_mode: fetchMode,
        git_repo: gitRepo.trim(),
        git_branch: gitBranch.trim() || "main",
        git_path: gitPath.trim() || "docker-compose.yml",
        git_integration_id: gitVisibility === "private" ? gitIntegrationId || null : null,
      }
    }
    return { name: name.trim(), spec }
  }

  const canDeployTemplate =
    !!templateId &&
    promptVars.every((v) => !v.required || (promptValues[v.key] ?? "").trim().length > 0)

  const canCreate =
    sourceMode === "template"
      ? canDeployTemplate
      : name.trim().length > 0 &&
        (sourceMode === "raw"
          ? true
          : gitRepo.trim().length > 0 &&
            gitBranch.trim().length > 0 &&
            (gitVisibility === "public" || gitIntegrationId.length > 0))

  const deployMutation = useMutation({
    mutationFn: () =>
      templatesApi.deploy(
        orgId!,
        projectId,
        templateId,
        { spec, prompt_values: promptValues },
        token
      ),
    onSuccess: (stack) => {
      queryClient.invalidateQueries({ queryKey: ["stacks", orgId, projectId] })
      queryClient.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({
        to: "/projects/$id/stacks/$stackId/services",
        params: { id: projectId, stackId: stack.id },
      })
    },
  })

  const createMutation = useMutation({
    mutationFn: () => stacksApi.create(orgId!, projectId, buildCreateBody(), token),
    onSuccess: (stack) => {
      queryClient.invalidateQueries({ queryKey: ["stacks", orgId, projectId] })
      queryClient.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({
        to: "/projects/$id/stacks/$stackId/editor",
        params: { id: projectId, stackId: stack.id },
      })
    },
  })

  const createAndApplyMutation = useMutation({
    mutationFn: async () => {
      const stack = await stacksApi.create(orgId!, projectId, buildCreateBody(), token)
      if (sourceMode === "git") {
        await stacksApi.sync(orgId!, projectId, stack.id, token)
      } else {
        await stacksApi.apply(orgId!, projectId, stack.id, token)
      }
      return stack
    },
    onSuccess: (stack) => {
      queryClient.invalidateQueries({ queryKey: ["stacks", orgId, projectId] })
      queryClient.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({
        to: "/projects/$id/stacks/$stackId/services",
        params: { id: projectId, stackId: stack.id },
      })
    },
  })

  const isPending =
    createMutation.isPending || createAndApplyMutation.isPending || deployMutation.isPending
  const error = (createMutation.error ||
    createAndApplyMutation.error ||
    deployMutation.error) as Error | null

  return (
    <div className="space-y-8">
      {sourceMode !== "template" && (
        <Section title="Name" subtitle="Deploy a group of services together using a Docker Compose spec.">
          <Field label="Stack name">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-stack"
              className={inputCls}
            />
          </Field>
        </Section>
      )}

      <Section title="Source" subtitle="Write a Compose spec inline, pull it from git, or start from a template.">
        <SegmentedControl
          value={sourceMode}
          onValueChange={(v) => setSourceMode(v as StackSourceMode)}
          options={[
            { value: "raw", label: "Inline" },
            { value: "git", label: "Git" },
            { value: "template", label: "Template" },
          ]}
          className="text-sm"
        />

        {sourceMode === "git" ? (
          <div className="space-y-4 mt-4">
            <SegmentedControl
              value={gitVisibility}
              onValueChange={(v) => {
                setGitVisibility(v as StackGitVisibility)
                setGitIntegrationId("")
                setGitRepo("")
                setGitBranch("main")
              }}
              options={[
                { value: "public",  label: "Public" },
                { value: "private", label: "Private" },
              ]}
              className="text-sm"
            />

            {gitVisibility === "private" && (
              <Field label="Git integration" required>
                <Select
                  value={gitIntegrationId}
                  onValueChange={(v) => {
                    setGitIntegrationId(v ?? "")
                    setGitRepo("")
                    setGitBranch("main")
                  }}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue
                      placeholder={
                        connectedGit.length === 0
                          ? "No connected integrations — add one in Integrations"
                          : "Select a git integration…"
                      }
                    >
                      {connectedGit.find((g) => g.id === gitIntegrationId)?.name}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {connectedGit.map((g) => (
                      <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}

            <div className="grid grid-cols-2 gap-4">
              {gitVisibility === "private" ? (
                <Field label={reposFetching ? "Repository (loading…)" : "Repository"} required>
                  <Select
                    value={gitRepo}
                    onValueChange={(v) => {
                      const repo = repoList.find((r) => r.full_name === v)
                      setGitRepo(v ?? "")
                      setGitBranch(repo?.default_branch ?? "main")
                    }}
                    disabled={!gitIntegrationId || reposFetching}
                  >
                    <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                      <SelectValue
                        placeholder={
                          !gitIntegrationId
                            ? "Select an integration first"
                            : reposFetching
                            ? "Loading repositories…"
                            : repoList.length === 0
                            ? "No repositories found"
                            : "Select a repository…"
                        }
                      >
                        {gitRepo}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {repoList.map((r) => (
                        <SelectItem key={r.full_name} value={r.full_name}>{r.full_name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              ) : (
                <Field label="Repository URL" required>
                  <input
                    value={gitRepo}
                    onChange={(e) => setGitRepo(e.target.value)}
                    placeholder="https://github.com/org/repo"
                    className={inputCls}
                  />
                </Field>
              )}

              {gitVisibility === "private" ? (
                <Field label={branchesFetching ? "Branch (loading…)" : "Branch"} required>
                  <Select
                    value={gitBranch}
                    onValueChange={(v) => setGitBranch(v ?? "main")}
                    disabled={!gitRepo || branchesFetching}
                  >
                    <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                      <SelectValue placeholder={!gitRepo ? "Select a repo first" : "Select a branch…"}>
                        {gitBranch}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {branchList.map((b) => (
                        <SelectItem key={b} value={b}>{b}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              ) : (
                <Field label="Branch" required>
                  <input
                    value={gitBranch}
                    onChange={(e) => setGitBranch(e.target.value)}
                    placeholder="main"
                    className={inputCls}
                  />
                </Field>
              )}
            </div>

            <Field label="Compose file path">
              <input
                value={gitPath}
                onChange={(e) => setGitPath(e.target.value)}
                placeholder="docker-compose.yml"
                className={inputCls}
              />
            </Field>

            <Field label="Fetch mode">
              <SegmentedControl
                value={fetchMode}
                onValueChange={(v) => setFetchMode(v as StackFetchMode)}
                options={[
                  { value: "file", label: "Compose file only" },
                  { value: "repo", label: "Whole repo" },
                ]}
                className="text-sm w-fit"
              />
              <p className="text-xs text-muted-foreground mt-1">
                {fetchMode === "file"
                  ? "Only the Compose file is fetched. Use this when all images are pre-built."
                  : "The full repo is cloned so build contexts (e.g. ./frontend) can be resolved."}
              </p>
            </Field>
          </div>
        ) : sourceMode === "template" ? (
          <div className="space-y-4 mt-4">
            <Field label="Template" required>
              <Select value={templateId} onValueChange={(v) => setTemplateId(v ?? "")}>
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      templateList.length === 0 ? "No templates available" : "Choose a template…"
                    }
                  >
                    {templateList.find((t: TemplateManifest) => t.id === templateId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {templateList.map((t: TemplateManifest) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {templateDetail && (
              <>
                {promptVars.map((v) => (
                  <Field key={v.key} label={v.prompt || v.key} required={v.required}>
                    <input
                      value={promptValues[v.key] ?? ""}
                      onChange={(e) =>
                        setPromptValues((p) => ({ ...p, [v.key]: e.target.value }))
                      }
                      placeholder={v.key}
                      className={inputCls}
                    />
                  </Field>
                ))}
                {generatedVars.length > 0 && (
                  <p className="text-xs text-muted-foreground">
                    Auto-generated: {generatedVars.map((v) => v.key).join(", ")}
                  </p>
                )}
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Compose (editable)</p>
                  <StackEditor value={spec} onChange={setSpec} minHeight="300px" />
                </div>
              </>
            )}
          </div>
        ) : (
          <div className="mt-4">
            <StackEditor value={spec} onChange={setSpec} minHeight="360px" />
          </div>
        )}
      </Section>

      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
          <p className="text-xs text-destructive">{error.message}</p>
        </div>
      )}

      <div className="flex gap-2">
        {sourceMode === "template" ? (
          <Button
            className="flex-1 gap-2"
            disabled={!canCreate || isPending}
            onClick={() => deployMutation.mutate()}
          >
            {deployMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Deploy
          </Button>
        ) : (
          <>
            <Button
              variant="outline"
              className="flex-1 gap-2"
              disabled={!canCreate || isPending}
              onClick={() => createMutation.mutate()}
            >
              {createMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Create stack
            </Button>
            <Button
              className="flex-1 gap-2"
              disabled={!canCreate || isPending}
              onClick={() => createAndApplyMutation.mutate()}
            >
              {createAndApplyMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {sourceMode === "git" ? "Create & Sync" : "Create & Apply"}
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

// ─── Coming soon placeholder ──────────────────────────────────────────────────

// ─── Volume form ─────────────────────────────────────────────────────────────

function VolumeForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [name, setName] = useState("")
  const [storageGB, setStorageGB] = useState(5)

  const createMutation = useMutation({
    mutationFn: () => volumesApi.create(orgId!, projectId, { name, storage_gb: storageGB }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({ to: "/projects/$id/volumes", params: { id: projectId } })
    },
  })

  return (
    <div className="space-y-8">
      <Section title="Volume" subtitle="A persistent volume backed by a Kubernetes PVC. Attach it to an application service after creation.">
        <Field label="Name" required>
          <input
            className={inputCls}
            placeholder="uploads"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>

        <Field label="Size (GB)">
          <input
            type="number"
            min={1}
            max={500}
            className={inputCls}
            value={storageGB}
            onChange={(e) => setStorageGB(Number(e.target.value))}
          />
          <p className="text-[11px] text-muted-foreground mt-1">Default: 5 GB. Can be increased but not decreased after creation.</p>
        </Field>
      </Section>

      {/* Replica tradeoff callout */}
      <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-3">
        <Info className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
        <div className="text-xs text-amber-300/80 space-y-1">
          <p className="font-medium text-amber-300">Replicas vs persistence</p>
          <p>
            Volumes use ReadWriteOnce access — they can only be mounted to pods on a single node at a time.
            Attaching a volume to a service automatically scales it to 1 replica. To scale out, detach the volume first.
          </p>
        </div>
      </div>

      {createMutation.error && (
        <p className="text-xs text-destructive">{(createMutation.error as Error).message}</p>
      )}

      <Button
        disabled={!name.trim() || createMutation.isPending}
        onClick={() => createMutation.mutate()}
      >
        {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
        Create volume
      </Button>
    </div>
  )
}

function VariableGroupForm({ projectId }: { projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [name, setName] = useState("")
  const [description, setDescription] = useState("")

  const createMut = useMutation({
    mutationFn: () => groupsApi.create(orgId, projectId, { name: name.trim(), description: description.trim() }, token),
    onSuccess: (g) => {
      qc.invalidateQueries({ queryKey: ["variable-groups", orgId, projectId] })
      navigate({ to: "/projects/$id/variables/$groupId", params: { id: projectId, groupId: g.id } })
    },
  })

  return (
    <div className="space-y-8">
      <Section title="Details" subtitle="Group related variables and secrets, then attach them to services to inject as environment variables at deploy time.">
        <Field label="Name">
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Stripe, Database, Redis"
            className={inputCls}
          />
        </Field>
        <Field label="Description">
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Optional description"
            className={inputCls}
          />
        </Field>
      </Section>

      {createMut.error && (
        <p className="text-xs text-destructive">{(createMut.error as Error).message}</p>
      )}

      <Button
        disabled={!name.trim() || createMut.isPending}
        onClick={() => createMut.mutate()}
      >
        {createMut.isPending && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
        Create group
      </Button>
    </div>
  )
}

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

