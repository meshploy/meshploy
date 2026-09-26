import { cn } from "@/lib/utils"
import type { LogLevel } from "@/lib/log-level"

// Ordinary lines carry nothing but their number; a line that matters gets a
// mark in the gutter and its own colour, so a long log reads like a terminal
// and the few lines worth stopping at are the only ones that stand out.
const LEVEL_STYLES: Record<LogLevel, { mark: string; markClass: string; row: string; text: string }> = {
  error: { mark: "✕", markClass: "text-red-400", row: "bg-red-500/[0.08]", text: "text-red-300" },
  warning: { mark: "!", markClass: "text-amber-400", row: "", text: "text-amber-200/90" },
  success: { mark: "✓", markClass: "text-emerald-400", row: "", text: "text-emerald-300/90" },
  info: { mark: "", markClass: "", row: "", text: "text-muted-foreground" },
}

/**
 * One line of a build or deploy log: its number, a mark for errors, warnings
 * and successes, and the text. Error rows are tinted so they stand out while
 * scrolling a long log.
 */
export function BuildLogLine({ text, level, number }: { text: string; level: LogLevel; number: number }) {
  const s = LEVEL_STYLES[level]
  return (
    <div data-level={level} className={cn("group flex items-start rounded-sm", s.row)}>
      <span className="w-12 shrink-0 select-none pr-2 text-right tabular-nums text-muted-foreground/35 group-hover:text-muted-foreground/70">
        {number}
      </span>
      <span className={cn("w-4 shrink-0 select-none text-center font-semibold", s.markClass)} aria-hidden={!s.mark}>
        {s.mark}
      </span>
      <span className={cn("min-w-0 flex-1 whitespace-pre-wrap break-all pl-1 pr-1", s.text)}>{text || " "}</span>
    </div>
  )
}
