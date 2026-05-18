import { Link } from "@tanstack/react-router"
import { ArrowLeft } from "lucide-react"
import { cn } from "@/lib/utils"

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
  children?: React.ReactNode  // tab nav items
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
  children,
}: DetailPageHeaderProps) {
  return (
    <div className="border-b border-border/40 bg-muted/10">
      <div className="px-6 pt-4 pb-0">
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
        <div className={cn("flex items-start justify-between gap-4", children ? "mb-2.5" : "mb-4")}>
          <div className="flex items-center gap-3 min-w-0">
            {/* Icon box */}
            <div className="w-9 h-9 rounded-md bg-muted/40 border border-border/40 flex items-center justify-center shrink-0">
              {icon}
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className={cn("text-base font-semibold leading-tight", nameClassName)}>
                  {name}
                </span>
                {badge}
              </div>
              {subtitle && (
                <div className="text-xs text-muted-foreground font-mono mt-0.5">{subtitle}</div>
              )}
            </div>
          </div>
          {actions && (
            <div className="flex items-center gap-2 shrink-0">{actions}</div>
          )}
        </div>

        {/* Tab nav */}
        {children && (
          <nav className="flex items-center -mb-px">{children}</nav>
        )}
      </div>
    </div>
  )
}

/** Class string for a router Link tab (uses data-[status=active] from TanStack Router). */
export const tabLinkCls =
  "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap " +
  "text-muted-foreground hover:text-foreground border-b-transparent hover:border-b-border/60 " +
  "data-[status=active]:text-foreground data-[status=active]:border-b-foreground/25"

/** Class string for a button/manual-active tab. */
export function tabItemCls(isActive: boolean) {
  return cn(
    "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap",
    isActive
      ? "text-foreground border-b-foreground/25"
      : "text-muted-foreground hover:text-foreground border-b-transparent hover:border-b-border/60"
  )
}
