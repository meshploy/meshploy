import { useState, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { parse as parseYaml, stringify as stringifyYaml } from "yaml"
import CodeMirror from "@uiw/react-codemirror"
import { yaml } from "@codemirror/lang-yaml"
import { Plus, Trash2, Code2, LayoutGrid } from "lucide-react"
import {
  SiPostgresql, SiMysql, SiRedis, SiMongodb, SiClickhouse,
} from "@icons-pack/react-simple-icons"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import { inputCls, Field } from "@/components/services/form-primitives"
import { gitIntegrations as gitApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { cn } from "@/lib/utils"

// ─── Types ────────────────────────────────────────────────────────────────────

export type VisualBuilder = "nixpacks" | "railpack" | "dockerfile"

const DB_ENGINES = [
  { value: "postgres",   label: "PostgreSQL",  versions: ["17", "16", "15", "14", "13"], port: 5432,  icon: SiPostgresql },
  { value: "mysql",      label: "MySQL",        versions: ["8.0", "5.7"],                 port: 3306,  icon: SiMysql },
  { value: "redis",      label: "Redis",        versions: ["7", "6"],                     port: 6379,  icon: SiRedis },
  { value: "mongodb",    label: "MongoDB",      versions: ["7", "6"],                     port: 27017, icon: SiMongodb },
  { value: "clickhouse", label: "ClickHouse",   versions: ["24", "23"],                   port: 9000,  icon: SiClickhouse },
  { value: "dragonfly",  label: "Dragonfly",    versions: ["latest"],                     port: 6379,  icon: null },
]

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
  builder: VisualBuilder
  // App — deploy
  port: number | ""
  replicas: number | ""
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
    builder: "nixpacks",
    port: 3000,
    replicas: 1,
    dbEngine: "postgres",
    dbVersion: "16",
    dbStorageGB: 10,
  }
}

function defaultDbPort(engine: string): number {
  return DB_ENGINES.find((e) => e.value === engine)?.port ?? 5432
}

function defaultDbVersions(engine: string): string[] {
  return DB_ENGINES.find((e) => e.value === engine)?.versions ?? ["latest"]
}

// ─── YAML ↔ Visual ────────────────────────────────────────────────────────────

// eslint-disable-next-line @typescript-eslint/no-explicit-any
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
        return {
          _key: crypto.randomUUID(),
          serviceType: "database" as const,
          name,
          source: "image",
          image: "",
          integrationId: "",
          gitRepo: "",
          gitBranch: "main",
          builder: "nixpacks",
          port: mp?.deploy?.port ?? defaultDbPort(db.engine ?? "postgres"),
          replicas: mp?.deploy?.replicas ?? 1,
          dbEngine: db.engine ?? "postgres",
          dbVersion: db.version ?? "16",
          dbStorageGB: db.storage_gb ?? 10,
        }
      }

      const integrationId: string = mp?.source?.integration_id ?? ""
      const gitRepo: string = mp?.source?.git ?? ""
      const gitBranch: string = mp?.source?.branch ?? "main"
      const builder: VisualBuilder = mp?.build?.builder ?? "nixpacks"
      const image: string = svc?.image ?? ""
      const source = integrationId || gitRepo ? "git" : image ? "image" : "git"

      return {
        _key: crypto.randomUUID(),
        serviceType: "app" as const,
        name,
        source: source as "image" | "git",
        image,
        integrationId,
        gitRepo,
        gitBranch,
        builder,
        port: mp?.deploy?.port ?? 3000,
        replicas: mp?.deploy?.replicas ?? 1,
        dbEngine: "postgres",
        dbVersion: "16",
        dbStorageGB: 10,
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
      svcs[name] = {
        image: `${engine}:${s.dbVersion || "latest"}`,
        "x-meshploy": {
          type: "database",
          database: {
            engine,
            version: s.dbVersion || "latest",
            storage_gb: s.dbStorageGB || 10,
          },
          deploy: {
            port: s.port || defaultDbPort(engine),
            replicas: s.replicas || 1,
          },
        },
      }
    } else {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mp: any = {
        deploy: { port: s.port || 3000, replicas: s.replicas || 1 },
      }
      if (s.source === "git") {
        mp.source = {
          integration_id: s.integrationId,
          git: s.gitRepo,
          branch: s.gitBranch,
        }
        mp.build = { builder: s.builder }
      }
      svcs[name] = {
        image: s.source === "image" ? s.image : "",
        "x-meshploy": mp,
      }
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

  const switchToVisual = useCallback(() => {
    setVisual(yamlToVisual(value))
    setMode("visual")
  }, [value])

  const switchToYaml = useCallback(() => {
    const next = visualToYaml(visual)
    onChange(next)
    setMode("yaml")
  }, [visual, onChange])

  const patchService = (key: string, patch: Partial<VisualService>) =>
    setVisual((prev) => prev.map((s) => (s._key === key ? { ...s, ...patch } : s)))

  const addService = () => setVisual((prev) => [...prev, newService()])

  const removeService = (key: string) =>
    setVisual((prev) => prev.filter((s) => s._key !== key))

  return (
    <div className="flex flex-col rounded-md border border-border/60 overflow-hidden">
      {/* Mode toggle bar */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-border/60 bg-muted/20 shrink-0">
        <span className="text-xs text-muted-foreground font-medium">
          {mode === "yaml" ? "YAML" : "Visual"}
        </span>
        <div className="flex items-center rounded-md border border-border/60 overflow-hidden text-xs">
          <button
            onClick={() => { if (mode === "visual") switchToYaml() }}
            className={cn(
              "flex items-center gap-1.5 px-2.5 py-1 transition-colors",
              mode === "yaml"
                ? "bg-primary/10 text-primary font-medium"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <Code2 className="h-3 w-3" />
            YAML
          </button>
          <div className="w-px h-4 bg-border/60" />
          <button
            onClick={() => { if (mode === "yaml") switchToVisual() }}
            className={cn(
              "flex items-center gap-1.5 px-2.5 py-1 transition-colors",
              mode === "visual"
                ? "bg-primary/10 text-primary font-medium"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <LayoutGrid className="h-3 w-3" />
            Visual
          </button>
        </div>
      </div>

      {mode === "yaml" ? (
        <CodeMirror
          value={value}
          height={minHeight}
          theme="dark"
          extensions={[yaml()]}
          onChange={onChange}
          style={{ fontSize: 13 }}
          basicSetup={{
            lineNumbers: true,
            foldGutter: true,
            autocompletion: true,
            indentOnInput: true,
          }}
        />
      ) : (
        <div
          className="flex flex-col gap-3 p-4 overflow-y-auto"
          style={{ minHeight }}
        >
          {visual.length === 0 && (
            <p className="text-xs text-muted-foreground text-center py-6">
              No services. Add one below.
            </p>
          )}
          {visual.map((svc) => (
            <ServiceCard
              key={svc._key}
              svc={svc}
              onChange={(patch) => patchService(svc._key, patch)}
              onRemove={() => removeService(svc._key)}
            />
          ))}
          <button
            onClick={addService}
            className="flex items-center justify-center gap-1.5 rounded-md border border-dashed border-border/60 py-2.5 text-xs text-muted-foreground hover:text-foreground hover:border-border transition-colors"
          >
            <Plus className="h-3.5 w-3.5" />
            Add service
          </button>
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

  const dbEngineOpts = DB_ENGINES
  const dbVersionOpts = defaultDbVersions(svc.dbEngine)

  return (
    <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-3 px-3 py-2.5 border-b border-border/40 bg-muted/10">
        <input
          value={svc.name}
          onChange={(e) => onChange({ name: e.target.value })}
          placeholder="service-name"
          className="flex-1 bg-transparent text-sm font-medium text-foreground placeholder:text-muted-foreground/40 focus:outline-none"
        />

        {/* App / Database toggle */}
        <div className="flex items-center rounded-md border border-border/60 overflow-hidden text-[11px]">
          <button
            onClick={() => onChange({ serviceType: "app" })}
            className={cn(
              "px-2.5 py-1 transition-colors",
              svc.serviceType === "app"
                ? "bg-primary/10 text-primary font-medium"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            App
          </button>
          <div className="w-px h-3.5 bg-border/60" />
          <button
            onClick={() => onChange({ serviceType: "database" })}
            className={cn(
              "px-2.5 py-1 transition-colors",
              svc.serviceType === "database"
                ? "bg-primary/10 text-primary font-medium"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Database
          </button>
        </div>

        <button
          onClick={onRemove}
          className="text-muted-foreground hover:text-destructive transition-colors shrink-0"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Body */}
      <div className="p-3 space-y-3">
        {svc.serviceType === "database" ? (
          <DatabaseFields svc={svc} onChange={onChange} engineOpts={dbEngineOpts} versionOpts={dbVersionOpts} />
        ) : (
          <AppFields
            svc={svc}
            onChange={onChange}
            connectedGit={connectedGit}
            repoList={repoList}
            branchList={branchList}
            reposFetching={reposFetching}
            branchesFetching={branchesFetching}
          />
        )}
      </div>
    </div>
  )
}

// ─── App fields ───────────────────────────────────────────────────────────────

function AppFields({
  svc,
  onChange,
  connectedGit,
  repoList,
  branchList,
  reposFetching,
  branchesFetching,
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
}) {
  return (
    <>
      {/* Source toggle */}
      <Field label="Source">
        <div className="flex rounded-lg border border-border/60 overflow-hidden w-fit">
          {(["git", "image"] as const).map((src) => (
            <button
              key={src}
              onClick={() => onChange({ source: src })}
              className={cn(
                "px-4 py-1.5 text-xs transition-colors",
                svc.source === src
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              {src === "git" ? "Git repository" : "Docker image"}
            </button>
          ))}
        </div>
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
              onValueChange={(v) =>
                onChange({ integrationId: v ?? "", gitRepo: "", gitBranch: "" })
              }
            >
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue
                  placeholder={
                    connectedGit.length === 0
                      ? "No connected integrations"
                      : "Select a git integration…"
                  }
                >
                  {connectedGit.find((g) => g.id === svc.integrationId)?.name}
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

          <Field
            label={reposFetching ? "Repository (loading…)" : "Repository"}
            required
          >
            <Select
              value={svc.gitRepo}
              onValueChange={(v) => {
                const repo = repoList.find((r) => r.full_name === v)
                onChange({ gitRepo: v ?? "", gitBranch: repo?.default_branch ?? "" })
              }}
              disabled={!svc.integrationId || reposFetching}
            >
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue
                  placeholder={
                    !svc.integrationId
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

          <div className="grid grid-cols-2 gap-3">
            <Field
              label={branchesFetching ? "Branch (loading…)" : "Branch"}
              required
            >
              <Select
                value={svc.gitBranch}
                onValueChange={(v) => onChange({ gitBranch: v ?? "" })}
                disabled={!svc.gitRepo || branchesFetching}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      !svc.gitRepo
                        ? "Select a repo first"
                        : branchesFetching
                        ? "Loading branches…"
                        : "Select a branch…"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {branchList.map((b) => (
                    <SelectItem key={b} value={b}>
                      {b}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <Field label="Builder">
              <Select
                value={svc.builder}
                onValueChange={(v) => onChange({ builder: (v ?? "nixpacks") as VisualBuilder })}
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
        </>
      )}

      <div className="grid grid-cols-2 gap-3">
        <Field label="Port">
          <input
            type="number"
            value={svc.port}
            onChange={(e) =>
              onChange({ port: e.target.value === "" ? "" : Number(e.target.value) })
            }
            placeholder="3000"
            className={inputCls}
          />
        </Field>
        <Field label="Replicas">
          <input
            type="number"
            value={svc.replicas}
            onChange={(e) =>
              onChange({ replicas: e.target.value === "" ? "" : Number(e.target.value) })
            }
            placeholder="1"
            min={1}
            className={inputCls}
          />
        </Field>
      </div>
    </>
  )
}

// ─── Database fields ──────────────────────────────────────────────────────────

function DatabaseFields({
  svc,
  onChange,
  engineOpts,
  versionOpts,
}: {
  svc: VisualService
  onChange: (p: Partial<VisualService>) => void
  engineOpts: typeof DB_ENGINES
  versionOpts: string[]
}) {
  return (
    <>
      {/* Engine picker */}
      <Field label="Engine">
        <div className="grid grid-cols-3 gap-2">
          {engineOpts.map((eng) => {
            const Icon = eng.icon
            return (
              <button
                key={eng.value}
                onClick={() =>
                  onChange({
                    dbEngine: eng.value,
                    dbVersion: eng.versions[0],
                    port: eng.port,
                  })
                }
                className={cn(
                  "flex items-center gap-2 px-2.5 py-2 rounded-lg border text-left transition-colors",
                  svc.dbEngine === eng.value
                    ? "border-primary/50 bg-primary/10 text-foreground"
                    : "border-border/60 bg-muted/10 text-muted-foreground hover:text-foreground hover:bg-muted/30"
                )}
              >
                {Icon ? (
                  <Icon className="h-3.5 w-3.5 shrink-0" />
                ) : (
                  <span className="h-3.5 w-3.5 flex items-center justify-center text-[9px] font-bold shrink-0">DF</span>
                )}
                <span className="text-xs truncate">{eng.label}</span>
              </button>
            )
          })}
        </div>
      </Field>

      <div className="grid grid-cols-3 gap-3">
        <Field label="Version">
          <Select
            value={svc.dbVersion}
            onValueChange={(v) => onChange({ dbVersion: v ?? versionOpts[0] })}
          >
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {versionOpts.map((v) => (
                <SelectItem key={v} value={v}>
                  {v}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field label="Storage (GB)">
          <input
            type="number"
            value={svc.dbStorageGB}
            onChange={(e) =>
              onChange({ dbStorageGB: e.target.value === "" ? "" : Number(e.target.value) })
            }
            placeholder="10"
            min={1}
            className={inputCls}
          />
        </Field>

        <Field label="Replicas">
          <input
            type="number"
            value={svc.replicas}
            onChange={(e) =>
              onChange({ replicas: e.target.value === "" ? "" : Number(e.target.value) })
            }
            placeholder="1"
            min={1}
            className={inputCls}
          />
        </Field>
      </div>
    </>
  )
}
