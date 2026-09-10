import { cn } from "@/lib/utils"
import type { LogLevel } from "@/lib/log-level"

const LEVEL_STYLES: Record<LogLevel, { bar: string; badge: string; row: string; text: string; label: string }> = {
  error: {
    bar: "bg-destructive/80",
    badge: "bg-destructive/15 text-destructive border-destructive/30",
    row: "bg-destructive/[0.07]",
    text: "text-red-300/90",
    label: "error",
  },
  warning: {
    bar: "bg-amber-500/70",
    badge: "bg-amber-500/10 text-amber-400 border-amber-500/25",
    row: "bg-amber-500/[0.04]",
    text: "text-amber-200/80",
    label: "warn",
  },
  success: {
    bar: "bg-emerald-500/70",
    badge: "bg-emerald-500/10 text-emerald-400 border-emerald-500/25",
    row: "",
    text: "text-emerald-300/90",
    label: "success",
  },
  info: {
    bar: "bg-sky-500/35",
    badge: "bg-sky-500/10 text-sky-300/80 border-sky-500/20",
    row: "",
    text: "text-muted-foreground",
    label: "info",
  },
}

/**
 * One line of a build or deploy log: a level bar, a level badge, and the text.
 * Error lines are tinted so they stand out while scrolling a long log.
 */
export function BuildLogLine({ text, level }: { text: string; level: LogLevel }) {
  const s = LEVEL_STYLES[level]
  return (
    <div data-level={level} className={cn("flex items-stretch gap-2.5 rounded-sm pr-1", s.row)}>
      <span className={cn("w-1 shrink-0 rounded-full", s.bar)} />
      <span
        className={cn(
          "mt-[3px] h-4 w-14 shrink-0 select-none rounded-full border text-center text-[10px] font-medium leading-[14px]",
          s.badge,
        )}
      >
        {s.label}
      </span>
      <span className={cn("min-w-0 flex-1 whitespace-pre-wrap break-all py-px", s.text)}>{text || " "}</span>
    </div>
  )
}
