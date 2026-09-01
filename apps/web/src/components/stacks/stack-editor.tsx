import { useState, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { parse as parseYaml, stringify as stringifyYaml, parseDocument, isMap } from "yaml"
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
  /**
   * The key this service has in the YAML document, or "" when the visual editor
   * created it. Renames are applied to that key so the rest of the service's
   * YAML — every field the visual editor does not model — survives.
   */
  _origName: string
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
  // Environment — compose `environment`, as ordered pairs so the editor can
  // hold a half-typed row without dropping it.
  env: { key: string; value: string }[]
  // Volumes — compose `volumes`, kept as raw mount strings ("name:/path").
  volumes: string[]
  /**
   * Keys present on this service in the YAML that the visual editor does not
   * render. Listed in the UI so the spec has no invisible parts, and carried
   * through untouched on write.
   */
  unmodelled: string[]
  // Database
  dbEngine: string
  dbVersion: string
  dbStorageGB: number | ""
}

/** Service keys the visual editor renders. Anything else is reported as unmodelled. */
const MODELLED_KEYS = new Set(["image", "environment", "volumes", "x-meshploy"])

/** Reads compose `environment` in either form: a map, or a list of "K=V". */
function readEnv(raw: unknown): { key: string; value: string }[] {
  if (Array.isArray(raw)) {
    return raw.map((e) => {
      const str = String(e)
      const i = str.indexOf("=")
      return i < 0 ? { key: str, value: "" } : { key: str.slice(0, i), value: str.slice(i + 1) }
    })
  }
  if (raw && typeof raw === "object") {
    return Object.entries(raw as Record<string, unknown>).map(([key, value]) => ({
      key,
      value: value == null ? "" : String(value),
    }))
  }
  return []
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function newService(): VisualService {
  return {
    _key: crypto.randomUUID(),
    _origName: "",
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
    env: [],
    volumes: [],
    unmodelled: [],
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
          _origName: name,
          serviceType: "database" as const,
          name,
          env: readEnv(svc?.environment),
          volumes: Array.isArray(svc?.volumes) ? svc.volumes.map(String) : [],
          unmodelled: Object.keys(svc ?? {}).filter((k) => !MODELLED_KEYS.has(k)),
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
        _origName: name,
        serviceType: "app" as const,
        name,
        env: readEnv(svc?.environment),
        volumes: Array.isArray(svc?.volumes) ? svc.volumes.map(String) : [],
        unmodelled: Object.keys(svc ?? {}).filter((k) => !MODELLED_KEYS.has(k)),
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
        // Absent means absent. Defaulting these on read materialises resource
        // limits the spec never had as soon as the user opens the visual tab.
        cpuRequest: deploy.cpu_request ?? "",
        cpuLimit: deploy.cpu_limit ?? "",
        memoryRequest: deploy.memory_request ?? "",
        memoryLimit: deploy.memory_limit ?? "",
      }
    })
  } catch {
    return []
  }
}

/**
 * writeEnv updates a service's `environment`, but only when it actually
 * differs from what is already in the document.
 *
 * Replacing an unchanged block would rewrite it as a plain map and drop the
 * comments inside it — which is how a template's explanation of WHY a variable
 * is set disappears just because someone opened the visual tab. Leaving an
 * untouched block alone keeps both the comments and the author's chosen form
 * (map or "K=V" list).
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function writeEnv(doc: any, name: string, env: { key: string; value: string }[]) {
  const rows = env.filter((e) => e.key.trim() !== "")
  const current = readEnv(doc.getIn(["services", name, "environment"])?.toJSON?.() ?? doc.getIn(["services", name, "environment"]))
  const same =
    current.length === rows.length &&
    current.every((c, i) => c.key === rows[i].key && c.value === rows[i].value)
  if (same) return
  if (rows.length === 0) {
    doc.deleteIn(["services", name, "environment"])
    return
  }
  const obj: Record<string, string> = {}
  for (const r of rows) obj[r.key.trim()] = r.value
  doc.setIn(["services", name, "environment"], doc.createNode(obj))
}

/** writeVolumes mirrors writeEnv for the service's mount list. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function writeVolumes(doc: any, name: string, volumes: string[]) {
  const rows = volumes.map((v) => v.trim()).filter(Boolean)
  const raw = doc.getIn(["services", name, "volumes"])
  const current: string[] = Array.isArray(raw?.toJSON?.() ?? raw) ? (raw.toJSON?.() ?? raw).map(String) : []
  if (current.length === rows.length && current.every((c, i) => c === rows[i])) return
  if (rows.length === 0) {
    doc.deleteIn(["services", name, "volumes"])
    return
  }
  doc.setIn(["services", name, "volumes"], doc.createNode(rows))
}

/**
 * visualToYaml applies the visual editor's changes back onto the ORIGINAL
 * document, rather than rebuilding one from the visual model.
 *
 * The model only describes what the visual editor can edit: image, source,
 * build and deploy. A compose file holds much more — environment, volumes,
 * depends_on, healthcheck, command, labels, the top-level volumes and networks
 * blocks, and comments. Regenerating from the model deleted every one of them,
 * so merely opening the visual tab and switching back silently dropped a
 * service's volume mount and its environment, and the user then deployed a spec
 * that no longer persisted anything.
 *
 * Editing the parsed document preserves untouched keys and comments, and only
 * the fields the visual editor owns are written.
 */
export function visualToYaml(services: VisualService[], originalSpec = ""): string {
  const doc = (() => {
    try {
      const d = parseDocument(originalSpec)
      return d.errors.length === 0 ? d : parseDocument("")
    } catch {
      return parseDocument("")
    }
  })()

  if (!isMap(doc.get("services"))) doc.set("services", doc.createNode({}))
  const svcMap = doc.get("services") as ReturnType<typeof doc.createNode> & {
    items: { key: { value: string } }[]
  }

  // Drop services the visual editor removed. Compared on the original key, so a
  // rename is a rename rather than a delete plus an add that loses everything.
  const keep = new Set(services.map((s) => s._origName).filter(Boolean))
  for (const item of [...(svcMap.items ?? [])]) {
    const key = String(item.key?.value ?? "")
    if (key && !keep.has(key)) doc.deleteIn(["services", key])
  }

  const setIn = (path: (string | number)[], v: unknown) => {
    if (v === "" || v === undefined || v === null) doc.deleteIn(path)
    else doc.setIn(path, v)
  }

  for (const s of services) {
    const name = s.name.trim() || s._origName || "service"

    // Rename in place, carrying the whole node across so unmodelled keys move
    // with it.
    if (s._origName && s._origName !== name) {
      const node = doc.getIn(["services", s._origName])
      doc.deleteIn(["services", s._origName])
      doc.setIn(["services", name], node ?? doc.createNode({}))
    }
    if (!isMap(doc.getIn(["services", name]))) {
      doc.setIn(["services", name], doc.createNode({}))
    }

    if (s.serviceType === "database") {
      const engine = s.dbEngine || "postgres"
      const version = s.dbVersion || "latest"
      setIn(["services", name, "image"], `${engine}:${version}`)
      setIn(["services", name, "x-meshploy", "type"], "database")
      setIn(["services", name, "x-meshploy", "database", "engine"], engine)
      setIn(["services", name, "x-meshploy", "database", "version"], version)
      setIn(["services", name, "x-meshploy", "database", "storage_gb"], s.dbStorageGB || 10)
      setIn(["services", name, "x-meshploy", "deploy", "port"], s.port || dbDefaultPort(engine))
      setIn(["services", name, "x-meshploy", "deploy", "replicas"], s.replicas || 1)
      setIn(["services", name, "x-meshploy", "deploy", "node"], s.nodeId)
      continue
    }

    if (s.source === "image") {
      setIn(["services", name, "image"], s.image)
      // An image service has no git source or build step; clear any left from a
      // previous git configuration rather than leaving a contradictory spec.
      doc.deleteIn(["services", name, "x-meshploy", "source"])
      doc.deleteIn(["services", name, "x-meshploy", "build"])
    } else {
      doc.deleteIn(["services", name, "image"])
      setIn(["services", name, "x-meshploy", "source", "integration_id"], s.integrationId)
      setIn(["services", name, "x-meshploy", "source", "git"], s.gitRepo)
      setIn(["services", name, "x-meshploy", "source", "branch"], s.gitBranch)
      setIn(["services", name, "x-meshploy", "build", "builder"], s.builder)
      setIn(["services", name, "x-meshploy", "build", "builder_node"], s.builderNodeName)
      setIn(["services", name, "x-meshploy", "build", "builder_cpu_request"], s.builderCPURequest)
      setIn(["services", name, "x-meshploy", "build", "builder_memory_request"], s.builderMemoryRequest)
    }

    setIn(["services", name, "x-meshploy", "deploy", "port"], s.port || 3000)
    setIn(["services", name, "x-meshploy", "deploy", "replicas"], s.replicas || 1)
    setIn(["services", name, "x-meshploy", "deploy", "node"], s.nodeId)
    // Written only when set. Empty clears the key instead of pinning a default,
    // so a spec that never declared limits does not acquire them by being looked at.
    setIn(["services", name, "x-meshploy", "deploy", "cpu_request"], s.cpuRequest)
    setIn(["services", name, "x-meshploy", "deploy", "cpu_limit"], s.cpuLimit)
    setIn(["services", name, "x-meshploy", "deploy", "memory_request"], s.memoryRequest)
    setIn(["services", name, "x-meshploy", "deploy", "memory_limit"], s.memoryLimit)

    writeEnv(doc, name, s.env)
    writeVolumes(doc, name, s.volumes)
  }

  return doc.toString({ lineWidth: 120 })
}

// ─── StackEditor ──────────────────────────────────────────────────────────────

interface StackEditorProps {
  value: string
  onChange?: (value: string) => void
  minHeight?: string
  readOnly?: boolean
}

export function StackEditor({ value, onChange, minHeight = "360px", readOnly = false }: StackEditorProps) {
  const [mode, setMode] = useState<"yaml" | "visual">("yaml")
  const [visual, setVisual] = useState<VisualService[]>([])
  const [justConverted, setJustConverted] = useState(false)

  const switchToVisual = useCallback(() => {
    setVisual(yamlToVisual(value))
    setMode("visual")
  }, [value])

  const switchToYaml = useCallback(() => {
    onChange?.(visualToYaml(visual, value))
    setMode("yaml")
  }, [visual, value, onChange])

  const handleConvert = useCallback(() => {
    onChange?.(convertCompose(value))
    setJustConverted(true)
    setTimeout(() => setJustConverted(false), 2000)
  }, [value, onChange])

  const patchService = (key: string, patch: Partial<VisualService>) =>
    setVisual((prev) => prev.map((s) => (s._key === key ? { ...s, ...patch } : s)))

  const addService = () => setVisual((prev) => [...prev, newService()])
  const removeService = (key: string) => setVisual((prev) => prev.filter((s) => s._key !== key))

  const canConvert = !readOnly && mode === "yaml" && specNeedsConversion(value)

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
        {!readOnly && (
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
        )}
      </div>

      {mode === "yaml" ? (
        <CodeMirror
          value={value}
          height={minHeight}
          theme="dark"
          extensions={[yaml()]}
          onChange={readOnly ? undefined : onChange}
          editable={!readOnly}
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

        <EnvAndVolumeFields svc={svc} onChange={onChange} />
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

// ─── EnvAndVolumeFields ───────────────────────────────────────────────────────

/**
 * Environment, volumes, and a note of anything else the spec carries.
 *
 * Rendered for every service type. The visual and YAML tabs are two views of one
 * document, so a key that exists in the YAML has to be visible here too —
 * otherwise the visual tab quietly under-reports what a service is, and editing
 * through it feels like it lost something even when it did not.
 */
function EnvAndVolumeFields({
  svc,
  onChange,
}: {
  svc: VisualService
  onChange: (p: Partial<VisualService>) => void
}) {
  const setEnv = (i: number, patch: Partial<{ key: string; value: string }>) =>
    onChange({ env: svc.env.map((e, idx) => (idx === i ? { ...e, ...patch } : e)) })

  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <SectionHeader title="Environment" subtitle="Variables passed to the container" />
        {svc.env.length === 0 && (
          <p className="text-xs text-muted-foreground/60">No environment variables.</p>
        )}
        {svc.env.map((e, i) => (
          <div key={i} className="flex items-center gap-2">
            <input
              value={e.key}
              onChange={(ev) => setEnv(i, { key: ev.target.value })}
              placeholder="KEY"
              className={`${inputCls} font-mono flex-1`}
            />
            <input
              value={e.value}
              onChange={(ev) => setEnv(i, { value: ev.target.value })}
              placeholder="value"
              className={`${inputCls} font-mono flex-1`}
            />
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 shrink-0 text-muted-foreground hover:text-destructive"
              aria-label={`Remove ${e.key || "variable"}`}
              onClick={() => onChange({ env: svc.env.filter((_, idx) => idx !== i) })}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
        <Button
          size="sm"
          variant="outline"
          className="gap-1.5"
          onClick={() => onChange({ env: [...svc.env, { key: "", value: "" }] })}
        >
          <Plus className="h-3.5 w-3.5" />
          Add variable
        </Button>
      </div>

      <div className="space-y-2">
        <SectionHeader title="Volumes" subtitle="Mounts, as name:/path/in/container" />
        {svc.volumes.length === 0 && (
          <p className="text-xs text-muted-foreground/60">No volumes mounted.</p>
        )}
        {svc.volumes.map((v, i) => (
          <div key={i} className="flex items-center gap-2">
            <input
              value={v}
              onChange={(ev) =>
                onChange({ volumes: svc.volumes.map((x, idx) => (idx === i ? ev.target.value : x)) })
              }
              placeholder="data:/var/lib/data"
              className={`${inputCls} font-mono flex-1`}
            />
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 shrink-0 text-muted-foreground hover:text-destructive"
              aria-label={`Remove mount ${v}`}
              onClick={() => onChange({ volumes: svc.volumes.filter((_, idx) => idx !== i) })}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
        <Button
          size="sm"
          variant="outline"
          className="gap-1.5"
          onClick={() => onChange({ volumes: [...svc.volumes, ""] })}
        >
          <Plus className="h-3.5 w-3.5" />
          Add volume
        </Button>
      </div>

      {svc.unmodelled.length > 0 && (
        <div className="rounded-lg border border-border/60 bg-muted/20 p-3">
          <p className="text-xs font-medium text-foreground">Also set in YAML</p>
          <p className="text-[11px] text-muted-foreground/70 mt-0.5">
            This service also sets{" "}
            <span className="font-mono text-muted-foreground">{svc.unmodelled.join(", ")}</span>. There
            is no field for {svc.unmodelled.length === 1 ? "it" : "them"} here yet — edit in the YAML
            tab. {svc.unmodelled.length === 1 ? "It is" : "They are"} kept as written.
          </p>
        </div>
      )}
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
