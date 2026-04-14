import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import {
  Box,
  ChevronLeft,
  ChevronDown,
  Clock,
  Database,
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
  toNode,
  type CreateServiceBody,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

// ─── Route ───────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/_app/projects/$id/new")({
  component: NewResourcePage,
})

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = "service" | "job" | "cron-job" | "database"
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
  nodeId: string | null
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
  nodeId: null,
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
  { type: "job",      icon: Zap,      label: "Job",      soon: true },
  { type: "cron-job", icon: Clock,    label: "Cron Job", soon: true },
  { type: "database", icon: Database, label: "Database", soon: true },
]

// ─── Page ─────────────────────────────────────────────────────────────────────

function NewResourcePage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/new" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()

  const [resourceType, setResourceType] = useState<ResourceType>("service")
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
        body.git_repo   = form.gitRepo
        body.branch     = form.gitBranch
        body.builder    = form.builder
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
        form.gitBranch.length > 0)

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

  const { data: allNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
    select: (raw) => raw.map(toNode),
  })

  const workerNodes = allNodes.filter(
    (n) => n.k8sMember && n.status === "online" && n.k3sRole === "agent"
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
                  />
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
          </div>
        )}
      </Section>

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

        {/* Replicas */}
        <Field label="Replicas">
          <input
            type="number"
            min={1}
            max={20}
            value={form.replicas}
            onChange={(e) => patch({ replicas: Math.max(1, parseInt(e.target.value) || 1) })}
            className={cn(inputCls, "w-24")}
          />
        </Field>
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

// ─── Shared components ────────────────────────────────────────────────────────

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
    <div className="space-y-4">
      <div className="border-b border-border/40 pb-2">
        <p className="text-sm font-medium text-foreground">{title}</p>
        {subtitle && (
          <p className="text-xs text-muted-foreground mt-0.5">{subtitle}</p>
        )}
      </div>
      {children}
    </div>
  )
}

function Field({
  label,
  required,
  children,
}: {
  label: string
  required?: boolean
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label className="text-xs font-medium text-muted-foreground">
        {label}
        {required && <span className="text-destructive ml-0.5">*</span>}
      </label>
      {children}
    </div>
  )
}

function NodeCard({
  label,
  sub,
  selected,
  onClick,
  online,
}: {
  label: string
  sub: string
  selected: boolean
  onClick: () => void
  online?: boolean
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex flex-col gap-0.5 rounded-lg border-2 px-3 py-2.5 text-left transition-all min-w-[120px]",
        selected
          ? "border-primary bg-primary/5"
          : "border-border/60 bg-card hover:border-border hover:bg-muted/20"
      )}
    >
      <div className="flex items-center gap-1.5">
        {online && (
          <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 shrink-0" />
        )}
        <span className="text-xs font-medium text-foreground truncate">{label}</span>
      </div>
      <span className="text-[11px] text-muted-foreground font-mono truncate">{sub}</span>
    </button>
  )
}

const inputCls =
  "w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"
