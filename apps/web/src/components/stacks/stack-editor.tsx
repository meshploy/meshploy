import { useState, useCallback } from "react"
import { parse as parseYaml, stringify as stringifyYaml } from "yaml"
import CodeMirror from "@uiw/react-codemirror"
import { yaml } from "@codemirror/lang-yaml"
import { Plus, Trash2, Code2, LayoutGrid } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

// ─── Types ────────────────────────────────────────────────────────────────────

export type VisualServiceSource = "image" | "git"
export type VisualBuilder = "nixpacks" | "railpack" | "dockerfile"

export interface VisualService {
  _key: string
  name: string
  source: VisualServiceSource
  image: string
  git: string
  branch: string
  builder: VisualBuilder
  port: number | ""
  replicas: number | ""
}

// ─── YAML ↔ Visual conversion ─────────────────────────────────────────────────

function newService(): VisualService {
  return {
    _key: crypto.randomUUID(),
    name: "",
    source: "git",
    image: "",
    git: "",
    branch: "main",
    builder: "nixpacks",
    port: 3000,
    replicas: 1,
  }
}

export function yamlToVisual(spec: string): VisualService[] {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const doc = parseYaml(spec) as any
    const svcs = doc?.services
    if (!svcs || typeof svcs !== "object") return []
    return Object.entries(svcs).map(([name, svcRaw]) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const svc = svcRaw as any
      const mp = svc?.["x-meshploy"] ?? {}
      const image: string = svc?.image ?? ""
      const git: string = mp?.source?.git ?? ""
      const branch: string = mp?.source?.branch ?? "main"
      const builder: VisualBuilder = mp?.build?.builder ?? "nixpacks"
      const port: number = mp?.deploy?.port ?? 3000
      const replicas: number = mp?.deploy?.replicas ?? 1
      const source: VisualServiceSource = git ? "git" : "image"
      return {
        _key: crypto.randomUUID(),
        name,
        source,
        image,
        git,
        branch,
        builder,
        port,
        replicas,
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const svc: any = {}
    if (s.source === "image") {
      svc.image = s.image
    } else {
      svc.image = ""
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const mp: any = { deploy: { port: s.port || 3000, replicas: s.replicas || 1 } }
    if (s.source === "git") {
      mp.source = { git: s.git, branch: s.branch }
      mp.build = { builder: s.builder }
    }
    svc["x-meshploy"] = mp
    svcs[name] = svc
  }
  return stringifyYaml({ services: svcs }, { lineWidth: 120 })
}

// ─── Component ────────────────────────────────────────────────────────────────

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
    const yaml = visualToYaml(visual)
    onChange(yaml)
    setMode("yaml")
  }, [visual, onChange])

  const patchService = (key: string, patch: Partial<VisualService>) => {
    setVisual((prev) =>
      prev.map((s) => (s._key === key ? { ...s, ...patch } : s))
    )
  }

  const addService = () => {
    setVisual((prev) => [...prev, newService()])
  }

  const removeService = (key: string) => {
    setVisual((prev) => prev.filter((s) => s._key !== key))
  }

  return (
    <div className="flex flex-col gap-0 rounded-md border border-border/60 overflow-hidden">
      {/* Mode toggle */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-border/60 bg-muted/20">
        <span className="text-xs text-muted-foreground font-medium">
          {mode === "yaml" ? "YAML" : "Visual"}
        </span>
        <div className="flex items-center rounded-md border border-border/60 overflow-hidden text-xs">
          <button
            onClick={() => mode === "visual" ? switchToYaml() : undefined}
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
            onClick={() => mode === "yaml" ? switchToVisual() : undefined}
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
        <div className="flex flex-col gap-3 p-4 overflow-y-auto" style={{ minHeight }}>
          {visual.length === 0 && (
            <p className="text-xs text-muted-foreground text-center py-6">
              No services defined. Add one below.
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

// ─── Service card ─────────────────────────────────────────────────────────────

const fieldCls =
  "w-full rounded-md border border-border/60 bg-transparent px-2.5 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-1 focus:ring-primary/40 transition-colors"

const labelCls = "text-[11px] text-muted-foreground mb-1 block"

function ServiceCard({
  svc,
  onChange,
  onRemove,
}: {
  svc: VisualService
  onChange: (patch: Partial<VisualService>) => void
  onRemove: () => void
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-card">
      {/* Card header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-border/40">
        <input
          value={svc.name}
          onChange={(e) => onChange({ name: e.target.value })}
          placeholder="service-name"
          className="bg-transparent text-sm font-medium text-foreground placeholder:text-muted-foreground/40 focus:outline-none w-48"
        />
        <button
          onClick={onRemove}
          className="text-muted-foreground hover:text-destructive transition-colors"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="p-3 grid grid-cols-2 gap-3">
        {/* Source toggle */}
        <div className="col-span-2">
          <label className={labelCls}>Source</label>
          <div className="flex items-center rounded-md border border-border/60 overflow-hidden text-xs w-fit">
            {(["git", "image"] as const).map((src) => (
              <button
                key={src}
                onClick={() => onChange({ source: src })}
                className={cn(
                  "px-3 py-1 transition-colors capitalize",
                  svc.source === src
                    ? "bg-primary/10 text-primary font-medium"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                {src === "git" ? "Git" : "Image"}
              </button>
            ))}
          </div>
        </div>

        {svc.source === "image" ? (
          <div className="col-span-2">
            <label className={labelCls}>Image</label>
            <input
              value={svc.image}
              onChange={(e) => onChange({ image: e.target.value })}
              placeholder="nginx:latest"
              className={fieldCls}
            />
          </div>
        ) : (
          <>
            <div className="col-span-2">
              <label className={labelCls}>Repository URL</label>
              <input
                value={svc.git}
                onChange={(e) => onChange({ git: e.target.value })}
                placeholder="https://github.com/org/repo"
                className={fieldCls}
              />
            </div>
            <div>
              <label className={labelCls}>Branch</label>
              <input
                value={svc.branch}
                onChange={(e) => onChange({ branch: e.target.value })}
                placeholder="main"
                className={fieldCls}
              />
            </div>
            <div>
              <label className={labelCls}>Builder</label>
              <select
                value={svc.builder}
                onChange={(e) => onChange({ builder: e.target.value as VisualBuilder })}
                className={fieldCls}
              >
                <option value="nixpacks">Nixpacks</option>
                <option value="railpack">Railpack</option>
                <option value="dockerfile">Dockerfile</option>
              </select>
            </div>
          </>
        )}

        <div>
          <label className={labelCls}>Port</label>
          <input
            type="number"
            value={svc.port}
            onChange={(e) => onChange({ port: e.target.value === "" ? "" : Number(e.target.value) })}
            placeholder="3000"
            className={fieldCls}
          />
        </div>
        <div>
          <label className={labelCls}>Replicas</label>
          <input
            type="number"
            value={svc.replicas}
            onChange={(e) => onChange({ replicas: e.target.value === "" ? "" : Number(e.target.value) })}
            placeholder="1"
            min={1}
            className={fieldCls}
          />
        </div>
      </div>
    </div>
  )
}
