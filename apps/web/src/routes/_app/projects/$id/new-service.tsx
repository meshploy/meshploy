import { createFileRoute, useNavigate, notFound } from "@tanstack/react-router"
import { useState } from "react"
import {
  Box,
  Database,
  Layers,
  LayoutGrid,
  ChevronLeft,
  ChevronRight,
  Check,
  GitBranch,
  Image,
  Server,
  Settings2,
  Globe,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { mockProjects, mockNodes, mockTemplates } from "@/lib/mock-data"

// ─── Route ──────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/_app/projects/$id/new-service")({
  loader: ({ params }) => {
    const project = mockProjects.find((p) => p.id === params.id)
    if (!project) throw notFound()
    return { project }
  },
  component: NewServicePage,
})

// ─── Types ───────────────────────────────────────────────────────────────────

type ServiceKind = "app" | "database" | "compose" | "template"
type AppSource = "image" | "git"
type Builder = "nixpacks" | "dockerfile" | "buildpack"
type DBEngine = "postgres" | "mysql" | "redis" | "mongodb"

interface WizardState {
  // Step 1
  kind: ServiceKind | null
  // Step 2 — App
  appName: string
  appSource: AppSource
  appImage: string
  gitRepo: string
  gitBranch: string
  builder: Builder
  // Step 2 — Database
  dbName: string
  dbEngine: DBEngine
  dbVersion: string
  dbStorageGB: number
  // Step 2 — Compose
  composeName: string
  composeYaml: string
  // Step 2 — Template
  templateId: string | null
  templateName: string
  // Step 3
  nodeId: string | null
  cpuRequest: string
  cpuLimit: string
  memoryRequest: string
  memoryLimit: string
  createRoute: boolean
}

const INITIAL: WizardState = {
  kind: null,
  appName: "", appSource: "image", appImage: "", gitRepo: "", gitBranch: "main", builder: "nixpacks",
  dbName: "", dbEngine: "postgres", dbVersion: "16", dbStorageGB: 10,
  composeName: "", composeYaml: "",
  templateId: null, templateName: "",
  nodeId: null, cpuRequest: "100m", cpuLimit: "500m", memoryRequest: "128Mi", memoryLimit: "512Mi",
  createRoute: false,
}

const STEPS = ["Type", "Configuration", "Deployment"]

// ─── Page ────────────────────────────────────────────────────────────────────

function NewServicePage() {
  const { project } = Route.useLoaderData()
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [state, setState] = useState<WizardState>(INITIAL)
  const [showAdvanced, setShowAdvanced] = useState(false)

  const patch = (partial: Partial<WizardState>) =>
    setState((s) => ({ ...s, ...partial }))

  const canNext =
    step === 0 ? state.kind !== null :
    step === 1 ? isStep2Valid(state) :
    true

  function handleCreate() {
    // TODO: POST /api/v1/workloads
    navigate({ to: "/projects/$id", params: { id: project.id } })
  }

  return (
    <div className="min-h-full bg-background">
      {/* Top bar */}
      <div className="border-b border-border/40 bg-background/80 backdrop-blur-sm sticky top-0 z-10">
        <div className="max-w-4xl mx-auto px-6 h-14 flex items-center gap-4">
          <button
            onClick={() => navigate({ to: "/projects/$id", params: { id: project.id } })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            {project.name}
          </button>
          <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40" />
          <span className="text-sm font-medium">New Service</span>
        </div>
      </div>

      <div className="max-w-4xl mx-auto px-6 py-8 space-y-8">
        {/* Step indicator */}
        <StepIndicator steps={STEPS} current={step} />

        {/* Step content */}
        <div className="min-h-[420px]">
          {step === 0 && <Step1Type state={state} patch={patch} />}
          {step === 1 && <Step2Config state={state} patch={patch} />}
          {step === 2 && (
            <Step3Deployment
              state={state}
              patch={patch}
              showAdvanced={showAdvanced}
              setShowAdvanced={setShowAdvanced}
            />
          )}
        </div>

        {/* Footer nav */}
        <div className="flex items-center justify-between pt-4 border-t border-border/40">
          <button
            onClick={() => step > 0 ? setStep(step - 1) : navigate({ to: "/projects/$id", params: { id: project.id } })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors px-4 py-2 rounded-md border border-border/60 hover:bg-muted/30"
          >
            {step === 0 ? "Cancel" : <><ChevronLeft className="h-3.5 w-3.5" /> Back</>}
          </button>

          {step < 2 ? (
            <button
              onClick={() => setStep(step + 1)}
              disabled={!canNext}
              className="flex items-center gap-1.5 text-sm bg-primary text-primary-foreground px-5 py-2 rounded-md hover:bg-primary/90 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              Next <ChevronRight className="h-3.5 w-3.5" />
            </button>
          ) : (
            <button
              onClick={handleCreate}
              className="flex items-center gap-1.5 text-sm bg-primary text-primary-foreground px-5 py-2 rounded-md hover:bg-primary/90 transition-colors"
            >
              <Check className="h-3.5 w-3.5" /> Create Service
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── Step 1 — Type ───────────────────────────────────────────────────────────

const KIND_CARDS = [
  { kind: "app" as ServiceKind, icon: Box, label: "App", desc: "Deploy a container application" },
  { kind: "database" as ServiceKind, icon: Database, label: "Database", desc: "Deploy a managed database" },
  { kind: "compose" as ServiceKind, icon: Layers, label: "Docker Compose", desc: "Deploy a multi-container stack" },
  { kind: "template" as ServiceKind, icon: LayoutGrid, label: "Template", desc: "Choose from pre-built templates" },
]

function Step1Type({ state, patch }: { state: WizardState; patch: (p: Partial<WizardState>) => void }) {
  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">What would you like to deploy?</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Choose a service type to get started</p>
      </div>
      <div className="grid grid-cols-2 gap-4">
        {KIND_CARDS.map(({ kind, icon: Icon, label, desc }) => (
          <button
            key={kind}
            onClick={() => patch({ kind })}
            className={cn(
              "flex flex-col items-center gap-4 rounded-xl border-2 p-8 text-center transition-all hover:border-primary/50 hover:bg-muted/20",
              state.kind === kind
                ? "border-primary bg-primary/5"
                : "border-border/60 bg-card"
            )}
          >
            <div className={cn(
              "flex items-center justify-center w-14 h-14 rounded-xl",
              state.kind === kind ? "bg-primary/15" : "bg-muted"
            )}>
              <Icon className={cn("h-7 w-7", state.kind === kind ? "text-primary" : "text-muted-foreground")} />
            </div>
            <div>
              <p className="font-semibold text-foreground">{label}</p>
              <p className="text-xs text-muted-foreground mt-1">{desc}</p>
            </div>
          </button>
        ))}
      </div>
    </div>
  )
}

// ─── Step 2 — Configuration ──────────────────────────────────────────────────

function Step2Config({ state, patch }: { state: WizardState; patch: (p: Partial<WizardState>) => void }) {
  if (state.kind === "app") return <Step2App state={state} patch={patch} />
  if (state.kind === "database") return <Step2Database state={state} patch={patch} />
  if (state.kind === "compose") return <Step2Compose state={state} patch={patch} />
  if (state.kind === "template") return <Step2Template state={state} patch={patch} />
  return null
}

function Step2App({ state, patch }: { state: WizardState; patch: (p: Partial<WizardState>) => void }) {
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Configure your application</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Set up the source and name for your app</p>
      </div>

      <Field label="Service name">
        <input value={state.appName} onChange={(e) => patch({ appName: e.target.value })}
          placeholder="my-api" className={inputCls} />
      </Field>

      {/* Source type toggle */}
      <div className="space-y-3">
        <label className="text-sm font-medium text-foreground">Source</label>
        <div className="flex rounded-lg border border-border/60 overflow-hidden">
          {([["image", Image, "Docker Image"], ["git", GitBranch, "Git Repository"]] as const).map(([src, Icon, label]) => (
            <button
              key={src}
              onClick={() => patch({ appSource: src })}
              className={cn(
                "flex-1 flex items-center justify-center gap-2 py-2.5 text-sm transition-colors",
                state.appSource === src
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              <Icon className="h-3.5 w-3.5" />{label}
            </button>
          ))}
        </div>
      </div>

      {state.appSource === "image" ? (
        <Field label="Docker image">
          <input value={state.appImage} onChange={(e) => patch({ appImage: e.target.value })}
            placeholder="nginx:alpine" className={inputCls} />
        </Field>
      ) : (
        <div className="space-y-4">
          <Field label="Repository URL">
            <input value={state.gitRepo} onChange={(e) => patch({ gitRepo: e.target.value })}
              placeholder="https://github.com/org/repo" className={inputCls} />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Branch">
              <input value={state.gitBranch} onChange={(e) => patch({ gitBranch: e.target.value })}
                placeholder="main" className={inputCls} />
            </Field>
            <Field label="Builder">
              <select value={state.builder} onChange={(e) => patch({ builder: e.target.value as Builder })} className={inputCls}>
                <option value="nixpacks">Nixpacks (auto-detect)</option>
                <option value="dockerfile">Dockerfile</option>
                <option value="buildpack">Buildpack</option>
              </select>
            </Field>
          </div>
        </div>
      )}
    </div>
  )
}

const DB_ENGINES: { engine: DBEngine; label: string; versions: string[] }[] = [
  { engine: "postgres", label: "PostgreSQL", versions: ["16", "15", "14", "13"] },
  { engine: "mysql", label: "MySQL", versions: ["8.4", "8.0", "5.7"] },
  { engine: "redis", label: "Redis", versions: ["7.4", "7.2", "6.2"] },
  { engine: "mongodb", label: "MongoDB", versions: ["7.0", "6.0", "5.0"] },
]

function Step2Database({ state, patch }: { state: WizardState; patch: (p: Partial<WizardState>) => void }) {
  const selected = DB_ENGINES.find((e) => e.engine === state.dbEngine)!

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Configure your database</h2>
        <p className="text-sm text-muted-foreground mt-0.5">
          A connection secret is auto-generated and injected into services that reference it
        </p>
      </div>

      <Field label="Service name">
        <input value={state.dbName} onChange={(e) => patch({ dbName: e.target.value })}
          placeholder="postgres-db" className={inputCls} />
      </Field>

      <div className="space-y-2">
        <label className="text-sm font-medium text-foreground">Engine</label>
        <div className="grid grid-cols-4 gap-2">
          {DB_ENGINES.map(({ engine, label }) => (
            <button
              key={engine}
              onClick={() => patch({ dbEngine: engine, dbVersion: DB_ENGINES.find(e => e.engine === engine)!.versions[0] })}
              className={cn(
                "flex flex-col items-center gap-1.5 rounded-lg border-2 p-3 text-xs font-medium transition-all",
                state.dbEngine === engine
                  ? "border-primary bg-primary/5 text-primary"
                  : "border-border/60 bg-card text-muted-foreground hover:border-border hover:text-foreground"
              )}
            >
              <Database className="h-5 w-5" />
              {label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Field label="Version">
          <select value={state.dbVersion} onChange={(e) => patch({ dbVersion: e.target.value })} className={inputCls}>
            {selected.versions.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
        </Field>
        <Field label="Storage (GB)">
          <input type="number" min={1} value={state.dbStorageGB}
            onChange={(e) => patch({ dbStorageGB: parseInt(e.target.value) || 10 })}
            className={inputCls} />
        </Field>
      </div>
    </div>
  )
}

function Step2Compose({ state, patch }: { state: WizardState; patch: (p: Partial<WizardState>) => void }) {
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Docker Compose</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Paste your compose.yml — services are deployed as a group</p>
      </div>
      <Field label="Stack name">
        <input value={state.composeName} onChange={(e) => patch({ composeName: e.target.value })}
          placeholder="my-stack" className={inputCls} />
      </Field>
      <Field label="compose.yml">
        <Textarea
          value={state.composeYaml}
          onChange={(e) => patch({ composeYaml: e.target.value })}
          placeholder={"services:\n  web:\n    image: nginx:alpine\n    ports:\n      - 80:80"}
          className="font-mono text-xs min-h-[280px] resize-y bg-muted/20 border-border/60"
        />
      </Field>
    </div>
  )
}

function Step2Template({ state, patch }: { state: WizardState; patch: (p: Partial<WizardState>) => void }) {
  const [search, setSearch] = useState("")
  const filtered = mockTemplates.filter((t) =>
    t.name.toLowerCase().includes(search.toLowerCase()) ||
    t.category.toLowerCase().includes(search.toLowerCase())
  )
  const categories = Array.from(new Set(mockTemplates.map((t) => t.category)))

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Choose a template</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Pre-built Docker Compose stacks, ready to deploy</p>
      </div>

      <input
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder="Search templates…"
        className={inputCls}
      />

      <div className="space-y-4 max-h-[340px] overflow-y-auto pr-1">
        {categories.map((cat) => {
          const items = filtered.filter((t) => t.category === cat)
          if (!items.length) return null
          return (
            <div key={cat} className="space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{cat}</p>
              <div className="grid grid-cols-3 gap-2">
                {items.map((t) => (
                  <button
                    key={t.id}
                    onClick={() => patch({ templateId: t.id, templateName: t.name })}
                    className={cn(
                      "flex flex-col gap-2 rounded-lg border-2 p-3 text-left transition-all",
                      state.templateId === t.id
                        ? "border-primary bg-primary/5"
                        : "border-border/60 bg-card hover:border-border/80 hover:bg-muted/20"
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <div className="flex items-center justify-center w-7 h-7 rounded-md bg-muted text-xs font-bold text-muted-foreground">
                        {t.name.slice(0, 2).toUpperCase()}
                      </div>
                      <span className="text-sm font-medium text-foreground truncate">{t.name}</span>
                    </div>
                    <p className="text-[11px] text-muted-foreground line-clamp-2">{t.description}</p>
                  </button>
                ))}
              </div>
            </div>
          )
        })}
        {filtered.length === 0 && (
          <p className="text-sm text-muted-foreground py-8 text-center">No templates match "{search}"</p>
        )}
      </div>
    </div>
  )
}

// ─── Step 3 — Deployment ─────────────────────────────────────────────────────

function Step3Deployment({
  state, patch, showAdvanced, setShowAdvanced,
}: {
  state: WizardState
  patch: (p: Partial<WizardState>) => void
  showAdvanced: boolean
  setShowAdvanced: (v: boolean) => void
}) {
  const workerNodes = mockNodes.filter((n) => n.status === "online" && n.k3sRole === "agent")

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Deployment settings</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Configure where and how this service runs</p>
      </div>

      {/* Node selection */}
      <div className="space-y-3">
        <label className="text-sm font-medium text-foreground flex items-center gap-2">
          <Server className="h-3.5 w-3.5 text-muted-foreground" />
          Target node
        </label>
        <div className="grid grid-cols-3 gap-2">
          <button
            onClick={() => patch({ nodeId: null })}
            className={cn(
              "flex flex-col gap-1 rounded-lg border-2 p-3 text-left transition-all",
              state.nodeId === null
                ? "border-primary bg-primary/5"
                : "border-border/60 bg-card hover:border-border/80"
            )}
          >
            <span className="text-sm font-medium">Auto-schedule</span>
            <span className="text-[11px] text-muted-foreground">Let K3s decide</span>
          </button>
          {workerNodes.map((node) => (
            <button
              key={node.id}
              onClick={() => patch({ nodeId: node.id })}
              className={cn(
                "flex flex-col gap-1 rounded-lg border-2 p-3 text-left transition-all",
                state.nodeId === node.id
                  ? "border-primary bg-primary/5"
                  : "border-border/60 bg-card hover:border-border/80"
              )}
            >
              <div className="flex items-center gap-1.5">
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                <span className="text-sm font-medium truncate">{node.name}</span>
              </div>
              <span className="text-[11px] text-muted-foreground font-mono">{node.tailscaleIP}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Advanced toggle */}
      <div>
        <button
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <Settings2 className="h-3.5 w-3.5" />
          {showAdvanced ? "Hide" : "Show"} advanced options
        </button>

        {showAdvanced && (
          <div className="mt-4 grid grid-cols-2 gap-4 p-4 rounded-lg border border-border/40 bg-muted/10">
            <Field label="CPU request"><input value={state.cpuRequest} onChange={(e) => patch({ cpuRequest: e.target.value })} className={inputCls} /></Field>
            <Field label="CPU limit"><input value={state.cpuLimit} onChange={(e) => patch({ cpuLimit: e.target.value })} className={inputCls} /></Field>
            <Field label="Memory request"><input value={state.memoryRequest} onChange={(e) => patch({ memoryRequest: e.target.value })} className={inputCls} /></Field>
            <Field label="Memory limit"><input value={state.memoryLimit} onChange={(e) => patch({ memoryLimit: e.target.value })} className={inputCls} /></Field>
          </div>
        )}
      </div>

      {/* Optional route */}
      <div className={cn(
        "flex items-center justify-between p-4 rounded-lg border transition-all",
        state.createRoute ? "border-primary/40 bg-primary/5" : "border-border/60 bg-card"
      )}>
        <div className="flex items-center gap-3">
          <Globe className="h-4 w-4 text-muted-foreground" />
          <div>
            <p className="text-sm font-medium">Expose with a route</p>
            <p className="text-xs text-muted-foreground">Configure a domain for this service after creation</p>
          </div>
        </div>
        <button
          onClick={() => patch({ createRoute: !state.createRoute })}
          className={cn(
            "relative inline-flex h-5 w-9 items-center rounded-full transition-colors",
            state.createRoute ? "bg-primary" : "bg-muted"
          )}
        >
          <span className={cn(
            "inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform shadow-sm",
            state.createRoute ? "translate-x-[18px]" : "translate-x-[3px]"
          )} />
        </button>
      </div>
    </div>
  )
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function StepIndicator({ steps, current }: { steps: string[]; current: number }) {
  return (
    <div className="flex items-center gap-0">
      {steps.map((label, i) => (
        <div key={i} className="flex items-center gap-0">
          <div className="flex items-center gap-2.5">
            <div className={cn(
              "flex items-center justify-center w-7 h-7 rounded-full text-xs font-semibold transition-colors",
              i < current ? "bg-primary text-primary-foreground" :
              i === current ? "bg-primary text-primary-foreground" :
              "bg-muted text-muted-foreground"
            )}>
              {i < current ? <Check className="h-3.5 w-3.5" /> : i + 1}
            </div>
            <span className={cn(
              "text-sm transition-colors",
              i === current ? "font-medium text-foreground" : "text-muted-foreground"
            )}>
              {label}
            </span>
          </div>
          {i < steps.length - 1 && (
            <div className={cn(
              "h-px w-12 mx-3 transition-colors",
              i < current ? "bg-primary/50" : "bg-border/60"
            )} />
          )}
        </div>
      ))}
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

const inputCls =
  "w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"

function isStep2Valid(state: WizardState): boolean {
  if (state.kind === "app") return state.appName.length > 0 && (state.appSource === "image" ? state.appImage.length > 0 : state.gitRepo.length > 0)
  if (state.kind === "database") return state.dbName.length > 0
  if (state.kind === "compose") return state.composeName.length > 0 && state.composeYaml.length > 0
  if (state.kind === "template") return state.templateId !== null
  return false
}
