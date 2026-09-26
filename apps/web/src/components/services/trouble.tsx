import { Link } from "@tanstack/react-router"
import { AlertOctagon, ArrowRight, MemoryStick, RotateCcw, ImageOff, Ban } from "lucide-react"
import type { ApiTrouble } from "@/lib/api"
import { cn, formatRelativeTime } from "@/lib/utils"

/**
 * Why a service is not staying up, in words: an out-of-memory kill with the
 * limit it hit, a process that keeps exiting, an image that cannot be pulled,
 * a container that cannot start. Read from its pods, since a pod that keeps
 * dying still reads "Running" most of the time.
 */
const KIND = {
  out_of_memory: { icon: MemoryStick, title: "Killed for using too much memory", tag: "out of memory" },
  crashing: { icon: RotateCcw, title: "Keeps stopping and restarting", tag: "crashing" },
  image_pull: { icon: ImageOff, title: "Cannot pull its image", tag: "image pull failing" },
  cannot_start: { icon: Ban, title: "Cannot start", tag: "cannot start" },
} as const

function detail(t: ApiTrouble) {
  const restarts = `${t.restarts} ${t.restarts === 1 ? "restart" : "restarts"}${t.last_at ? `, the last ${formatRelativeTime(new Date(t.last_at))}` : ""}`
  switch (t.kind) {
    case "out_of_memory":
      return `${restarts}. ${t.memory_limit ? `Its memory limit is ${t.memory_limit}; ` : ""}raise it, or find what uses more than expected.`
    case "crashing":
      return `${restarts}, exiting with code ${t.exit_code ?? "?"}. Its log says why.`
    default:
      return t.message || restarts
  }
}

/** The service page's banner, with the one place to fix it. */
export function TroubleBanner({ trouble: t, projectId, serviceId }: { trouble: ApiTrouble; projectId: string; serviceId: string }) {
  const k = KIND[t.kind] ?? { icon: AlertOctagon, title: "Not staying up", tag: "failing" }
  const Icon = k.icon
  const action =
    t.kind === "crashing"
      ? { to: "/projects/$id/services/$serviceId/logs" as const, label: "Open logs" }
      : { to: "/projects/$id/services/$serviceId/config" as const, label: t.kind === "out_of_memory" ? "Raise memory limit" : "Open configuration" }
  return (
    <div role="alert" className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-red-500/30 bg-red-500/[0.07] px-3 py-2.5" data-testid="trouble-banner">
      <Icon className="size-4 shrink-0 text-red-400" />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-red-300">{k.title}</p>
        <p className="text-xs text-muted-foreground">{detail(t)}</p>
      </div>
      <Link
        to={action.to}
        params={{ id: projectId, serviceId }}
        className="inline-flex shrink-0 items-center gap-1 rounded-md border border-red-500/30 px-2.5 py-1 text-xs font-medium text-red-300 hover:bg-red-500/10"
      >
        {action.label}
        <ArrowRight className="size-3" />
      </Link>
    </div>
  )
}

/** A compact tag for lists and cards. */
export function TroubleTag({ trouble, className }: { trouble: ApiTrouble; className?: string }) {
  const k = KIND[trouble.kind]
  return (
    <span
      className={cn("inline-flex shrink-0 items-center gap-1 rounded-full border border-red-500/35 bg-red-500/10 px-1.5 text-[10px] font-medium text-red-300", className)}
      data-testid="trouble-tag"
    >
      {k?.tag ?? "failing"}
    </span>
  )
}
