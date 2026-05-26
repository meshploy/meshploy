import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import { useState, useEffect } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Check, ChevronDown, Copy, ExternalLink, HardDrive, Layers, Loader2, Lock, Plus, Save, Server, Trash2, X, Zap } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import CodeMirror from "@uiw/react-codemirror"
import {
  services as servicesApi,
  buildConfigs as buildConfigsApi,
  nodes as nodesApi,
  volumes as volumesApi,
  variableGroups as groupsApi,
  toNode,
  type ApiNode,
  type ApiBuildConfig,
  type ApiServicePort,
  type ApiVolumeMount,
  type ApiVariableGroup,
  type UpdateServiceBody,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Section, Field, NodeCard } from "@/components/services/form-primitives"
import { Input } from "@/components/ui/input"
import { SourceFields, type SourceState } from "@/components/services/source-fields"

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
        <div className="rounded-md overflow-hidden border border-border/60">
          <CodeMirror
            value={envVars}
            height="160px"
            theme="dark"
            onChange={(val) => setEnvVars(val)}
            placeholder={"DATABASE_URL=postgres://...\nSECRET_KEY=..."}
            style={{ fontSize: 12 }}
            basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
          />
        </div>
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

// ─── Variable groups section ──────────────────────────────────────────────────

function GroupAttachmentRow({
  group, last, onDetach, isDetaching,
}: {
  group: ApiVariableGroup
  last: boolean
  onDetach: () => void
  isDetaching: boolean
}) {
  const varCount = group.items.filter((i) => !i.is_secret).length
  const secretCount = group.items.filter((i) => i.is_secret).length

  return (
    <div className={cn("flex items-center gap-3 px-3 py-2.5", !last && "border-b border-border/40")}>
      {group.system_managed
        ? <Lock className="h-3 w-3 text-muted-foreground/40 shrink-0" />
        : <Layers className="h-3 w-3 text-muted-foreground/40 shrink-0" />
      }
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium text-foreground truncate">{group.name}</span>
          {group.system_managed && (
            <span className="text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border/60 shrink-0">auto</span>
          )}
        </div>
        <p className="text-[11px] text-muted-foreground/60 mt-0.5">
          {varCount > 0 && `${varCount} var${varCount !== 1 ? "s" : ""}`}
          {varCount > 0 && secretCount > 0 && " · "}
          {secretCount > 0 && `${secretCount} secret${secretCount !== 1 ? "s" : ""}`}
          {group.items.length === 0 && "empty"}
        </p>
      </div>
      {!group.system_managed && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDetach}
          disabled={isDetaching}
          className="text-muted-foreground/30 hover:text-destructive transition-colors disabled:opacity-40 shrink-0"
          title="Detach"
        >
          {isDetaching ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
        </Button>
      )}
    </div>
  )
}

function VariableGroupsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const [showAdd, setShowAdd] = useState(false)
  const [selectedGroupId, setSelectedGroupId] = useState("")

  const { data: attached = [], isLoading } = useQuery<ApiVariableGroup[]>({
    queryKey: ["service-variable-groups", orgId, projectId, serviceId],
    queryFn: () => groupsApi.listForService(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: allGroups = [] } = useQuery<ApiVariableGroup[]>({
    queryKey: ["variable-groups", orgId, projectId],
    queryFn: () => groupsApi.list(orgId, projectId, token),
    enabled: !!orgId && showAdd,
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ["service-variable-groups", orgId, projectId, serviceId] })

  const attachMut = useMutation({
    mutationFn: () => groupsApi.attach(orgId, projectId, serviceId, selectedGroupId, token),
    onSuccess: () => { setShowAdd(false); setSelectedGroupId(""); invalidate() },
  })

  const detachMut = useMutation({
    mutationFn: (groupId: string) => groupsApi.detach(orgId, projectId, serviceId, groupId, token),
    onSuccess: invalidate,
  })

  const attachedIds = new Set(attached.map((g) => g.id))
  const availableGroups = allGroups.filter((g) => !attachedIds.has(g.id) && !(g.system_managed && g.service_id === serviceId))

  return (
    <Section
      title="Variable groups"
      subtitle="Attach groups of variables and secrets — all items inject as env vars on next deploy."
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <div className="space-y-2">
          {attached.length > 0 && (
            <div className="rounded-lg border border-border/60 overflow-hidden">
              {attached.map((g, i) => (
                <GroupAttachmentRow
                  key={g.id}
                  group={g}
                  last={i === attached.length - 1}
                  onDetach={() => detachMut.mutate(g.id)}
                  isDetaching={detachMut.isPending && detachMut.variables === g.id}
                />
              ))}
            </div>
          )}

          {showAdd ? (
            <div className="rounded-lg border border-border/60 bg-card p-3 space-y-3">
              <Select value={selectedGroupId} onValueChange={(v) => setSelectedGroupId(v ?? "")}>
                <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
                  <SelectValue placeholder={availableGroups.length === 0 ? "No groups available" : "Select a variable group…"} />
                </SelectTrigger>
                <SelectContent>
                  {availableGroups.map((g) => (
                    <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <div className="flex gap-2">
                <Button
                  onClick={() => attachMut.mutate()}
                  disabled={!selectedGroupId || attachMut.isPending || availableGroups.length === 0}
                  className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md bg-primary text-primary-foreground disabled:opacity-40 transition-opacity"
                >
                  {attachMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
                  Attach
                </Button>
                <Button
                  variant="ghost"
                  onClick={() => { setShowAdd(false); setSelectedGroupId("") }}
                  className="flex items-center gap-1 text-xs px-3 py-1.5 rounded-md text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-3 w-3" /> Cancel
                </Button>
              </div>
              {attachMut.isError && (
                <p className="text-xs text-destructive">{(attachMut.error as Error).message}</p>
              )}
            </div>
          ) : (
            <Button
              variant="ghost"
              onClick={() => setShowAdd(true)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <Plus className="h-3.5 w-3.5" /> Attach group
            </Button>
          )}
        </div>
      )}
    </Section>
  )
}

// ─── Volumes section ──────────────────────────────────────────────────────────

function VolumesSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const [showAdd, setShowAdd] = useState(false)
  const [selectedVolumeId, setSelectedVolumeId] = useState("")
  const [mountPath, setMountPath] = useState("")

  const { data: mounts = [], isLoading } = useQuery({
    queryKey: ["service-volume-mounts", orgId, projectId, serviceId],
    queryFn: () => volumesApi.listServiceMounts(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: projectVolumes = [] } = useQuery({
    queryKey: ["volumes", orgId, projectId],
    queryFn: () => volumesApi.list(orgId, projectId, token),
    enabled: !!orgId && showAdd,
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["service-volume-mounts", orgId, projectId, serviceId] })
    qc.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
  }

  const attachMut = useMutation({
    mutationFn: () => volumesApi.attach(orgId, projectId, selectedVolumeId, { service_id: serviceId, mount_path: mountPath.trim() }, token),
    onSuccess: () => { setShowAdd(false); setSelectedVolumeId(""); setMountPath(""); invalidate() },
  })

  const detachMut = useMutation({
    mutationFn: ({ volumeId, mountId }: { volumeId: string; mountId: string }) =>
      volumesApi.detach(orgId, projectId, volumeId, mountId, token),
    onSuccess: invalidate,
  })

  const availableVolumes = projectVolumes.filter((v) => v.status === "ready" && (!v.mounts || v.mounts.length === 0))

  return (
    <Section title="Volumes" subtitle="Mount a persistent volume into this service. Attaching locks replicas to 1.">
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <div className="space-y-2">
          {mounts.length > 0 && (
            <div className="rounded-lg border border-border/60 overflow-hidden">
              {mounts.map((m, i) => (
                <VolumeMountRow
                  key={m.id}
                  mount={m}
                  last={i === mounts.length - 1}
                  onDetach={() => detachMut.mutate({ volumeId: m.volume_id, mountId: m.id })}
                  isDetaching={detachMut.isPending && detachMut.variables?.mountId === m.id}
                />
              ))}
            </div>
          )}

          {mounts.length === 0 && showAdd ? (
            <div className="rounded-lg border border-border/60 bg-card p-3 space-y-3">
              <div className="flex items-start gap-2 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2">
                <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
                <p className="text-xs text-amber-300/80">
                  Attaching a volume scales this service to 1 replica. Detach the volume to scale out again.
                </p>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-muted-foreground">Volume</label>
                  <Select value={selectedVolumeId} onValueChange={(v) => setSelectedVolumeId(v ?? "")}>
                    <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
                      <SelectValue placeholder={availableVolumes.length === 0 ? "No volumes available" : "Select a volume…"} />
                    </SelectTrigger>
                    <SelectContent>
                      {availableVolumes.map((v) => (
                        <SelectItem key={v.id} value={v.id}>{v.name} ({v.storage_gb} GB)</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-muted-foreground">Mount path</label>
                  <input
                    className={inputCls}
                    placeholder="/data"
                    value={mountPath}
                    onChange={(e) => setMountPath(e.target.value)}
                  />
                </div>
              </div>
              <div className="flex gap-2">
                <Button
                  onClick={() => attachMut.mutate()}
                  disabled={!selectedVolumeId || !mountPath.trim() || attachMut.isPending || availableVolumes.length === 0}
                  className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md bg-primary text-primary-foreground disabled:opacity-40 transition-opacity"
                >
                  {attachMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
                  Attach
                </Button>
                <Button
                  variant="ghost"
                  onClick={() => { setShowAdd(false); setSelectedVolumeId(""); setMountPath("") }}
                  className="flex items-center gap-1 text-xs px-3 py-1.5 rounded-md text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-3 w-3" /> Cancel
                </Button>
              </div>
              {attachMut.isError && (
                <p className="text-xs text-destructive">{(attachMut.error as Error).message}</p>
              )}
            </div>
          ) : mounts.length === 0 ? (
            <Button
              variant="ghost"
              onClick={() => setShowAdd(true)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <Plus className="h-3.5 w-3.5" /> Attach volume
            </Button>
          ) : null}
        </div>
      )}
    </Section>
  )
}

function VolumeMountRow({
  mount, last, onDetach, isDetaching,
}: {
  mount: ApiVolumeMount
  last: boolean
  onDetach: () => void
  isDetaching: boolean
}) {
  return (
    <div className={cn("flex items-center gap-3 px-3 py-2.5", !last && "border-b border-border/40")}>
      <HardDrive className="h-3 w-3 text-muted-foreground/40 shrink-0" />
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <span className="text-xs text-muted-foreground truncate">{mount.volume?.name ?? mount.volume_id}</span>
        <span className="text-muted-foreground/30 text-xs">→</span>
        <code className="text-xs font-mono text-foreground truncate">{mount.mount_path}</code>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onDetach}
        disabled={isDetaching}
        className="text-muted-foreground/30 hover:text-destructive transition-colors disabled:opacity-40 shrink-0"
        title="Detach"
      >
        {isDetaching ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
      </Button>
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

  // ── Form state ────────────────────────────────────────────────────────────
  const [form, setForm] = useState({
    source: "git" as "git" | "image",
    // git visibility: "public" = no auth needed; "private" = requires git integration
    gitVisibility: "private" as "public" | "private",
    image: "",
    // image visibility: "public" = no pull auth; "private" = needs registry pull creds
    imageVisibility: "public" as "public" | "private",
    pullRegistryIntegrationId: "",
    gitIntegrationId: "",
    gitRepo: "",
    gitBranch: "main",
    builder: "railpack" as "railpack" | "dockerfile",
    dockerfilePath: "Dockerfile",
    registryIntegrationId: "",
    builderNodeName: "" as string,
    builderCPURequest: "1000m",
    builderMemoryRequest: "1Gi",
    autoDeploy: false,
    nodeId: "",
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
      gitVisibility: bc?.git_integration_id ? "private" : "public",
      image: service.image ?? "",
      imageVisibility: service.pull_registry_integration_id ? "private" : "public",
      pullRegistryIntegrationId: service.pull_registry_integration_id ?? "",
      gitIntegrationId: bc?.git_integration_id ?? "",
      gitRepo: bc?.git_repo ?? "",
      gitBranch: bc?.branch ?? "main",
      builder: (bc?.builder as typeof form.builder) ?? "railpack",
      dockerfilePath: bc?.dockerfile_path ?? "Dockerfile",
      registryIntegrationId: bc?.registry_integration_id ?? "",
      builderNodeName: bc?.builder_node ?? "",
      builderCPURequest: bc?.builder_cpu_request || "1000m",
      builderMemoryRequest: bc?.builder_memory_request || "1Gi",
      autoDeploy: bc?.auto_deploy ?? false,
      nodeId: service.node_id ?? "",
      replicas: service.replicas,
      cpuRequest: service.cpu_request,
      cpuLimit: service.cpu_limit,
      memoryRequest: service.memory_request,
      memoryLimit: service.memory_limit,
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [service, bc])

  const { data: volumeMounts = [] } = useQuery({
    queryKey: ["service-volume-mounts", orgId, projectId, serviceId],
    queryFn: () => volumesApi.listServiceMounts(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })
  const hasVolume = volumeMounts.length > 0

  // Clamp replicas to 1 whenever a volume is attached
  useEffect(() => {
    if (hasVolume && form.replicas > 1) patch({ replicas: 1 })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasVolume])

  // ── Save ──────────────────────────────────────────────────────────────────
  const mutation = useMutation({
    mutationFn: async () => {
      // Always update service fields
      const svcBody: UpdateServiceBody = {
        node_id: form.nodeId,
        replicas: form.replicas,
        cpu_request: form.cpuRequest,
        cpu_limit: form.cpuLimit,
        memory_request: form.memoryRequest,
        memory_limit: form.memoryLimit,
      }
      if (form.source === "image") {
        svcBody.image = form.image
        // "" clears (public image), UUID sets pull secret; always send to reflect visibility choice
        svcBody.pull_registry_integration_id = form.imageVisibility === "private" ? form.pullRegistryIntegrationId : ""
      }
      const updatedSvc = await servicesApi.update(orgId, projectId, serviceId, svcBody, token)
      // Update build config when git source
      if (form.source === "git") {
        await buildConfigsApi.update(orgId, projectId, serviceId, {
          // public git: clear the integration; private git: send the selected one
          git_integration_id: form.gitVisibility === "private" ? (form.gitIntegrationId || undefined) : "",
          git_repo: form.gitRepo,
          branch: form.gitBranch,
          builder: form.builder,
          dockerfile_path: form.builder === "dockerfile" ? form.dockerfilePath : undefined,
          registry_integration_id: form.registryIntegrationId || undefined,
          builder_node: form.builderNodeName,
          builder_cpu_request: form.builderCPURequest,
          builder_memory_request: form.builderMemoryRequest,
          auto_deploy: form.autoDeploy,
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
        {service?.stack_id ? (
          <div className="flex items-center justify-between rounded-md border border-border/60 bg-muted/20 px-3 py-2.5">
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Layers className="h-3.5 w-3.5 shrink-0" />
              <span>Source is managed by a stack</span>
            </div>
            <Link
              to="/projects/$id/stacks/$stackId/editor"
              params={{ id: projectId, stackId: service.stack_id }}
              className="flex items-center gap-1 text-xs text-primary/80 hover:text-primary transition-colors"
            >
              Edit in Stack Editor
              <ExternalLink className="h-3 w-3" />
            </Link>
          </div>
        ) : (
          <SourceFields
            value={form as SourceState}
            onChange={patch}
          />
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

      {/* ── Auto-deploy ───────────────────────────────────────── */}
      {form.source === "git" && (
        <AutoDeploySection
          bc={bc}
          autoDeploy={form.autoDeploy}
          onToggle={(v) => patch({ autoDeploy: v })}
          orgId={orgId}
          projectId={projectId}
          serviceId={serviceId}
        />
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

        <Field label="Replicas">
          <Input
            type="number"
            min={1}
            max={hasVolume ? 1 : 20}
            value={hasVolume ? 1 : form.replicas}
            disabled={hasVolume}
            onChange={(e) => !hasVolume && patch({ replicas: Math.max(1, parseInt(e.target.value) || 1) })}
          />
          {hasVolume && (
            <p className="text-[11px] text-amber-400 flex items-center gap-1 mt-1">
              <AlertTriangle className="h-3 w-3 shrink-0" /> Locked to 1 — volume attached
            </p>
          )}
        </Field>
      </Section>

      {/* ── Resource limits (collapsible) ─────────────────────── */}
      <div className="rounded-lg border border-border/40">
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
      subtitle="Injected at build time only — not available at runtime. One KEY=VALUE per line. For dockerfile: passed as --build-arg."
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-4">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <div className="rounded-md overflow-hidden border border-border/60">
          <CodeMirror
            value={envVars}
            height="120px"
            theme="dark"
            onChange={(val) => setEnvVars(val)}
            placeholder={"NIXPACKS_INSTALL_CMD=npm install\nNODE_ENV=production"}
            style={{ fontSize: 12 }}
            basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
          />
        </div>
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

// ─── Auto-deploy section ──────────────────────────────────────────────────────

function AutoDeploySection({
  bc,
  autoDeploy,
  onToggle,
  orgId,
  projectId,
  serviceId,
}: {
  bc: ApiBuildConfig | undefined
  autoDeploy: boolean
  onToggle: (v: boolean) => void
  orgId: string
  projectId: string
  serviceId: string
}) {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [copied, setCopied] = useState(false)

  const regenMut = useMutation({
    mutationFn: () => buildConfigsApi.regenerateDeployToken(orgId, projectId, serviceId, token),
    onSuccess: (updated) => {
      qc.setQueryData(["build-config", orgId, projectId, serviceId], updated)
    },
  })

  if (!bc) return null

  const deployToken = bc.deploy_token
  const webhookPath = `/api/v1/webhooks/deploy/${serviceId}?token=${deployToken}`

  function copyWebhook() {
    navigator.clipboard.writeText(window.location.origin + webhookPath)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Section
      title="Auto-deploy"
      subtitle="Trigger a new build automatically on every push to the tracked branch."
    >
      <div className="space-y-4">
        {/* GitHub App auto-deploy toggle */}
        <Field label="Auto-deploy on push">
          <div className="flex items-center gap-2">
            <Switch checked={autoDeploy} onCheckedChange={onToggle} />
            <span className="text-xs text-muted-foreground">
              {autoDeploy
                ? "Enabled — deploys on every push via GitHub App webhook"
                : "Disabled"}
            </span>
          </div>
          {autoDeploy && !bc.git_integration_id && (
            <p className="text-xs text-amber-400 flex items-center gap-1 mt-1.5">
              <AlertTriangle className="h-3 w-3 shrink-0" />
              GitHub App auto-deploy requires a connected private git integration. For public repos, use the webhook URL below instead.
            </p>
          )}
        </Field>

        {/* Per-service deploy token webhook URL */}
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Zap className="h-3 w-3" /> Deploy webhook URL
          </label>
          <p className="text-xs text-muted-foreground mb-2">
            Add this as a webhook in your git provider — any POST triggers a build. Works with GitHub, GitLab, Gitea, Bitbucket, or any provider.
          </p>
          <div className="flex items-center gap-2">
            <code className="flex-1 text-[11px] font-mono bg-muted/30 border border-border/40 rounded px-2.5 py-1.5 text-foreground/70 truncate">
              POST {webhookPath}
            </code>
            <Button size="icon-sm" variant="outline" onClick={copyWebhook} title="Copy full webhook URL">
              {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
            </Button>
            <Button
              size="icon-sm"
              variant="outline"
              onClick={() => regenMut.mutate()}
              disabled={regenMut.isPending}
              title="Regenerate token — invalidates the current URL"
            >
              {regenMut.isPending
                ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                : <Zap className="h-3.5 w-3.5" />}
            </Button>
          </div>
          {regenMut.isError && (
            <p className="text-xs text-destructive mt-1">{(regenMut.error as Error).message}</p>
          )}
        </div>
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
            <Switch checked={enabled} onCheckedChange={setEnabled} />
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

// ─── Ports section ────────────────────────────────────────────────────────────

interface PortRow {
  key: number
  name: string
  port: string
  isHTTP: boolean
  isPrimary: boolean
  isPublic: boolean
}

let _portKey = 0
const mkPortRow = (p?: ApiServicePort): PortRow => ({
  key: ++_portKey,
  name: p?.name ?? "",
  port: p ? String(p.port) : "",
  isHTTP: p?.is_http ?? true,
  isPrimary: p?.is_primary ?? false,
  isPublic: p?.is_public ?? true,
})

function PortsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const [rows, setRows] = useState<PortRow[]>([])

  useEffect(() => {
    if (service?.ports?.length) {
      setRows(service.ports.map(mkPortRow))
    }
  }, [service])

  const patchRow = (key: number, patch: Partial<PortRow>) =>
    setRows((rs) => rs.map((r) => r.key === key ? { ...r, ...patch } : r))

  const setPrimary = (key: number) =>
    setRows((rs) => rs.map((r) => ({ ...r, isPrimary: r.key === key })))

  const addRow = () => setRows((rs) => [...rs, mkPortRow()])

  const removeRow = (key: number) =>
    setRows((rs) => {
      const next = rs.filter((r) => r.key !== key)
      if (next.length > 0 && !next.some((r) => r.isPrimary)) {
        next[0].isPrimary = true
      }
      return next
    })

  const mutation = useMutation({
    mutationFn: () => {
      const ports = rows.map((r) => ({
        name: r.name,
        port: parseInt(r.port) || 3000,
        is_http: r.isHTTP,
        is_primary: r.isPrimary,
        is_public: r.isPublic,
      }))
      return servicesApi.update(orgId, projectId, serviceId, { ports }, token)
    },
    onSuccess: (updated) => {
      qc.setQueryData(["service", orgId, projectId, serviceId], updated)
      qc.invalidateQueries({ queryKey: ["services", orgId, projectId] })
    },
  })

  if (service?.type !== "application") return null

  return (
    <Section title="Ports" subtitle="Expose ports from this service. The primary port is used as the default route target.">
      <div className="space-y-2">
        {rows.map((row) => (
          <div key={row.key} className="rounded-md border border-border/60 bg-muted/10 p-3 space-y-2">
            <div className="grid grid-cols-[1fr_100px] gap-2">
              <Field label="Name">
                <input
                  value={row.name}
                  onChange={(e) => patchRow(row.key, { name: e.target.value })}
                  placeholder="http"
                  className={inputCls}
                />
              </Field>
              <Field label="Port">
                <input
                  type="number"
                  value={row.port}
                  onChange={(e) => patchRow(row.key, { port: e.target.value })}
                  placeholder="3000"
                  className={inputCls}
                />
              </Field>
            </div>
            <div className="flex items-center gap-4 flex-wrap">
              <label className="flex items-center gap-1.5 text-xs text-muted-foreground select-none cursor-pointer">
                <input
                  type="checkbox"
                  checked={row.isHTTP}
                  onChange={(e) => patchRow(row.key, { isHTTP: e.target.checked })}
                  className="accent-primary"
                />
                HTTP
              </label>
              <label className="flex items-center gap-1.5 text-xs text-muted-foreground select-none cursor-pointer">
                <input
                  type="checkbox"
                  checked={row.isPublic}
                  onChange={(e) => patchRow(row.key, { isPublic: e.target.checked })}
                  className="accent-primary"
                />
                Public (NodePort)
              </label>
              <label className="flex items-center gap-1.5 text-xs text-muted-foreground select-none cursor-pointer">
                <input
                  type="radio"
                  checked={row.isPrimary}
                  onChange={() => setPrimary(row.key)}
                  className="accent-primary"
                />
                Primary
              </label>
              <div className="ml-auto">
                {rows.length > 1 && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => removeRow(row.key)}
                    className="text-muted-foreground/40 hover:text-destructive"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            </div>
          </div>
        ))}
        <Button
          variant="ghost"
          onClick={addRow}
          className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
        >
          <Plus className="h-3.5 w-3.5" /> Add port
        </Button>
      </div>
      {mutation.isError && (
        <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>
      )}
      {mutation.isSuccess && <p className="text-xs text-emerald-400">Saved.</p>}
      <div className="flex justify-end">
        <Button size="sm" className="gap-1.5" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
          {mutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save ports
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
<VariableGroupsSection projectId={projectId} serviceId={serviceId} />
      <PortsSection projectId={projectId} serviceId={serviceId} />
      <VolumesSection projectId={projectId} serviceId={serviceId} />
      <BuildEnvVarsSection projectId={projectId} serviceId={serviceId} />
      <SourceDeploySection projectId={projectId} serviceId={serviceId} />
      <RollbackSection projectId={projectId} serviceId={serviceId} />
    </div>
  )
}
