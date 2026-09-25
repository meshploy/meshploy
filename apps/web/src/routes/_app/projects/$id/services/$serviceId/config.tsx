import { ConfigSaveBar, useConfigDraft, useConfigSave } from "@/components/layout/config-save-bar"
import { GroupAttachments } from "@/components/variables/group-attachments"
import { ResourceIntro } from "@/components/layout/resource-workbench"
import { FormLayout } from "@/components/layout/form-layout"
import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import { useState, useEffect, useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Check, ChevronDown, Copy, Eye, EyeOff, ExternalLink, HardDrive, Layers, Loader2, Plus, Server, Trash2, X, Zap } from "lucide-react"
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
import { envLanguage, envTheme } from "@/lib/env-lang"
import { envRefAutocomplete } from "@/lib/env-refs"
import {
  services as servicesApi,
  buildConfigs as buildConfigsApi,
  nodes as nodesApi,
  tcpRoutes as tcpRoutesApi,
  volumes as volumesApi,
  variableGroups as groupsApi,
  configFiles as configFilesApi,
  gitIntegrations as gitApi,
  toNode,
  type ApiNode,
  type ApiBuildConfig,
  type ApiServicePort,
  type ApiVolumeMount,
  type ApiVariableGroup,
  type UpdateServiceBody,
} from "@/lib/api"
import { nodeCardSub, schedulableNodes } from "@/lib/api/nodes"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Section, Field, NodeCard } from "@/components/services/form-primitives"
import { DeployWebhookURL } from "@/components/services/deploy-webhook"
import { useReceiving } from "@/components/services/deploy-guard"
import { formatRelativeTime } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { SourceFields, type SourceState } from "@/components/services/source-fields"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/config"
)({
  component: ConfigTab,
})

// ─── Env Vars section ─────────────────────────────────────────────────────────

// envEditorHeight grows an editor when its content would otherwise scroll.
//
// Not to fit: a hundred-line block would push everything else off the page.
// It grows to a ceiling and scrolls beyond that, which keeps a long block
// readable without making the rest of the form unreachable.
function envEditorHeight(value: string, base: number, max: number): string {
  const lines = value ? value.split("\n").length : 1
  const needed = lines * 19 + 16 // CodeMirror's line height, plus its padding
  return `${Math.min(Math.max(base, needed), max)}px`
}

function EnvVarsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const draft = useConfigDraft("")
  const { value: envVars, setValue: setEnvVars } = draft

  const { data, isLoading } = useQuery({
    queryKey: ["service-env-vars", orgId, projectId, serviceId],
    queryFn: () => servicesApi.getEnvVars(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  useEffect(() => {
    if (data !== undefined) draft.sync(data.env_vars)
  }, [data])

  const mutation = useMutation({
    mutationFn: () => servicesApi.update(orgId, projectId, serviceId, { env_vars: envVars }, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["service-env-vars", orgId, projectId, serviceId] })
    },
  })

  useConfigSave("Runtime environment variables", draft, () => mutation.mutateAsync(), !isLoading)

  // Names a ${…} reference can use besides the service's own: its attached
  // groups' variables. The same query as the variable groups section below.
  const { data: attached } = useQuery<ApiVariableGroup[]>({
    queryKey: ["service-variable-groups", orgId, projectId, serviceId],
    queryFn: () => groupsApi.listForService(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })
  const extensions = useMemo(() => {
    const groupNames = (attached ?? []).flatMap((g) =>
      g.items.map((i) => ({ name: i.key, source: g.name, secret: i.is_secret })),
    )
    return [envLanguage, envTheme, envRefAutocomplete(groupNames)]
  }, [attached])

  return (
    <Section
      title="Runtime environment variables"
      subtitle="Reach the running service, not the build. One KEY=VALUE pair per line. ${NAME} uses another variable, e.g. DATABASE_URL=${PRIMARY_PG_DB_URL} from an attached group; type ${ to pick one. Values are AES-256 encrypted at rest."
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
            height={envEditorHeight(envVars, 160, 420)}
            theme="dark"
            extensions={extensions}
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
    </Section>
  )
}

// ─── Variable groups section ──────────────────────────────────────────────────

// ─── Config files section ─────────────────────────────────────────────────────

/**
 * Files mounted into this service.
 *
 * Detaching is allowed while the service runs and re-applies immediately. The
 * mount is a copy taken at container start, so without the re-apply the file
 * would linger in the running pod and vanish at some later restart — failing
 * for a reason nobody would connect to this action.
 */
function ConfigFilesSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [selectedId, setSelectedId] = useState("")

  const { data, isLoading } = useQuery({
    queryKey: ["config-files", orgId, projectId],
    queryFn: () => configFilesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })
  const all = data?.files ?? []
  const { data: svc } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })
  const serviceName = svc?.name ?? ""

  const attached = all.filter((f) => f.services.includes(serviceName))
  const available = all.filter((f) => !f.services.includes(serviceName))

  const invalidate = () => qc.invalidateQueries({ queryKey: ["config-files", orgId, projectId] })
  const attachMut = useMutation({
    mutationFn: () => configFilesApi.attach(orgId, projectId, selectedId, serviceId, token),
    onSuccess: () => { setSelectedId(""); invalidate() },
  })
  const detachMut = useMutation({
    mutationFn: (fileId: string) => configFilesApi.detach(orgId, projectId, fileId, serviceId, token),
    onSuccess: invalidate,
  })

  return (
    <Section
      title="Config files"
      subtitle="Files mounted at a path in the container. Attaching or detaching re-applies the service."
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <div className="space-y-2">
          {attached.length > 0 && (
            <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
              {attached.map((f) => (
                <div key={f.id} className="flex items-center gap-3 px-3 py-2">
                  <Link
                    to="/projects/$id/config-files/$fileId"
                    params={{ id: projectId, fileId: f.id }}
                    className="flex-1 min-w-0 group"
                  >
                    <span className="text-xs font-medium text-foreground group-hover:underline">{f.name}</span>
                    <code className="text-[11px] font-mono text-muted-foreground/60 truncate block">{f.path}</code>
                  </Link>
                  <button
                    onClick={() => detachMut.mutate(f.id)}
                    disabled={detachMut.isPending}
                    className="text-[11px] text-muted-foreground hover:text-destructive transition-colors disabled:opacity-40"
                  >
                    {detachMut.isPending && detachMut.variables === f.id ? "Detaching…" : "Detach"}
                  </button>
                </div>
              ))}
            </div>
          )}

          <div className="flex gap-2">
            <Select value={selectedId} onValueChange={(v) => setSelectedId(v ?? "")}>
              <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
                <SelectValue placeholder={available.length === 0 ? "No config files available" : "Select a config file…"}>
                  {available.find((f) => f.id === selectedId)?.name}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {available.map((f) => (
                  <SelectItem key={f.id} value={f.id}>{f.name} — {f.path}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              size="sm"
              className="gap-1.5 shrink-0"
              onClick={() => attachMut.mutate()}
              disabled={!selectedId || attachMut.isPending || available.length === 0}
            >
              {attachMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
              Attach
            </Button>
          </div>
          {attachMut.isError && <p className="text-xs text-destructive">{(attachMut.error as Error).message}</p>}
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
                      <SelectValue placeholder={availableVolumes.length === 0 ? "No volumes available" : "Select a volume…"}>
                        {availableVolumes.find((v) => v.id === selectedVolumeId)?.name}
                      </SelectValue>
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
  const workerNodes = schedulableNodes(rawNodes).map(toNode)
  const builderNodes = rawNodes.filter(
    (n) => n.k8s_member && n.status === "online" && n.k3s_labels?.["meshploy.com/role"] === "builder"
  )

  // ── Form state ────────────────────────────────────────────────────────────
  const draft = useConfigDraft({
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
    builderCPULimit: "",
    builderMemoryLimit: "",
    autoDeploy: false,
    watchPaths: "",
    nodeId: "",
    replicas: 1,
    cpuRequest: "100m",
    cpuLimit: "500m",
    memoryRequest: "128Mi",
    memoryLimit: "512Mi",
  })
  const { value: form, setValue: setForm } = draft
  const [showResources, setShowResources] = useState(false)
  const patch = (p: Partial<typeof form>) => setForm((f) => ({ ...f, ...p }))

  useEffect(() => {
    if (!service) return
    const isGit = !!bc?.git_repo
    draft.sync({
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
      builderCPULimit: bc?.builder_cpu_limit ?? "",
      builderMemoryLimit: bc?.builder_memory_limit ?? "",
      autoDeploy: bc?.auto_deploy ?? false,
      watchPaths: (bc?.watch_paths ?? []).join("\n"),
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
          builder_cpu_limit: form.builderCPULimit,
          builder_memory_limit: form.builderMemoryLimit,
          auto_deploy: form.autoDeploy,
          watch_paths: form.watchPaths.split(/[\s,]+/).filter(Boolean),
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

  useConfigSave("Source and deployment", draft, () => mutation.mutateAsync(), !svcLoading && !bcLoading)

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
          <div className="flex flex-col gap-3">
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
            <Field label="Builder CPU limit">
              <input value={form.builderCPULimit} onChange={(e) => patch({ builderCPULimit: e.target.value })} placeholder="No cap" className={inputCls} />
            </Field>
            <Field label="Builder memory request">
              <input value={form.builderMemoryRequest} onChange={(e) => patch({ builderMemoryRequest: e.target.value })} placeholder="1Gi" className={inputCls} />
            </Field>
            <Field label="Builder memory limit">
              <input value={form.builderMemoryLimit} onChange={(e) => patch({ builderMemoryLimit: e.target.value })} placeholder="4Gi" className={inputCls} />
            </Field>
          </div>
          <p className="text-[11px] text-muted-foreground">
            Memory is capped so a build cannot take its node down: 4Gi when empty, or the request when that is larger. Leave the CPU limit empty to let builds use spare CPU.
          </p>
        </Section>
      )}

      {/* ── Auto-deploy ───────────────────────────────────────── */}
      {form.source === "git" && (
        <AutoDeploySection
          bc={bc}
          autoDeploy={form.autoDeploy}
          onToggle={(v) => patch({ autoDeploy: v })}
          watchPaths={form.watchPaths}
          onWatchPaths={(v) => patch({ watchPaths: v })}
          orgId={orgId}
          projectId={projectId}
          serviceId={serviceId}
        />
      )}

      {/* ── Deployment ────────────────────────────────────────── */}
      <Section title="Deployment" subtitle="Choose where this service runs and how many replicas to start">
        <div className="flex flex-col gap-3">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5" /> Target node
          </label>
          <div className="flex flex-wrap gap-2">
            <NodeCard label="Auto-schedule" sub="Let K3s decide" selected={form.nodeId === ""} onClick={() => patch({ nodeId: "" })} />
            {workerNodes.map((node) => (
              <NodeCard key={node.id} label={node.name} sub={nodeCardSub(node)}
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

        {/* Resource limits (collapsible), as on the create form */}
        <div className="rounded-lg border border-border/40">
          <Button
            variant="ghost"
            onClick={() => setShowResources(!showResources)}
            aria-expanded={showResources}
            aria-controls="service-resource-limits"
            className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <span className="font-medium">Resource limits</span>
            <ChevronDown className={cn("h-4 w-4 transition-transform", showResources ? "rotate-180" : "")} />
          </Button>
          {showResources && (
            <div id="service-resource-limits" className="grid grid-cols-2 gap-4 border-t border-border/40 p-4">
              <Field label="CPU request"><input value={form.cpuRequest} onChange={(e) => patch({ cpuRequest: e.target.value })} className={inputCls} /></Field>
              <Field label="CPU limit"><input value={form.cpuLimit} onChange={(e) => patch({ cpuLimit: e.target.value })} className={inputCls} /></Field>
              <Field label="Memory request"><input value={form.memoryRequest} onChange={(e) => patch({ memoryRequest: e.target.value })} className={inputCls} /></Field>
              <Field label="Memory limit"><input value={form.memoryLimit} onChange={(e) => patch({ memoryLimit: e.target.value })} className={inputCls} /></Field>
            </div>
          )}
        </div>
      </Section>

      {mutation.isError && (
        <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>
      )}
      {mutation.isSuccess && !draft.dirty && (
        <p className="text-xs text-emerald-400">Saved.</p>
      )}
    </div>
  )
}

// ─── Build env vars section ───────────────────────────────────────────────────

function BuildEnvVarsSection({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const draft = useConfigDraft("")
  const { value: envVars, setValue: setEnvVars } = draft

  const { data, isLoading, isError } = useQuery({
    queryKey: ["build-env-vars", orgId, projectId, serviceId],
    queryFn: () => buildConfigsApi.getBuildEnvVars(orgId, projectId, serviceId, token),
    enabled: !!orgId,
    retry: false,
  })

  useEffect(() => {
    if (data !== undefined) draft.sync(data.build_env_vars)
  }, [data])

  const mutation = useMutation({
    mutationFn: () =>
      buildConfigsApi.putBuildEnvVars(orgId, projectId, serviceId, envVars, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["build-env-vars", orgId, projectId, serviceId] })
    },
  })

  useConfigSave("Build environment variables", draft, () => mutation.mutateAsync(), !isLoading && !isError)

  // The same colouring and the same ${…} picker as the runtime block: a
  // reference there is resolved against the runtime block and its groups when
  // the build job is created, so the two editors behave alike.
  const { data: attached } = useQuery<ApiVariableGroup[]>({
    queryKey: ["service-variable-groups", orgId, projectId, serviceId],
    queryFn: () => groupsApi.listForService(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })
  const extensions = useMemo(() => {
    const groupNames = (attached ?? []).flatMap((g) =>
      g.items.map((i) => ({ name: i.key, source: g.name, secret: i.is_secret })),
    )
    return [envLanguage, envTheme, envRefAutocomplete(groupNames)]
  }, [attached])

  if (isError) return null // no build config yet

  return (
    <Section
      title="Build environment variables"
      subtitle="Reach the build only, not the running service — a build step that needs a value has to find it here. One KEY=VALUE per line; ${NAME} may reference a runtime variable or an attached group. For dockerfile: passed as --build-arg."
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
            height={envEditorHeight(envVars, 120, 360)}
            theme="dark"
            extensions={extensions}
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
      {mutation.isSuccess && !draft.dirty && (
        <p className="text-xs text-emerald-400">Saved.</p>
      )}
    </Section>
  )
}

// ─── Auto-deploy section ──────────────────────────────────────────────────────

function AutoDeploySection({
  bc,
  autoDeploy,
  onToggle,
  watchPaths,
  onWatchPaths,
  orgId,
  projectId,
  serviceId,
}: {
  bc: ApiBuildConfig | undefined
  autoDeploy: boolean
  watchPaths: string
  onWatchPaths: (v: string) => void
  onToggle: (v: boolean) => void
  orgId: string
  projectId: string
  serviceId: string
}) {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [copiedHook, setCopiedHook] = useState<"url" | "secret" | null>(null)
  const [showSecret, setShowSecret] = useState(false)
  // Both shut by default. The toggle is what this section is about; the two
  // URLs are for the times it needs setting up by hand, and side by side in
  // the open they read as a choice to make rather than fallbacks.
  const [showHook, setShowHook] = useState(false)

  // Which provider this service builds from decides how a push reaches
  // Meshploy: GitHub's App delivers on its own, the others need a webhook on
  // the repository.
  const { data: integrations = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId && !!bc?.git_integration_id,
  })

  // Where to add a repository to a GitHub App: that choice is made at GitHub.
  const installUrlMut = useMutation({
    mutationFn: () => gitApi.installUrl(orgId, bc!.git_integration_id!, token),
    onSuccess: ({ url }) => window.open(url, "_blank", "noopener"),
  })
  const integration = integrations.find((i) => i.id === bc?.git_integration_id)
  // GitHub has no repository webhook to add, but it has the same question:
  // does the connection actually cover this repository?
  const needsRepoHook = !!integration && integration.provider !== "github"
  const checksRepo = !!integration

  // Meshploy adds this hook itself when the token allows it, so this is the
  // fallback rather than the instruction - shown only when there is one to
  // show, and only to an admin, which is who the endpoint answers.
  const { data: pushHook, isLoading: hookLoading } = useQuery({
    queryKey: ["git-push-hook", orgId, bc?.git_integration_id, bc?.git_repo],
    queryFn: () => gitApi.pushHook(orgId, bc!.git_integration_id!, token, bc!.git_repo),
    enabled: !!orgId && checksRepo,
    retry: false,
  })

  // Adding the hook is the same call the build config makes on save; offered
  // here for when that one failed, or the hook was removed at the provider.
  const installHookMut = useMutation({
    mutationFn: () => gitApi.installPushHook(orgId, bc!.git_integration_id!, bc!.git_repo, token),
    onSuccess: (updated) => {
      qc.setQueryData(["git-push-hook", orgId, bc?.git_integration_id, bc?.git_repo], updated)
    },
  })

  // A group promotes into this level: pushes and CI calls here skip the
  // level below, which is worth saying where they are set up.
  const { receiving } = useReceiving(orgId, projectId, serviceId, token)

  if (!bc) return null

  const deployToken = bc.deploy_token

  return (
    <Section
      title="Auto-deploy"
      subtitle="Trigger a new build automatically on every push to the tracked branch."
    >
      <div className="space-y-4">
        {/* Deploy-on-push toggle */}
        <Field label="Auto-deploy on push">
          <div className="flex items-center gap-2">
            <Switch checked={autoDeploy} onCheckedChange={onToggle} />
            <span className="text-xs text-muted-foreground">
              {autoDeploy
                ? integration?.provider === "github"
                  ? "Enabled — the GitHub App reports every push"
                  : needsRepoHook
                    ? `Enabled — a webhook on the repository reports every push to ${bc.branch}`
                    : "Enabled — deploys on every push to the tracked branch"
                : "Disabled"}
            </span>
          </div>
          {receiving && (
            <p className={cn("mt-1.5 text-xs", autoDeploy ? "text-amber-400" : "text-muted-foreground")} data-testid="receiving-autodeploy">
              {autoDeploy
                ? `Pushes to ${bc.branch} build ${receiving.here?.name} directly, skipping ${receiving.from?.name}: right for a branch only hotfixes land on. The next promotion from ${receiving.from?.name} will not replace such a build until ${receiving.from?.name} builds something newer.`
                : `Off: ${receiving.here?.name} takes its images from ${receiving.from?.name} (group ${receiving.group.name}). Turn it on only for a branch hotfixes land on; pushes to it then build ${receiving.here?.name} directly.`}
            </p>
          )}
          {autoDeploy && !bc.git_integration_id && (
            <p className="text-xs text-amber-400 flex items-center gap-1 mt-1.5">
              <AlertTriangle className="h-3 w-3 shrink-0" />
              Deploying on push needs a connected git integration. For a public repository, use the deploy webhook URL below instead.
            </p>
          )}
        </Field>

        {autoDeploy && (
          <Field label="Only when these paths change">
            <textarea
              className={cn(inputCls, "min-h-[62px] font-mono text-xs py-2")}
              value={watchPaths}
              placeholder={"apps/api\npackages/db"}
              onChange={(e) => onWatchPaths(e.target.value)}
            />
            <p className="text-xs text-muted-foreground mt-1.5">
              One path per line, relative to the repository root. Empty means every push to {bc.branch} builds this
              service - which is what you want unless the repository holds more than one.
              {integration?.provider === "bitbucket" &&
                " Bitbucket's push payload names no files, so every push builds whatever is listed here."}
            </p>
          </Field>
        )}

        {/* The repository webhook, for the providers that keep one per repo.
            What is shown depends on what is actually on the repository, because
            "add this by hand" is noise when the hook is already there. */}
        {autoDeploy && checksRepo && pushHook && (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2 text-xs">
              {pushHook.state === "installed" ? (
                <>
                  <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
                  <span className="text-muted-foreground">
                    {needsRepoHook ? "Webhook installed on " : "The GitHub App reports pushes for "}
                    <span className="text-foreground/80 font-mono">{bc.git_repo}</span>
                    {needsRepoHook ? " — pushes reach Meshploy." : "."}
                  </span>
                </>
              ) : pushHook.state === "missing" && needsRepoHook ? (
                <>
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                  <span className="text-muted-foreground">
                    No webhook on <span className="text-foreground/80 font-mono">{bc.git_repo}</span> yet, so pushes are not reaching Meshploy.
                  </span>
                  <Button size="sm" variant="outline" className="ml-1 gap-1.5"
                    onClick={() => installHookMut.mutate()} disabled={installHookMut.isPending}>
                    {installHookMut.isPending && <Loader2 className="h-3 w-3 animate-spin" />}
                    Add it
                  </Button>
                </>
              ) : !needsRepoHook ? (
                <>
                  {/* GitHub: the App delivers, so what can be wrong is which
                      repositories it was installed on, and that is changed at
                      GitHub rather than here. */}
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                  <span className="text-muted-foreground">
                    {pushHook.reason || `${bc.git_repo} is not covered by this GitHub App, so its pushes do not reach Meshploy.`}
                  </span>
                  <Button size="sm" variant="outline" className="ml-1 gap-1.5"
                    onClick={() => installUrlMut.mutate()} disabled={installUrlMut.isPending}>
                    {installUrlMut.isPending && <Loader2 className="h-3 w-3 animate-spin" />}
                    Add it on GitHub
                  </Button>
                </>
              ) : (
                <>
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                  <span className="text-muted-foreground">
                    {pushHook.reason || "The webhook on this repository could not be checked."} Add it by hand below.
                  </span>
                </>
              )}
            </div>
            {installHookMut.isError && (
              <p className="text-xs text-destructive">{(installHookMut.error as Error).message}</p>
            )}

            {needsRepoHook && (
            <div className="rounded-lg border border-border/40">
              <Button
                variant="ghost"
                onClick={() => setShowHook(!showHook)}
                aria-expanded={showHook}
                aria-controls="push-hook-details"
                className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                <span className="font-medium">Add the {integration?.name} webhook by hand</span>
                <ChevronDown className={cn("h-4 w-4 transition-transform", showHook ? "rotate-180" : "")} />
              </Button>
              {showHook && (
                <div id="push-hook-details" className="flex flex-col gap-3 border-t border-border/40 p-4">
                  <p className="text-xs text-muted-foreground">
                    In {integration?.provider === "bitbucket" ? "Repository settings → Webhooks" : "the repository's webhook settings"}:
                    the URL below, the secret in <span className="text-foreground/80">{pushHook.secret_field}</span>, and
                    the <span className="text-foreground/80">{pushHook.event}</span> event only.
                  </p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 min-w-0 text-[11px] font-mono bg-muted/30 border border-border/40 rounded-lg h-[34px] leading-[32px] px-2.5 text-foreground/70 truncate">
                      POST {pushHook.url}
                    </code>
                    <Button size="icon-sm" variant="outline" title="Copy the webhook URL"
                      onClick={() => {
                        navigator.clipboard.writeText(pushHook.url)
                        setCopiedHook("url"); setTimeout(() => setCopiedHook(null), 2000)
                      }}>
                      {copiedHook === "url" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                    </Button>
                  </div>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 min-w-0 text-[11px] font-mono bg-muted/30 border border-border/40 rounded-lg h-[34px] leading-[32px] px-2.5 text-foreground/70 truncate">
                      {showSecret ? pushHook.secret : "•".repeat(32)}
                    </code>
                    <Button size="icon-sm" variant="outline" onClick={() => setShowSecret((v) => !v)}
                      title={showSecret ? "Hide the secret" : "Show the secret"}>
                      {showSecret ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                    </Button>
                    <Button size="icon-sm" variant="outline" title="Copy the secret"
                      onClick={() => {
                        navigator.clipboard.writeText(pushHook.secret)
                        setCopiedHook("secret"); setTimeout(() => setCopiedHook(null), 2000)
                      }}>
                      {copiedHook === "secret" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                    </Button>
                  </div>
                  <p className="text-[11px] text-muted-foreground/60">
                    One secret per integration, shared by every repository connected through it.
                  </p>
                </div>
              )}
            </div>
            )}
          </div>
        )}
        {autoDeploy && checksRepo && hookLoading && (
          <p className="text-xs text-muted-foreground flex items-center gap-1.5">
            <Loader2 className="h-3 w-3 animate-spin" /> Checking whether pushes reach Meshploy…
          </p>
        )}

        {/* Meshploy cannot see a CI job, but it records each call: one still
            deploying here after a group took over says to move it. */}
        {receiving && bc.deploy_hook_called_at && (
          <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90" data-testid="receiving-ci">
            CI called this service&apos;s deploy webhook {formatRelativeTime(new Date(bc.deploy_hook_called_at))}: it builds{" "}
            {receiving.here?.name} directly, skipping {receiving.from?.name}. To build in {receiving.from?.name} first and
            promote, point CI at{" "}
            {receiving.fromCell ? (
              <Link
                to="/projects/$id/services/$serviceId/config"
                params={{ id: receiving.from!.project_id, serviceId: receiving.fromCell.service_id }}
                className="font-medium text-primary hover:underline"
              >
                {receiving.from?.name}&apos;s deploy webhook
              </Link>
            ) : (
              `${receiving.from?.name}'s deploy webhook`
            )}{" "}
            instead, and keep this one for hotfixes or remove it.
          </div>
        )}

        {/* The per-service deploy token: nothing to do with a git connection,
            which is the point of it. */}
        <div className="flex flex-col gap-3">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Zap className="h-3 w-3" /> Deploy webhook URL
          </label>
          <DeployWebhookURL
            orgId={orgId}
            projectId={projectId}
            serviceId={serviceId}
            deployToken={deployToken}
            description="A URL that builds this one service, whoever calls it: a public repository with no connection, a CI job, a cron. The token in it is the only thing guarding it, so treat it as a credential."
          />
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

  const draft = useConfigDraft({ enabled: false, retention: "5" })
  const { enabled, retention } = draft.value
  const setEnabled = (enabled: boolean) => draft.setValue(v => ({ ...v, enabled }))
  const setRetention = (retention: string) => draft.setValue(v => ({ ...v, retention }))

  useEffect(() => {
    if (bc) {
      draft.sync({ enabled: bc.rollback_enabled ?? false, retention: String(bc.image_retention ?? 5) })
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

  useConfigSave("Rollback", draft, () => mutation.mutateAsync(), service?.type === "application" && !!bc && !isLoading)

  // Only show for application services that have a build config
  if (service?.type !== "application" || (!isLoading && !bc)) return null

  return (
    <Section
      title="Rollback"
      subtitle="Keep previous deployment images so you can roll back instantly without a rebuild."
    >
      {/* Two short fields, and the second only means anything while the first
          is on, so they read as one setting on one line. */}
      <div className="flex flex-wrap items-start gap-x-10 gap-y-4">
        <Field label="Enable rollback">
          <div className="flex h-[34px] items-center gap-2">
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

  const draft = useConfigDraft<PortRow[]>([])
  const { value: rows, setValue: setRows } = draft

  useEffect(() => {
    if (service) {
      draft.sync((service.ports ?? []).map(mkPortRow))
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

  useConfigSave("Ports", draft, () => mutation.mutateAsync(), service?.type === "application")

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
      {mutation.isSuccess && !draft.dirty && <p className="text-xs text-emerald-400">Saved.</p>}
    </Section>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

// ─── Database network access ──────────────────────────────────────────────────

// Three rungs, each including the one before, so the selected rung is the state
// rather than something to infer from a switch. Labelling a switch "reachable
// over the mesh" read as a claim about the present, and the text beside it read
// as the opposite.
type DBReach = "cluster" | "mesh" | "internet"

const REACH_RUNGS: { value: DBReach; label: string; detail: string }[] = [
  {
    value: "cluster",
    label: "In-cluster only",
    detail: "Other services connect by name. Nothing outside the cluster can.",
  },
  {
    value: "mesh",
    label: "Reachable over the mesh",
    detail: "Any machine on your WireGuard network can connect, with a client of your own.",
  },
  {
    value: "internet",
    label: "Reachable from the internet",
    detail: "The gateway publishes a port of its own and forwards it. Restrict who may connect below.",
  },
]

// What the database is, in one sentence, for the line under the rungs.
// The path each rung opens, drawn rather than described: "the gateway forwards
// its own port" never said that :15432 reaches the mesh NodePort, which reaches
// the database - and that is the question people actually ask.
function reachPaths(reach: DBReach, opts: { dbPort: number; nodePort: string; gatewayPort: string; name: string }): string[] {
  const inCluster = `${opts.name}:${opts.dbPort}  ←  other services, by name`
  const mesh = `mesh 100.64.0.1:${opts.nodePort || "<port>"}  →  ${opts.name}:${opts.dbPort}`
  const internet = `internet :${opts.gatewayPort || "<port>"}  →  100.64.0.1:${opts.nodePort || "<port>"}  →  ${opts.name}:${opts.dbPort}`
  switch (reach) {
    case "cluster":
      return [inCluster]
    case "mesh":
      return [inCluster, mesh]
    default:
      return [inCluster, internet]
  }
}

// inClusterHost is the name other workloads resolve a database by.
//
// Not its display name, and not the service's slug either: a database's
// Kubernetes object is named from its *database config's* slug, which carries a
// random suffix (`dbSlug` on the server). A database called "docai_db" answers
// to something like "docai-db-a1b2c3". Printing the display name here sent
// people to a host that does not exist.
//
// The fallback, for a row written before that column existed, is the same rule
// the server falls back to: the name, lower case, with spaces and underscores
// turned into hyphens.
function inClusterHost(name: string, dbSlug?: string): string {
  if (dbSlug) return dbSlug
  return name.toLowerCase().replace(/[ _]/g, "-")
}

function DatabaseNetworkSection({ projectId, serviceId, dbPort, serviceName }: { projectId: string; serviceId: string; dbPort: number; serviceName: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const queryKey = ["database-config", orgId, projectId, serviceId]
  const tcpKey = ["tcp-routes", orgId, projectId]
  const draft = useConfigDraft<{ reach: DBReach; port: string; gatewayPort: string; allowFrom: string }>({
    reach: "cluster", port: "", gatewayPort: "", allowFrom: "",
  })
  const { value, setValue } = draft

  const { data: dc, isLoading } = useQuery({
    queryKey,
    queryFn: () => servicesApi.getDatabaseConfig(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: tcpList = [] } = useQuery({
    queryKey: tcpKey,
    queryFn: () => tcpRoutesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })
  const route = tcpList.find((r) => r.service_id === serviceId)

  // A gateway port is unique across the gateway, so the whole org is what says
  // whether one is free -- another project's route takes it just as surely.
  const { data: usage } = useQuery({
    queryKey: ["tcp-port-usage", orgId],
    queryFn: () => tcpRoutesApi.usage(orgId, token),
    enabled: !!orgId,
  })
  const inUse = useMemo(() => {
    const taken = new Map<number, string>()
    for (const p of usage?.reserved ?? []) taken.set(p, "the gateway uses it itself")
    for (const r of usage?.routes ?? []) {
      if (r.id !== route?.id) taken.set(r.gateway_port, r.project_id === projectId ? "another resource in this project has it" : "another project has it")
    }
    return taken
  }, [usage, route?.id, projectId])

  // The database's own port is what a client expects to type, so it is offered
  // first; otherwise the first free port above 10000.
  const freePort = () => {
    if (dbPort && !inUse.has(dbPort)) return dbPort
    for (let p = 10000; p < 65536; p++) if (!inUse.has(p)) return p
    return dbPort
  }

  useEffect(() => {
    if (!dc || !usage) return
    draft.sync({
      reach: route ? "internet" : dc.mesh_exposed ? "mesh" : "cluster",
      port: dc.node_port ? String(dc.node_port) : "",
      gatewayPort: String(route?.gateway_port ?? freePort() ?? ""),
      allowFrom: (route?.allowed_cidrs ?? []).join(", "),
    })
  }, [dc, route, usage])

  const gatewayPortNum = Number(value.gatewayPort)
  const gatewayPortError = (() => {
    if (value.reach !== "internet" || !value.gatewayPort) return null
    if (!(gatewayPortNum > 0 && gatewayPortNum < 65536)) return "A port is between 1 and 65535."
    const why = inUse.get(gatewayPortNum)
    return why ? `Port ${gatewayPortNum} is not free: ${why}.` : null
  })()

  const mutation = useMutation({
    // Mesh access first: the gateway forwards to the port the cluster
    // publishes, so a route created before it exists has nothing to point at.
    mutationFn: async () => {
      if (gatewayPortError) throw new Error(gatewayPortError)
      await servicesApi.updateDatabaseConfig(
        orgId,
        projectId,
        serviceId,
        { mesh_exposed: value.reach !== "cluster", node_port: value.port ? Number(value.port) : 0 },
        token
      )
      if (value.reach === "internet") {
        const allowed = value.allowFrom.split(/[\s,]+/).filter(Boolean)
        const gatewayPort = Number(value.gatewayPort)
        if (route) {
          await tcpRoutesApi.update(orgId, projectId, route.id, { gateway_port: gatewayPort, allowed_cidrs: allowed }, token)
        } else {
          await tcpRoutesApi.create(orgId, projectId, { gateway_port: gatewayPort, service_id: serviceId, allowed_cidrs: allowed }, token)
        }
      } else if (route) {
        await tcpRoutesApi.remove(orgId, projectId, route.id, token)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey })
      queryClient.invalidateQueries({ queryKey: tcpKey })
    },
  })

  useConfigSave("Network access", draft, () => mutation.mutateAsync(), !isLoading)

  // A published port answers on every node. The gateway is the one to show:
  // it is the address an operator already knows.
  const { data: node } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId, token),
    enabled: !!orgId && value.reach !== "cluster",
    select: (nodes) => nodes.find((n) => n.k3s_role === "server") ?? nodes[0],
  })

  const published = value.reach !== "cluster"

  return (
    <Section
      title="Network access"
      subtitle="Other services always reach this database by name, from inside the cluster. That name does not work anywhere else."
    >
      <div className="space-y-4">
        <div className="space-y-2" role="radiogroup" aria-label="Network access">
          {REACH_RUNGS.map((rung) => {
            const selected = value.reach === rung.value
            return (
              <button
                key={rung.value}
                type="button"
                role="radio"
                aria-checked={selected}
                onClick={() => setValue((v) => ({ ...v, reach: rung.value }))}
                className={cn(
                  "flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
                  selected ? "border-primary bg-primary/5" : "border-border/60 bg-card",
                  !selected && "hover:border-border hover:bg-muted/20"
                )}
              >
                <span
                  className={cn(
                    "mt-0.5 size-3.5 shrink-0 rounded-full border-2",
                    selected ? "border-primary bg-primary/30" : "border-border"
                  )}
                />
                <span className="min-w-0 space-y-0.5">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="text-xs font-medium text-foreground">{rung.label}</span>
                  </span>
                  <span className="block text-xs text-muted-foreground">{rung.detail}</span>
                </span>
              </button>
            )
          })}
        </div>

        {/* Says which moment it describes: a description that reads like now
            while describing later is the confusion the rungs just removed. */}
        <div className="space-y-1.5">
          <p className="text-xs text-foreground">{draft.dirty ? "After saving:" : "Current state:"}</p>
          <div className="rounded-md bg-muted/30 px-2.5 py-2 space-y-1">
            {reachPaths(value.reach, {
              dbPort,
              nodePort: value.port,
              gatewayPort: value.gatewayPort,
              name: inClusterHost(serviceName, dc?.slug),
            }).map((line) => (
              <p key={line} className="text-[11px] font-mono text-muted-foreground">{line}</p>
            ))}
          </div>
        </div>

        {published && (
          <>
            <Field label="Port">
              <input
                className={inputCls}
                value={value.port}
                inputMode="numeric"
                placeholder="Automatic: the cluster picks a free port"
                onChange={(e) => setValue((v) => ({ ...v, port: e.target.value.replace(/[^0-9]/g, "") }))}
              />
              <p className="text-xs text-muted-foreground mt-1.5">
                Between 30000 and 32767. Leave it empty to let the cluster choose, then copy what it picked.
              </p>
            </Field>

            {dc?.node_port ? (
              <Field label="Mesh address">
                <code className="text-xs break-all">{node?.tailscale_ip || "a node's mesh IP"}:{dc.node_port}</code>
              </Field>
            ) : null}

            {value.reach === "internet" && (
              <>
                <Field label="Gateway port">
                  <div className="flex items-center gap-2">
                    <input
                      className={cn(inputCls, "flex-1 min-w-0")}
                      value={value.gatewayPort}
                      inputMode="numeric"
                      placeholder={String(dbPort)}
                      onChange={(e) => setValue((v) => ({ ...v, gatewayPort: e.target.value.replace(/[^0-9]/g, "") }))}
                    />
                    <Button
                      variant="outline"
                      className="shrink-0 gap-1.5"
                      onClick={() => setValue((v) => ({ ...v, gatewayPort: String(freePort()) }))}
                    >
                      <Zap className="h-3.5 w-3.5" />
                      Find a free port
                    </Button>
                  </div>
                  {gatewayPortError ? (
                    <p className="text-xs text-destructive mt-1.5">{gatewayPortError}</p>
                  ) : (
                    <p className="text-xs text-muted-foreground mt-1.5">
                      The port the gateway listens on, which clients connect to. It cannot be one the gateway already
                      uses for itself.
                    </p>
                  )}
                </Field>

                <Field label="Allow from">
                  <input
                    className={inputCls}
                    value={value.allowFrom}
                    placeholder="Anyone. e.g. 203.0.113.7, 10.0.0.0/8"
                    onChange={(e) => setValue((v) => ({ ...v, allowFrom: e.target.value }))}
                  />
                  <p className="text-xs text-muted-foreground mt-1.5">
                    Addresses or ranges, separated by commas. Left empty the port is open to the internet.
                  </p>
                </Field>

                {route && (
                  <Field label="Public address">
                    <span className="flex flex-wrap items-center justify-end gap-2">
                      <code className="text-xs break-all">
                        {node?.public_ip || "the gateway"}:{route.gateway_port}
                      </code>
                      <span
                        className={cn(
                          "text-[11px] border rounded px-1.5 py-0.5",
                          route.status === "open"
                            ? "text-emerald-400 border-emerald-500/20 bg-emerald-500/10"
                            : route.status === "failed"
                              ? "text-destructive border-destructive/20 bg-destructive/10"
                              : "text-muted-foreground border-border/60"
                        )}
                      >
                        {route.status === "open" ? "listening" : route.status}
                      </span>
                    </span>
                    {route.status === "failed" && route.last_error && (
                      <p className="text-xs text-destructive mt-1.5">{route.last_error}</p>
                    )}
                    {route.status === "pending" && (
                      <p className="text-xs text-muted-foreground mt-1.5">
                        The gateway opens the port within a minute of saving.
                      </p>
                    )}
                  </Field>
                )}

                <div className="rounded-md border border-border/60 bg-muted/20 p-3 text-xs text-muted-foreground">
                  The host firewall, and a cloud security group if there is one, must also allow this port: the
                  gateway listens on it, but neither of those knows that.
                </div>
              </>
            )}

            {dc && !dc.nodeport_mesh_only && (
              <div className="rounded-md border border-amber-500/20 bg-amber-500/5 p-3 flex gap-2">
                <AlertTriangle className="size-4 text-amber-400 shrink-0 mt-0.5" />
                <div className="text-xs text-amber-200/80 space-y-1">
                  <p className="font-medium text-amber-300">This port answers on every interface, not only the mesh.</p>
                  <p>
                    A published port binds on all of a node's addresses unless the cluster is told otherwise, so on a
                    gateway it is reachable from the internet too. Until this cluster restricts published ports to the
                    mesh range, protect it with the host firewall, or keep the database in-cluster only.
                  </p>
                </div>
              </div>
            )}
          </>
        )}

        {mutation.isError && <p className="text-xs text-destructive">{(mutation.error as Error).message}</p>}
      </div>
    </Section>
  )
}

function ConfigTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/config",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const { data: service } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
  })

  // A database has no build source and no environment of its own: the only
  // thing there is to configure is how it can be reached.
  if (service?.type === "database") {
    return (
      <ConfigSaveBar key={serviceId}><div className="console-page space-y-6 pb-24"><ResourceIntro title="Database configuration" description="Control how this database can be reached." /><FormLayout><div className="space-y-6">
        <DatabaseNetworkSection
          projectId={projectId}
          serviceId={serviceId}
          dbPort={service.ports?.find((p) => p.is_primary)?.port ?? service.ports?.[0]?.port ?? 5432}
          serviceName={service.name}
        />
      </div></FormLayout></div></ConfigSaveBar>
    )
  }

  return (
    <ConfigSaveBar key={serviceId}><div className="console-page space-y-6"><ResourceIntro title="Service configuration" description="Configure the build source, environment, networking and mounted resources." /><FormLayout><div className="space-y-6">
      <EnvVarsSection projectId={projectId} serviceId={serviceId} />
      <BuildEnvVarsSection projectId={projectId} serviceId={serviceId} />
      <GroupAttachments owner={{ kind: "service", id: serviceId }} projectId={projectId} />
      <ConfigFilesSection projectId={projectId} serviceId={serviceId} />
      <PortsSection projectId={projectId} serviceId={serviceId} />
      <VolumesSection projectId={projectId} serviceId={serviceId} />
      <SourceDeploySection projectId={projectId} serviceId={serviceId} />
      <RollbackSection projectId={projectId} serviceId={serviceId} />
    </div></FormLayout></div></ConfigSaveBar>
  )
}
