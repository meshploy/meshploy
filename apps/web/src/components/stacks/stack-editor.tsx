import { useState, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { parse as parseYaml, stringify as stringifyYaml } from "yaml"
import CodeMirror from "@uiw/react-codemirror"
import { yaml } from "@codemirror/lang-yaml"
import { Plus, Trash2, Code2, LayoutGrid, ChevronDown, Server, Wand2 } from "lucide-react"
import {
  SiPostgresql, SiMysql, SiRedis, SiMongodb, SiClickhouse,
} from "@icons-pack/react-simple-icons"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import { inputCls, Field, NodeCard } from "@/components/services/form-primitives"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { Button } from "@/components/ui/button"
import { gitIntegrations as gitApi, nodes as nodesApi, toNode, type ApiNode } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { cn } from "@/lib/utils"

// ─── Constants ────────────────────────────────────────────────────────────────

export type VisualBuilder = "railpack" | "dockerfile"

const DB_ENGINES = [
  { value: "postgres",   label: "PostgreSQL",  versions: ["17", "16", "15", "14", "13"], port: 5432,  icon: SiPostgresql },
  { value: "mysql",      label: "MySQL",        versions: ["8.0", "5.7"],                 port: 3306,  icon: SiMysql },
  { value: "redis",      label: "Redis",        versions: ["7", "6"],                     port: 6379,  icon: SiRedis },
  { value: "mongodb",    label: "MongoDB",      versions: ["7", "6"],                     port: 27017, icon: SiMongodb },
  { value: "clickhouse", label: "ClickHouse",   versions: ["24", "23"],                   port: 9000,  icon: SiClickhouse },
  { value: "dragonfly",  label: "Dragonfly",    versions: ["latest"],                     port: 6379,  icon: null },
]

// ─── Types ────────────────────────────────────────────────────────────────────

export interface VisualService {
  _key: string
  serviceType: "app" | "database"
  name: string
  // App — source
  source: "image" | "git"
  image: string
  integrationId: string
  gitRepo: string
  gitBranch: string
  // App — build (git only)
  builder: VisualBuilder
  builderNodeName: string   // k8s_node_name or "" for auto
  builderCPURequest: string
  builderMemoryRequest: string
  // App — deploy
  port: number | ""
  replicas: number | ""
  nodeId: string            // node UUID or "" for auto
  cpuRequest: string
  cpuLimit: string
  memoryRequest: string
  memoryLimit: string
  // Database
  dbEngine: string
  dbVersion: string
  dbStorageGB: number | ""
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function newService(): VisualService {
  return {
    _key: crypto.randomUUID(),
    serviceType: "app",
    name: "",
    source: "git",
    image: "",
    integrationId: "",
    gitRepo: "",
    gitBranch: "main",
    builder: "railpack",
    builderNodeName: "",
    builderCPURequest: "1000m",
    builderMemoryRequest: "1Gi",
    port: 3000,
    replicas: 1,
    nodeId: "",
    cpuRequest: "100m",
    cpuLimit: "500m",
    memoryRequest: "128Mi",
    memoryLimit: "512Mi",
    dbEngine: "postgres",
    dbVersion: "16",
    dbStorageGB: 10,
  }
}

function dbDefaultPort(engine: string) {
  return DB_ENGINES.find((e) => e.value === engine)?.port ?? 5432
}

function dbVersions(engine: string) {
  return DB_ENGINES.find((e) => e.value === engine)?.versions ?? ["latest"]
}

// ─── Compose conversion ───────────────────────────────────────────────────────

const DB_IMAGE_PATTERN: Record<string, string> = {
  postgres: "postgres", postgresql: "postgres",
  mysql: "mysql", mariadb: "mysql",
  redis: "redis",
  mongo: "mongodb", mongodb: "mongodb",
  clickhouse: "clickhouse",
  dragonfly: "dragonfly",
}

function detectDbEngine(image: string): string | null {
  const name = image.split(":")[0].split("/").pop()?.toLowerCase() ?? ""
  return DB_IMAGE_PATTERN[name] ?? null
}

function imageVersion(image: string): string | null {
  const tag = image.split(":")[1]
  return tag && tag !== "latest" ? tag : null
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function extractPort(ports: any): number | null {
  if (!Array.isArray(ports) || ports.length === 0) return null
  const first = ports[0]
  if (typeof first === "string") {
    // "host:container" or "container" or "ip:host:container"
    const parts = first.split(":")
    const p = parseInt(parts[parts.length - 1])
    return isNaN(p) ? null : p
  }
  if (typeof first === "object" && first !== null) {
    // { target: 3000, published: 80 } — use target (container port)
    const p = parseInt((first as Record<string, unknown>).target as string)
    return isNaN(p) ? null : p
  }
  return null
}

// Returns true if any service in the spec is missing an x-meshploy block.
export function specNeedsConversion(spec: string): boolean {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const doc = parseYaml(spec) as any
    const svcs = doc?.services
    if (!svcs || typeof svcs !== "object") return false
    return Object.values(svcs).some((s) => !(s as Record<string, unknown>)?.["x-meshploy"])
  } catch {
    return false
  }
}

// Enrich a plain Docker Compose spec with x-meshploy defaults.
// Services that already have x-meshploy are left untouched.
export function convertCompose(spec: string): string {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const doc = parseYaml(spec) as any
    if (!doc?.services || typeof doc.services !== "object") return spec

    for (const svc of Object.values(doc.services) as Record<string, unknown>[]) {
      if (!svc || svc["x-meshploy"]) continue

      const image = (svc.image as string) ?? ""
      const dbEngine = detectDbEngine(image)
      const detectedPort = extractPort(svc.ports)

      if (dbEngine) {
        const defaultPort = dbDefaultPort(dbEngine)
        const ver = imageVersion(image) ?? dbVersions(dbEngine)[0]
        svc["x-meshploy"] = {
          type: "database",
          database: { engine: dbEngine, version: ver, storage_gb: 10 },
          deploy: { port: detectedPort ?? defaultPort, replicas: 1 },
        }
      } else {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const deploy: Record<string, any> = {
          port: detectedPort ?? 3000,
          replicas: 1,
          cpu_request: "100m",
          cpu_limit: "500m",
          memory_request: "128Mi",
          memory_limit: "512Mi",
        }
        svc["x-meshploy"] = { deploy }
      }
    }

    return stringifyYaml(doc, { lineWidth: 120 })
  } catch {
    return spec
  }
}

// ─── YAML ↔ Visual ────────────────────────────────────────────────────────────

export function yamlToVisual(spec: string): VisualService[] {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const doc = parseYaml(spec) as any
    const svcs = doc?.services
    if (!svcs || typeof svcs !== "object") return []

    return Object.entries(svcs).map(([name, raw]) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const svc = raw as any
      const mp = svc?.["x-meshploy"] ?? {}
      const isDb = mp?.type === "database"

      if (isDb) {
        const db = mp?.database ?? {}
        const engine = db.engine ?? "postgres"
        return {
          ...newService(),
          _key: crypto.randomUUID(),
          serviceType: "database" as const,
          name,
          dbEngine: engine,
          dbVersion: db.version ?? dbVersions(engine)[0],
          dbStorageGB: db.storage_gb ?? 10,
          port: mp?.deploy?.port ?? dbDefaultPort(engine),
          replicas: mp?.deploy?.replicas ?? 1,
          nodeId: mp?.deploy?.node ?? "",
        }
      }

      const src = mp?.source ?? {}
      const build = mp?.build ?? {}
      const deploy = mp?.deploy ?? {}
      const image: string = svc?.image ?? ""
      const integrationId: string = src.integration_id ?? ""
      const gitRepo: string = src.git ?? ""
      const source: "image" | "git" = integrationId || gitRepo ? "git" : image ? "image" : "git"

      return {
        ...newService(),
        _key: crypto.randomUUID(),
        serviceType: "app" as const,
        name,
        source,
        image,
        integrationId,
        gitRepo,
        gitBranch: src.branch ?? "main",
        builder: build.builder ?? "railpack",
        builderNodeName: build.builder_node ?? "",
        builderCPURequest: build.builder_cpu_request ?? "1000m",
        builderMemoryRequest: build.builder_memory_request ?? "1Gi",
        port: deploy.port ?? 3000,
        replicas: deploy.replicas ?? 1,
        nodeId: deploy.node ?? "",
        cpuRequest: deploy.cpu_request ?? "100m",
        cpuLimit: deploy.cpu_limit ?? "500m",
        memoryRequest: deploy.memory_request ?? "128Mi",
        memoryLimit: deploy.memory_limit ?? "512Mi",
      }
    })
  } catch {
    return []
  }
}

export function visualToYaml(services: VisualService[]): string {
  if (services.length === 0) return "services: {}\n"
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const svcs: Record<string, any> = {}

  for (const s of services) {
    const name = s.name.trim() || "service"

    if (s.serviceType === "database") {
      const engine = s.dbEngine || "postgres"
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const dbDeploy: Record<string, any> = {
        port: s.port || dbDefaultPort(engine),
        replicas: s.replicas || 1,
      }
      if (s.nodeId) dbDeploy.node = s.nodeId
      svcs[name] = {
        image: `${engine}:${s.dbVersion || "latest"}`,
        "x-meshploy": {
          type: "database",
          database: { engine, version: s.dbVersion || "latest", storage_gb: s.dbStorageGB || 10 },
          deploy: dbDeploy,
        },
      }
      continue
    }

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const deploy: Record<string, any> = {
      port: s.port || 3000,
      replicas: s.replicas || 1,
    }
    if (s.nodeId) deploy.node = s.nodeId
    if (s.cpuRequest)    deploy.cpu_request    = s.cpuRequest
    if (s.cpuLimit)      deploy.cpu_limit      = s.cpuLimit
    if (s.memoryRequest) deploy.memory_request = s.memoryRequest
    if (s.memoryLimit)   deploy.memory_limit   = s.memoryLimit

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const mp: Record<string, any> = { deploy }

    if (s.source === "git") {
      mp.source = {
        integration_id: s.integrationId,
        git: s.gitRepo,
        branch: s.gitBranch,
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const build: Record<string, any> = { builder: s.builder }
      if (s.builderNodeName)        build.builder_node            = s.builderNodeName
      if (s.builderCPURequest)      build.builder_cpu_request     = s.builderCPURequest
      if (s.builderMemoryRequest)   build.builder_memory_request  = s.builderMemoryRequest
      mp.build = build
    }

    svcs[name] = {
      image: s.source === "image" ? s.image : "",
      "x-meshploy": mp,
    }
  }

  return stringifyYaml({ services: svcs }, { lineWidth: 120 })
}

// ─── StackEditor ──────────────────────────────────────────────────────────────

interface StackEditorProps {
  value: string
  onChange: (value: string) => void
  minHeight?: string
}

export function StackEditor({ value, onChange, minHeight = "360px" }: StackEditorProps) {
  const [mode, setMode] = useState<"yaml" | "visual">("yaml")
  const [visual, setVisual] = useState<VisualService[]>([])
  const [justConverted, setJustConverted] = useState(false)

  const switchToVisual = useCallback(() => {
    setVisual(yamlToVisual(value))
    setMode("visual")
  }, [value])

  const switchToYaml = useCallback(() => {
    onChange(visualToYaml(visual))
    setMode("yaml")
  }, [visual, onChange])

  const handleConvert = useCallback(() => {
    onChange(convertCompose(value))
    setJustConverted(true)
    setTimeout(() => setJustConverted(false), 2000)
  }, [value, onChange])

  const patchService = (key: string, patch: Partial<VisualService>) =>
    setVisual((prev) => prev.map((s) => (s._key === key ? { ...s, ...patch } : s)))

  const addService = () => setVisual((prev) => [...prev, newService()])
  const removeService = (key: string) => setVisual((prev) => prev.filter((s) => s._key !== key))

  const canConvert = mode === "yaml" && specNeedsConversion(value)

  return (
    <div className="flex flex-col rounded-md border border-border/60 overflow-hidden">
      {/* Mode toggle bar */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-border/60 bg-muted/20 shrink-0">
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground font-medium">
            {mode === "yaml" ? "YAML" : "Visual"}
          </span>
          {canConvert && (
            <Button
              variant="ghost"
              onClick={handleConvert}
              className="flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-medium border border-amber-500/30 bg-amber-500/10 text-amber-400 hover:bg-amber-500/20 transition-colors"
            >
              <Wand2 className="h-3 w-3" />
              {justConverted ? "Done!" : "Add Meshploy config"}
            </Button>
          )}
          {justConverted && !canConvert && (
            <span className="text-[11px] text-emerald-400/80 font-mono">converted</span>
          )}
        </div>
        <SegmentedControl
          value={mode}
          onValueChange={(v) => {
            if (v === "yaml" && mode === "visual") switchToYaml()
            else if (v === "visual" && mode === "yaml") switchToVisual()
          }}
          options={[
            { value: "yaml", label: "YAML", icon: <Code2 className="h-3 w-3" /> },
            { value: "visual", label: "Visual", icon: <LayoutGrid className="h-3 w-3" /> },
          ]}
        />
      </div>

      {mode === "yaml" ? (
        <CodeMirror
          value={value}
          height={minHeight}
          theme="dark"
          extensions={[yaml()]}
          onChange={onChange}
          style={{ fontSize: 13 }}
          basicSetup={{ lineNumbers: true, foldGutter: true, autocompletion: true, indentOnInput: true }}
        />
      ) : (
        <div className="flex flex-col gap-3 p-4 overflow-y-auto" style={{ minHeight }}>
          {visual.length === 0 && (
            <p className="text-xs text-muted-foreground text-center py-6">No services. Add one below.</p>
          )}
          {visual.map((svc) => (
            <ServiceCard
              key={svc._key}
              svc={svc}
              onChange={(patch) => patchService(svc._key, patch)}
              onRemove={() => removeService(svc._key)}
            />
          ))}
          <Button
            variant="ghost"
            onClick={addService}
            className="flex items-center justify-center gap-1.5 rounded-md border border-dashed border-border/60 py-2.5 text-xs text-muted-foreground hover:text-foreground hover:border-border transition-colors"
          >
            <Plus className="h-3.5 w-3.5" />
            Add service
          </Button>
        </div>
      )}
    </div>
  )
}

// ─── ServiceCard ──────────────────────────────────────────────────────────────

function ServiceCard({
  svc,
  onChange,
  onRemove,
}: {
  svc: VisualService
  onChange: (p: Partial<VisualService>) => void
  onRemove: () => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: gitList = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token),
    enabled: !!orgId,
    staleTime: 60_000,
  })
  const connectedGit = gitList.filter((g) => g.connected)

  const { data: repoList = [], isFetching: reposFetching } = useQuery({
    queryKey: ["git-repos", orgId, svc.integrationId],
    queryFn: () => gitApi.repos(orgId, svc.integrationId, token),
    enabled: !!svc.integrationId,
    staleTime: 5 * 60_000,
  })

  const { data: branchList = [], isFetching: branchesFetching } = useQuery({
    queryKey: ["git-branches", orgId, svc.integrationId, svc.gitRepo],
    queryFn: () => gitApi.branches(orgId, svc.integrationId, svc.gitRepo, token),
    enabled: !!svc.integrationId && !!svc.gitRepo,
    staleTime: 2 * 60_000,
  })

  const { data: rawNodes = [] } = useQuery<ApiNode[]>({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId, token),
    enabled: !!orgId,
    staleTime: 30_000,
  })
  const workerNodes = rawNodes
    .filter((n) => n.k8s_member && n.status === "online" && n.k3s_role === "agent")
    .map(toNode)
  const builderNodes = rawNodes.filter(
    (n) => n.k8s_member && n.status === "online" && n.k3s_labels?.["meshploy.com/role"] === "builder"
  )

  return (
    <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-3 px-3 py-2.5 border-b border-border/40 bg-muted/10">
        <input
          value={svc.name}
          onChange={(e) => onChange({ name: e.target.value })}
          placeholder="service-name"
          className="flex-1 bg-transparent text-sm font-medium text-foreground placeholder:text-muted-foreground/40 focus:outline-none min-w-0"
        />
        {/* App / Database toggle */}
        <SegmentedControl
          value={svc.serviceType}
          onValueChange={(v) => onChange({ serviceType: v as "app" | "database" })}
          options={[
            { value: "app", label: "App" },
            { value: "database", label: "Database" },
          ]}
          className="text-[11px] shrink-0"
        />
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRemove}
          className="text-muted-foreground hover:text-destructive shrink-0"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="p-4 space-y-5">
        {svc.serviceType === "database" ? (
          <DatabaseFields svc={svc} onChange={onChange} workerNodes={workerNodes} />
        ) : (
          <AppFields
            svc={svc}
            onChange={onChange}
            connectedGit={connectedGit}
            repoList={repoList}
            branchList={branchList}
            reposFetching={reposFetching}
            branchesFetching={branchesFetching}
            workerNodes={workerNodes}
            builderNodes={builderNodes}
          />
        )}
      </div>
    </div>
  )
}

// ─── AppFields ────────────────────────────────────────────────────────────────

function AppFields({
  svc,
  onChange,
  connectedGit,
  repoList,
  branchList,
  reposFetching,
  branchesFetching,
  workerNodes,
  builderNodes,
}: {
  svc: VisualService
  onChange: (p: Partial<VisualService>) => void
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  connectedGit: any[]
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  repoList: any[]
  branchList: string[]
  reposFetching: boolean
  branchesFetching: boolean
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  workerNodes: any[]
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  builderNodes: any[]
}) {
  const [showResources, setShowResources] = useState(false)

  return (
    <div className="space-y-5">
      {/* ── Source ── */}
      <SectionHeader title="Source" />
      <div className="space-y-4">
        <Field label="Source">
          <SegmentedControl
            value={svc.source ?? "git"}
            onValueChange={(v) => onChange({ source: v as "git" | "image" })}
            options={[
              { value: "git", label: "Git repository" },
              { value: "image", label: "Docker image" },
            ]}
            className="text-sm"
          />
        </Field>

        {svc.source === "image" ? (
          <Field label="Image" required>
            <input
              value={svc.image}
              onChange={(e) => onChange({ image: e.target.value })}
              placeholder="nginx:alpine"
              className={inputCls}
            />
          </Field>
        ) : (
          <>
            <Field label="Git integration" required>
              <Select
                value={svc.integrationId}
                onValueChange={(v) => onChange({ integrationId: v ?? "", gitRepo: "", gitBranch: "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue placeholder={connectedGit.length === 0 ? "No connected integrations" : "Select a git integration…"}>
                    {connectedGit.find((g) => g.id === svc.integrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {connectedGit.map((g) => (
                    <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <Field label={reposFetching ? "Repository (loading…)" : "Repository"} required>
              <Select
                value={svc.gitRepo}
                onValueChange={(v) => {
                  const repo = repoList.find((r) => r.full_name === v)
                  onChange({ gitRepo: v ?? "", gitBranch: repo?.default_branch ?? "" })
                }}
                disabled={!svc.integrationId || reposFetching}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue placeholder={
                    !svc.integrationId ? "Select an integration first"
                    : reposFetching ? "Loading repositories…"
                    : repoList.length === 0 ? "No accessible repositories"
                    : "Select a repository…"
                  } />
                </SelectTrigger>
                <SelectContent>
                  {repoList.map((r) => (
                    <SelectItem key={r.full_name} value={r.full_name}>{r.full_name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <div className="grid grid-cols-2 gap-4">
              <Field label={branchesFetching ? "Branch (loading…)" : "Branch"} required>
                <Select
                  value={svc.gitBranch}
                  onValueChange={(v) => onChange({ gitBranch: v ?? "" })}
                  disabled={!svc.gitRepo || branchesFetching}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue placeholder={
                      !svc.gitRepo ? "Select a repo first"
                      : branchesFetching ? "Loading branches…"
                      : "Select a branch…"
                    } />
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
                  value={svc.builder}
                  onValueChange={(v) => onChange({ builder: (v ?? "railpack") as VisualBuilder })}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="railpack">Railpack</SelectItem>
                    <SelectItem value="dockerfile">Dockerfile</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </>
        )}
      </div>

      {/* ── Build config (git only) ── */}
      {svc.source === "git" && (
        <>
          <div className="border-t border-border/40" />
          <SectionHeader title="Build" subtitle="Where and how the build job runs" />
          <div className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
                <Server className="h-3 w-3" />Builder node
              </label>
              <div className="flex flex-wrap gap-2">
                <NodeCard
                  label="Auto-schedule"
                  sub="Any builder node"
                  selected={svc.builderNodeName === ""}
                  onClick={() => onChange({ builderNodeName: "" })}
                />
                {builderNodes.map((node) => (
                  <NodeCard
                    key={node.k8s_node_name}
                    label={node.name}
                    sub={node.tailscale_ip ?? ""}
                    selected={svc.builderNodeName === node.k8s_node_name}
                    onClick={() => onChange({ builderNodeName: node.k8s_node_name })}
                    online
                  />
                ))}
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Field label="Builder CPU request">
                <input
                  value={svc.builderCPURequest}
                  onChange={(e) => onChange({ builderCPURequest: e.target.value })}
                  placeholder="1000m"
                  className={inputCls}
                />
              </Field>
              <Field label="Builder memory request">
                <input
                  value={svc.builderMemoryRequest}
                  onChange={(e) => onChange({ builderMemoryRequest: e.target.value })}
                  placeholder="1Gi"
                  className={inputCls}
                />
              </Field>
            </div>
          </div>
        </>
      )}

      {/* ── Deployment ── */}
      <div className="border-t border-border/40" />
      <SectionHeader title="Deployment" subtitle="Where this service runs" />
      <div className="space-y-4">
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3 w-3" />Target node
          </label>
          <div className="flex flex-wrap gap-2">
            <NodeCard
              label="Auto-schedule"
              sub="Let K3s decide"
              selected={svc.nodeId === ""}
              onClick={() => onChange({ nodeId: "" })}
            />
            {workerNodes.map((node) => (
              <NodeCard
                key={node.id}
                label={node.name}
                sub={node.tailscaleIP ?? ""}
                selected={svc.nodeId === node.id}
                onClick={() => onChange({ nodeId: node.id })}
                online
              />
            ))}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <Field label="Port" required>
            <input
              type="number"
              value={svc.port}
              onChange={(e) => onChange({ port: e.target.value === "" ? "" : Number(e.target.value) })}
              placeholder="3000"
              className={inputCls}
            />
          </Field>
          <Field label="Replicas">
            <input
              type="number"
              value={svc.replicas}
              onChange={(e) => onChange({ replicas: e.target.value === "" ? "" : Number(e.target.value) })}
              placeholder="1"
              min={1}
              className={inputCls}
            />
          </Field>
        </div>

        {/* Resource limits — collapsible */}
        <div className="rounded-lg border border-border/40">
          <Button
            variant="ghost"
            onClick={() => setShowResources((v) => !v)}
            className="w-full flex items-center justify-between px-4 py-2.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <span className="font-medium text-xs">Resource limits</span>
            <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", showResources && "rotate-180")} />
          </Button>
          {showResources && (
            <div className="px-4 pb-4 pt-0 grid grid-cols-2 gap-4 border-t border-border/40">
              <Field label="CPU request">
                <input value={svc.cpuRequest} onChange={(e) => onChange({ cpuRequest: e.target.value })} placeholder="100m" className={inputCls} />
              </Field>
              <Field label="CPU limit">
                <input value={svc.cpuLimit} onChange={(e) => onChange({ cpuLimit: e.target.value })} placeholder="500m" className={inputCls} />
              </Field>
              <Field label="Memory request">
                <input value={svc.memoryRequest} onChange={(e) => onChange({ memoryRequest: e.target.value })} placeholder="128Mi" className={inputCls} />
              </Field>
              <Field label="Memory limit">
                <input value={svc.memoryLimit} onChange={(e) => onChange({ memoryLimit: e.target.value })} placeholder="512Mi" className={inputCls} />
              </Field>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── DatabaseFields ───────────────────────────────────────────────────────────

function DatabaseFields({
  svc,
  onChange,
  workerNodes,
}: {
  svc: VisualService
  onChange: (p: Partial<VisualService>) => void
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  workerNodes: any[]
}) {
  return (
    <div className="space-y-5">
      <SectionHeader title="Engine" />
      <Field label="Engine">
        <div className="grid grid-cols-3 gap-2">
          {DB_ENGINES.map((eng) => {
            const Icon = eng.icon
            return (
              <Button
                key={eng.value}
                variant="ghost"
                onClick={() => onChange({ dbEngine: eng.value, dbVersion: eng.versions[0], port: eng.port })}
                className={cn(
                  "flex items-center gap-2 px-2.5 py-2 rounded-lg border text-left transition-colors",
                  svc.dbEngine === eng.value
                    ? "border-primary/50 bg-primary/10 text-foreground hover:bg-primary/10 hover:text-foreground dark:hover:bg-primary/10"
                    : "border-border/60 bg-muted/10 text-muted-foreground hover:text-foreground hover:bg-muted/30"
                )}
              >
                {Icon ? (
                  <Icon className="h-3.5 w-3.5 shrink-0" />
                ) : (
                  <span className="h-3.5 w-3.5 flex items-center justify-center text-[9px] font-bold shrink-0">DF</span>
                )}
                <span className="text-xs truncate">{eng.label}</span>
              </Button>
            )
          })}
        </div>
      </Field>

      <div className="grid grid-cols-3 gap-4">
        <Field label="Version">
          <Select
            value={svc.dbVersion}
            onValueChange={(v) => onChange({ dbVersion: v ?? dbVersions(svc.dbEngine)[0] })}
          >
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {dbVersions(svc.dbEngine).map((v) => (
                <SelectItem key={v} value={v}>{v}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="Storage (GB)">
          <input
            type="number"
            value={svc.dbStorageGB}
            onChange={(e) => onChange({ dbStorageGB: e.target.value === "" ? "" : Number(e.target.value) })}
            placeholder="10"
            min={1}
            className={inputCls}
          />
        </Field>
        <Field label="Replicas">
          <input
            type="number"
            value={svc.replicas}
            onChange={(e) => onChange({ replicas: e.target.value === "" ? "" : Number(e.target.value) })}
            placeholder="1"
            min={1}
            className={inputCls}
          />
        </Field>
      </div>

      <div className="border-t border-border/40" />
      <SectionHeader title="Deployment" subtitle="Where this database runs" />
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
          <Server className="h-3 w-3" />Target node
        </label>
        <div className="flex flex-wrap gap-2">
          <NodeCard
            label="Auto-schedule"
            sub="Let K3s decide"
            selected={svc.nodeId === ""}
            onClick={() => onChange({ nodeId: "" })}
          />
          {workerNodes.map((node) => (
            <NodeCard
              key={node.id}
              label={node.name}
              sub={node.tailscaleIP ?? ""}
              selected={svc.nodeId === node.id}
              onClick={() => onChange({ nodeId: node.id })}
              online
            />
          ))}
        </div>
      </div>
    </div>
  )
}

// ─── SectionHeader ────────────────────────────────────────────────────────────

function SectionHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div>
      <p className="text-sm font-medium text-foreground">{title}</p>
      {subtitle && <p className="text-xs text-muted-foreground mt-0.5">{subtitle}</p>}
    </div>
  )
}
