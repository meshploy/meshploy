import { useState } from "react"
import { Plus, Trash2, Globe, Lock, ChevronsUpDown } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { Node, RouteZone, RouteTarget } from "@/types"

interface NewRouteModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  projectId: string
  nodes: Node[]
  baseDomain?: string
}

interface TargetRow {
  path: string
  nodeId: string
  port: string
  override: string
}

const defaultTarget = (): TargetRow => ({ path: "/", nodeId: "", port: "", override: "" })

export function NewRouteModal({
  open,
  onOpenChange,
  nodes,
  baseDomain = "acme.com",
}: NewRouteModalProps) {
  const [subdomain, setSubdomain] = useState("")
  const [zone, setZone] = useState<RouteZone>("public")
  const [targets, setTargets] = useState<TargetRow[]>([defaultTarget()])
  const [loading, setLoading] = useState(false)

  const previewDomain =
    subdomain
      ? zone === "internal"
        ? `${subdomain}.internal.${baseDomain}`
        : `${subdomain}.${baseDomain}`
      : zone === "internal"
        ? `<subdomain>.internal.${baseDomain}`
        : `<subdomain>.${baseDomain}`

  function patchTarget(index: number, patch: Partial<TargetRow>) {
    setTargets((prev) => prev.map((t, i) => (i === index ? { ...t, ...patch } : t)))
  }

  function addTarget() {
    setTargets((prev) => [...prev, defaultTarget()])
  }

  function removeTarget(index: number) {
    setTargets((prev) => prev.filter((_, i) => i !== index))
  }

  function handleSubmit() {
    const validTargets: RouteTarget[] = targets
      .filter((t) => t.nodeId && t.port)
      .map((t) => ({
        path: t.path || "/",
        nodeId: t.nodeId,
        port: parseInt(t.port, 10),
        override: t.override || undefined,
      }))
    if (!subdomain || validTargets.length === 0) return

    setLoading(true)
    // TODO: call API
    setTimeout(() => {
      setLoading(false)
      onOpenChange(false)
      setSubdomain("")
      setZone("public")
      setTargets([defaultTarget()])
    }, 600)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" showCloseButton>
        <DialogHeader>
          <DialogTitle>New Route</DialogTitle>
        </DialogHeader>

        <div className="space-y-5 py-1">
          {/* Zone toggle */}
          <div className="grid grid-cols-2 gap-2">
            {(["public", "internal"] as RouteZone[]).map((z) => (
              <button
                key={z}
                type="button"
                onClick={() => setZone(z)}
                className={cn(
                  "flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-sm transition-colors text-left",
                  zone === z
                    ? z === "public"
                      ? "border-sky-500/40 bg-sky-500/8 text-sky-300"
                      : "border-violet-500/40 bg-violet-500/8 text-violet-300"
                    : "border-border/60 bg-card text-muted-foreground hover:border-border hover:text-foreground"
                )}
              >
                {z === "public" ? (
                  <Globe className="h-4 w-4 shrink-0" />
                ) : (
                  <Lock className="h-4 w-4 shrink-0" />
                )}
                <div>
                  <p className="font-medium capitalize leading-none">{z}</p>
                  <p className="text-[11px] mt-0.5 opacity-70 leading-none">
                    {z === "public" ? "Internet-accessible" : "WireGuard mesh only"}
                  </p>
                </div>
              </button>
            ))}
          </div>

          {/* Subdomain input */}
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Subdomain
            </label>
            <div className="flex items-stretch rounded-lg border border-border/60 bg-input/30 overflow-hidden focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/20 transition-all">
              <input
                type="text"
                placeholder="my-service"
                value={subdomain}
                onChange={(e) => setSubdomain(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                className="flex-1 min-w-0 bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none"
              />
              <span className="flex items-center bg-muted/30 px-3 text-xs font-mono text-muted-foreground border-l border-border/60 shrink-0">
                .{zone === "internal" ? `internal.${baseDomain}` : baseDomain}
              </span>
            </div>
            {subdomain && (
              <p className="text-[11px] font-mono text-muted-foreground/60 pl-0.5">
                → {previewDomain}
              </p>
            )}
          </div>

          {/* Targets */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Targets
              </label>
              <button
                type="button"
                onClick={addTarget}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                <Plus className="h-3 w-3" />
                Add path
              </button>
            </div>

            <div className="space-y-2">
              {targets.map((target, i) => (
                <TargetRowEditor
                  key={i}
                  target={target}
                  nodes={nodes}
                  canRemove={targets.length > 1}
                  onChange={(patch) => patchTarget(i, patch)}
                  onRemove={() => removeTarget(i)}
                />
              ))}
            </div>
          </div>
        </div>

        <DialogFooter showCloseButton>
          <Button
            onClick={handleSubmit}
            disabled={!subdomain || targets.every((t) => !t.nodeId || !t.port) || loading}
          >
            {loading ? "Creating…" : "Create Route"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── TargetRowEditor ─────────────────────────────────────────────────────────

interface TargetRowEditorProps {
  target: TargetRow
  nodes: Node[]
  canRemove: boolean
  onChange: (patch: Partial<TargetRow>) => void
  onRemove: () => void
}

function TargetRowEditor({ target, nodes, canRemove, onChange, onRemove }: TargetRowEditorProps) {
  const [showOverride, setShowOverride] = useState(false)

  return (
    <div className="rounded-lg border border-border/50 bg-muted/10 p-3 space-y-2">
      <div className="grid grid-cols-[1fr_1.6fr_0.8fr] gap-2">
        {/* Path */}
        <div>
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider mb-1">Path</p>
          <input
            type="text"
            value={target.path}
            onChange={(e) => onChange({ path: e.target.value })}
            placeholder="/"
            className="w-full rounded-md border border-border/50 bg-input/20 px-2.5 py-1.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-ring focus:ring-2 focus:ring-ring/20 transition-all"
          />
        </div>

        {/* Node */}
        <div>
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider mb-1">Node</p>
          <div className="relative">
            <select
              value={target.nodeId}
              onChange={(e) => onChange({ nodeId: e.target.value })}
              className="w-full appearance-none rounded-md border border-border/50 bg-input/20 px-2.5 py-1.5 text-sm text-foreground outline-none focus:border-ring focus:ring-2 focus:ring-ring/20 transition-all pr-7"
            >
              <option value="" disabled>Select node</option>
              {nodes.filter((n) => n.status === "online").map((n) => (
                <option key={n.id} value={n.id}>{n.name}</option>
              ))}
            </select>
            <ChevronsUpDown className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          </div>
        </div>

        {/* Port */}
        <div>
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider mb-1">Port</p>
          <input
            type="number"
            min={1}
            max={65535}
            value={target.port}
            onChange={(e) => onChange({ port: e.target.value })}
            placeholder="3000"
            className="w-full rounded-md border border-border/50 bg-input/20 px-2.5 py-1.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-ring focus:ring-2 focus:ring-ring/20 transition-all"
          />
        </div>
      </div>

      <div className="flex items-center justify-between">
        <button
          type="button"
          onClick={() => setShowOverride((v) => !v)}
          className="text-[11px] text-muted-foreground/60 hover:text-muted-foreground transition-colors"
        >
          {showOverride ? "Hide" : "Override URL"}
        </button>
        {canRemove && (
          <button
            type="button"
            onClick={onRemove}
            className="text-muted-foreground/40 hover:text-destructive transition-colors"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {showOverride && (
        <div>
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider mb-1">
            Override URL
          </p>
          <input
            type="text"
            value={target.override}
            onChange={(e) => onChange({ override: e.target.value })}
            placeholder="http://host:port/path"
            className="w-full rounded-md border border-border/50 bg-input/20 px-2.5 py-1.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-ring focus:ring-2 focus:ring-ring/20 transition-all"
          />
        </div>
      )}
    </div>
  )
}
