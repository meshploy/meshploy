import type { ReactNode } from "react"
import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

export function MetricTile({
  icon: Icon,
  label,
  value,
  unit,
  detail,
}: {
  icon: LucideIcon
  label: string
  value: ReactNode
  unit?: ReactNode
  detail?: ReactNode
}) {
  return (
    <div className="resource-metric">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className="size-4" />
        {label}
      </div>
      <div className="resource-metric-value">
        {value}
        <span className="resource-metric-unit">{unit}</span>
      </div>
      <div className="text-xs text-muted-foreground leading-relaxed">
        {detail}
      </div>
    </div>
  )
}
export function ResourcePanel({
  title,
  description,
  action,
  children,
  className,
}: {
  title: string
  description?: string
  action?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn("resource-panel", className)}>
      <header>
        <div>
          <h2>{title}</h2>
          {description && <p>{description}</p>}
        </div>
        {action}
      </header>
      <div className="resource-panel-body">{children}</div>
    </section>
  )
}
export function ResourceFact({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div className="resource-fact">
      <span>{label}</span>
      <div>{children}</div>
    </div>
  )
}
export function ResourceIntro({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action?: ReactNode
}) {
  return (
    <div className="resource-intro">
      <div>
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      {action}
    </div>
  )
}
export function StatusPill({ status }: { status: string }) {
  const tone = ["running", "success", "ready", "online", "applied"].includes(
    status
  )
    ? "good"
    : ["failed", "offline"].includes(status)
      ? "bad"
      : ["pending", "building", "deploying", "applying"].includes(status)
        ? "busy"
        : "neutral"
  return (
    <span className={`resource-status ${tone}`}>
      <i />
      {status === "success"
        ? "Successful"
        : status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  )
}
