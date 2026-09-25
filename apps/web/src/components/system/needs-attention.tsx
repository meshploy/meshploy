import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { AlertCircle, AlertTriangle, ArrowUpRight, CircleCheck, Info, Loader2 } from "lucide-react"
import type { ApiAttentionItem } from "@/lib/api"
import { cn } from "@/lib/utils"

/**
 * What in the workspace needs someone, most urgent first: something down,
 * something that will hurt later, something waiting on a person.
 *
 * Shown in full only when there is something; otherwise one quiet line, so an
 * empty space never has to be read as "all clear" when it could equally be a
 * check that did not run. A failed check says so instead of claiming all clear.
 */
const SHOWN = 5

export function NeedsAttention({ items, loading, failed }: { items: ApiAttentionItem[]; loading: boolean; failed: boolean }) {
  const [all, setAll] = useState(false)
  if (loading) {
    return (
      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        Checking what needs attention…
      </p>
    )
  }
  if (failed) {
    return (
      <p className="flex items-center gap-2 text-xs text-amber-400" role="status">
        <AlertTriangle className="size-3.5" />
        Could not check what needs attention.
      </p>
    )
  }
  if (items.length === 0) {
    return (
      <p className="flex items-center gap-2 text-xs text-muted-foreground" role="status" data-testid="all-clear">
        <CircleCheck className="size-3.5 text-emerald-400" />
        Nothing needs attention.
      </p>
    )
  }
  const shown = all ? items : items.slice(0, SHOWN)
  return (
    <section aria-labelledby="attention-heading" className="quiet-surface overflow-hidden rounded-xl border border-border bg-card">
      <div className="flex items-center justify-between border-b border-border/40 px-4 py-3">
        <h2 id="attention-heading" className="text-sm font-semibold">
          Needs attention <span className="ml-1 font-normal text-muted-foreground tabular-nums">{items.length}</span>
        </h2>
      </div>
      <ul className="divide-y divide-border/30">
        {shown.map((item, i) => (
          <li key={`${item.kind}-${i}`}>
            <AttentionRow item={item} />
          </li>
        ))}
      </ul>
      {items.length > SHOWN && (
        <button
          type="button"
          onClick={() => setAll(!all)}
          className="w-full border-t border-border/40 px-4 py-2 text-left text-xs text-muted-foreground hover:text-foreground"
        >
          {all ? "Show fewer" : `+${items.length - SHOWN} more`}
        </button>
      )}
    </section>
  )
}

const TONE = {
  critical: { icon: AlertCircle, cls: "text-red-400" },
  warning: { icon: AlertTriangle, cls: "text-amber-400" },
  info: { icon: Info, cls: "text-sky-400" },
} as const

function AttentionRow({ item }: { item: ApiAttentionItem }) {
  const { icon: Icon, cls } = TONE[item.severity]
  const body = (
    <>
      <Icon className={cn("mt-0.5 size-4 shrink-0", cls)} />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium leading-tight">{item.title}</p>
        {item.detail && <p className="mt-0.5 truncate text-xs text-muted-foreground">{item.detail}</p>}
      </div>
      <ArrowUpRight className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
    </>
  )
  const className = "flex items-start gap-3 px-4 py-2.5 transition-colors hover:bg-secondary/40"
  const p = item.project_id
  // Where each kind is dealt with.
  if (item.kind === "node_offline" && item.node_id)
    return <Link to="/nodes/$id" params={{ id: item.node_id }} className={className}>{body}</Link>
  if ((item.kind === "domain_unverified" || item.kind === "former_primary") && item.domain_id)
    return <Link to="/domains/$domainId" params={{ domainId: item.domain_id }} className={className}>{body}</Link>
  if (item.kind === "job_failed" && p && item.job_id)
    return <Link to="/projects/$id/jobs/$jobId/runs" params={{ id: p, jobId: item.job_id }} className={className}>{body}</Link>
  if (item.kind === "deploy_failed" && p && item.service_id && item.deployment_id)
    return (
      <Link to="/projects/$id/services/$serviceId/deployments/$deploymentId" params={{ id: p, serviceId: item.service_id, deploymentId: item.deployment_id }} className={className}>
        {body}
      </Link>
    )
  if (item.kind === "backup_missing" && p && !item.service_id)
    return <Link to="/projects/$id/databases" params={{ id: p }} className={className}>{body}</Link>
  if ((item.kind === "backup_missing" || item.kind === "backup_failed") && p && item.service_id)
    return <Link to="/projects/$id/services/$serviceId/backups" params={{ id: p, serviceId: item.service_id }} className={className}>{body}</Link>
  if (item.kind === "promotion_waiting" && p)
    return <Link to="/projects/$id" params={{ id: p }} className={className}>{body}</Link>
  if (p && item.service_id)
    return <Link to="/projects/$id/services/$serviceId" params={{ id: p, serviceId: item.service_id }} className={className}>{body}</Link>
  return <div className={className}>{body}</div>
}
