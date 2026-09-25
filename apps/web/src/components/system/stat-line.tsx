import { cn } from "@/lib/utils"

/**
 * A count broken down by state or kind, as a line of coloured dots: the
 * project overview's resource cards and the workspace overview's stat cards.
 */

// Each kind's breakdown, in the order worth reading it, and how each part looks.
const STAT_ORDER: Record<string, string[]> = {
  services: ["running", "deploying", "failed", "stopped"],
  databases: ["running", "deploying", "failed", "stopped"],
  stacks: ["running", "applying", "failed", "idle", "destroyed"],
  routes: ["https", "internal", "tcp", "paused"],
  volumes: ["ready", "idle", "failed"],
  variables: ["shared", "published"],
  config_files: ["attached", "unused"],
  jobs: ["scheduled", "manual", "failed"],
}
const STAT_LABEL: Record<string, string> = { https: "HTTPS", tcp: "TCP", published: "from services" }
// Green is live, amber on its way, red failed, grey inactive. A part that is a
// kind rather than a state (a scheduled job, a shared group) is blue.
const STAT_TONE: Record<string, string> = {
  running: "bg-emerald-400", ready: "bg-emerald-400", https: "bg-emerald-400", internal: "bg-emerald-400", tcp: "bg-emerald-400", attached: "bg-emerald-400",
  deploying: "bg-amber-400", applying: "bg-amber-400",
  failed: "bg-red-400",
  stopped: "bg-muted-foreground/50", idle: "bg-muted-foreground/50", paused: "bg-muted-foreground/50", unused: "bg-muted-foreground/50", destroyed: "bg-muted-foreground/50",
}

export function StatLine({ kind, stats, className }: { kind: string; stats?: Record<string, number>; className?: string }) {
  const parts = (STAT_ORDER[kind] ?? []).filter((k) => stats?.[k])
  if (!stats || parts.length === 0) return null
  return (
    <p className={cn("mt-3 flex flex-wrap gap-x-3 gap-y-1 border-t border-border/60 pt-3 text-xs text-muted-foreground", className)} data-testid={`stats-${kind}`}>
      {parts.map((k) => (
        <span key={k} className={cn("inline-flex items-center gap-1.5", k === "failed" && "text-red-300")}>
          <span className={cn("size-1.5 rounded-full", STAT_TONE[k] ?? "bg-sky-400")} />
          <span className="tabular-nums text-foreground/90">{stats[k]}</span> {STAT_LABEL[k] ?? k}
        </span>
      ))}
    </p>
  )
}
