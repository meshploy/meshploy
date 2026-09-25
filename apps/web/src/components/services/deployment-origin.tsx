import { ArrowDown, ArrowUp, GitBranch, GitCommitHorizontal, Package, RefreshCw, RotateCcw } from "lucide-react"
import { cn } from "@/lib/utils"

/** Where a deployment's image came from, as a deployment or a board cell carries it. */
export interface DeploymentOrigin {
  source?: string
  source_branch?: string
  source_commit?: string
  source_commit_message?: string
  from_level?: string
}

const sourceIcon = {
  build: GitBranch,
  promotion: ArrowUp,
  bring_down: ArrowDown,
  rollback: RotateCcw,
  image: Package,
  redeploy: RefreshCw,
} as const

/** The headline: built from a branch, or moved here from another level. */
export function originTitle(o: DeploymentOrigin): string {
  switch (o.source) {
    case "build": return o.source_branch ? `Built from ${o.source_branch}` : "Built here"
    case "promotion": return `Promoted from ${o.from_level || "the level below"}`
    case "bring_down": return `Brought down from ${o.from_level || "the level above"}`
    case "rollback": return "Rolled back"
    case "image": return "Deployed from an image"
    case "redeploy": return o.source_branch ? `Redeployed, built from ${o.source_branch}` : "Redeployed"
    default: return ""
  }
}

/**
 * A board card's line: "Built from develop · a1b2c3d Fix login", or
 * "Promoted from staging · develop · a1b2c3d Fix login", the message truncated.
 */
export function OriginLine({ origin, className }: { origin: DeploymentOrigin; className?: string }) {
  if (!origin.source) return null
  const Icon = sourceIcon[origin.source as keyof typeof sourceIcon] ?? GitBranch
  const moved = origin.source === "promotion" || origin.source === "bring_down"
  return (
    <p
      className={cn("flex min-w-0 items-center gap-1 text-xs text-muted-foreground", className)}
      title={[originTitle(origin), origin.source_commit_message].filter(Boolean).join(": ")}
      data-testid="origin-line"
    >
      <Icon className={cn("h-3.5 w-3.5 shrink-0", moved ? "text-sky-300/90" : "text-foreground/80")} />
      <span className={cn("shrink-0", moved ? "text-sky-300/90" : "text-foreground/80")}>{originTitle(origin)}</span>
      {moved && origin.source_branch && <span className="shrink-0">· {origin.source_branch}</span>}
      {origin.source_commit && <span className="shrink-0">· <span className="font-mono text-foreground/70">{origin.source_commit.slice(0, 7)}</span></span>}
      {origin.source_commit_message && <span className="min-w-0 truncate">{origin.source_commit_message}</span>}
    </p>
  )
}

/** The service page's strip: what is running here and where it came from. */
export function OriginStrip({ origin, image }: { origin: DeploymentOrigin; image?: string }) {
  if (!origin.source) return null
  const Icon = sourceIcon[origin.source as keyof typeof sourceIcon] ?? GitBranch
  const moved = origin.source === "promotion" || origin.source === "bring_down"
  return (
    <div
      className={cn(
        "mb-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-md border px-3 py-2 text-xs",
        moved ? "border-sky-400/25 bg-sky-400/[0.06]" : "border-border bg-muted/20",
      )}
      data-testid="origin-strip"
    >
      <span className={cn("inline-flex items-center gap-1.5 font-medium", moved ? "text-sky-300" : "text-foreground")}>
        <Icon className="h-3.5 w-3.5" />
        {originTitle(origin)}
      </span>
      {moved && origin.source_branch && (
        <span className="inline-flex items-center gap-1 text-muted-foreground">
          <GitBranch className="h-3 w-3" />
          <span className="font-mono">{origin.source_branch}</span>
        </span>
      )}
      {origin.source_commit && (
        <span className="inline-flex min-w-0 items-center gap-1 text-muted-foreground">
          <GitCommitHorizontal className="h-3 w-3 shrink-0" />
          <span className="font-mono text-foreground/90">{origin.source_commit.slice(0, 7)}</span>
          {origin.source_commit_message && <span className="truncate max-w-[42ch]">{origin.source_commit_message}</span>}
        </span>
      )}
      {image && !origin.source_commit && (
        <span className="truncate font-mono text-muted-foreground">{image}</span>
      )}
    </div>
  )
}
