import { Link } from "@tanstack/react-router"
import { ArrowLeft } from "lucide-react"
import { cn } from "@/lib/utils"
import { HelpButton } from "@/help/help-button"

interface DetailPageHeaderProps {
  backTo: string
  backLabel: string
  backParams?: Record<string, string>
  icon: React.ReactNode
  name: string
  nameClassName?: string
  badge?: React.ReactNode
  subtitle?: React.ReactNode
  actions?: React.ReactNode
  /** Shown between the identity row and the tabs, for what matters most right now. */
  highlight?: React.ReactNode
  /** A help topic, opened by a "?" beside the name. */
  help?: { topic: string; label: string }
  children?: React.ReactNode // tab nav items
}

export function DetailPageHeader({
  backTo,
  backLabel,
  backParams,
  icon,
  name,
  nameClassName,
  badge,
  subtitle,
  actions,
  highlight,
  help,
  children,
}: DetailPageHeaderProps) {
  return (
    <div className="detail-header border-b border-border">
      <div className="detail-header-inner">
        {/* Back link */}
        <Link
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          to={backTo as any}
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          params={backParams as any}
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors mb-3"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          {backLabel}
        </Link>

        {/* Identity row */}
        <div
          className={cn(
            "detail-identity-row flex items-start justify-between gap-4",
            children ? "mb-2.5" : "mb-4"
          )}
        >
          <div className="flex items-center gap-3 min-w-0">
            {/* Icon box */}
            <div className="detail-resource-icon">{icon}</div>
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <h1
                  className={cn(
                    "detail-title font-semibold leading-tight",
                    nameClassName
                  )}
                >
                  {name}
                </h1>
                {help && <HelpButton topic={help.topic} label={help.label} />}
                {badge}
              </div>
              {subtitle && (
                <div className="text-sm text-muted-foreground mt-2">
                  {subtitle}
                </div>
              )}
            </div>
          </div>
          {actions && (
            <div className="detail-actions flex items-center gap-2 shrink-0">
              {actions}
            </div>
          )}
        </div>

        {highlight}

        {/* Tab nav */}
        {children && (
          <nav className="detail-tabs flex items-center -mb-px">{children}</nav>
        )}
      </div>
    </div>
  )
}

/** Class string for a router Link tab (uses data-[status=active] from TanStack Router). */
export const tabLinkCls =
  "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap " +
  "text-muted-foreground hover:text-foreground border-b-transparent hover:border-b-border/60 " +
  "data-[status=active]:text-foreground data-[status=active]:border-b-primary"

/** Class string for a button/manual-active tab. */
export function tabItemCls(isActive: boolean) {
  return cn(
    "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap",
    isActive
      ? "text-primary border-b-primary"
      : "text-muted-foreground hover:text-foreground border-b-transparent hover:border-b-border/60"
  )
}
