import { formatBytes } from "@/lib/utils"
import type { MeshLoad, NodeLoad } from "./use-mesh-load"

/** The one-line summary for the Infrastructure panel's header. */
export function MeshLoadSummary({ load }: { load: MeshLoad }) {
  if (load.reporting === 0) return null
  return (
    <span className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <Measure label="Memory" used={load.total.memUsed} total={load.total.memTotal} />
      <Measure label="Disk" used={load.total.diskUsed} total={load.total.diskTotal} />
    </span>
  )
}

function Measure({ label, used, total }: { label: string; used: number; total: number }) {
  if (total === 0) return null
  return (
    <span className="flex items-center gap-1.5">
      {label}
      <span className="text-foreground tabular-nums">
        {formatBytes(used)} / {formatBytes(total)}
      </span>
      <Bar pct={(used / total) * 100} className="w-12" />
    </span>
  )
}

/** A node's own usage, for its row in the list. */
export function NodeLoadBars({ load }: { load?: NodeLoad }) {
  if (!load) return <span className="text-xs text-muted-foreground/60">no reading</span>
  const memPct = load.memTotal > 0 ? (load.memUsed / load.memTotal) * 100 : 0
  const diskPct = load.diskTotal > 0 ? (load.diskUsed / load.diskTotal) * 100 : 0
  return (
    <span className="flex items-center gap-4 text-xs text-muted-foreground">
      <span className="flex items-center gap-1.5">
        mem
        <Bar pct={memPct} className="w-14" />
        <span className="tabular-nums w-8 text-right">{Math.round(memPct)}%</span>
      </span>
      <span className="flex items-center gap-1.5">
        disk
        <Bar pct={diskPct} className="w-14" />
        <span className="tabular-nums w-8 text-right">{Math.round(diskPct)}%</span>
      </span>
    </span>
  )
}

/**
 * Amber past 80%, red past 92%. The thresholds are where a deployment starts
 * being refused rather than where a chart looks dramatic.
 */
function Bar({ pct, className = "" }: { pct: number; className?: string }) {
  const clamped = Math.max(0, Math.min(100, pct))
  const colour =
    clamped >= 92 ? "bg-destructive" : clamped >= 80 ? "bg-amber-400" : "bg-primary/70"
  return (
    <span className={`inline-block h-1.5 rounded-full bg-muted/40 overflow-hidden ${className}`}>
      <span className={`block h-full rounded-full ${colour}`} style={{ width: `${clamped}%` }} />
    </span>
  )
}
