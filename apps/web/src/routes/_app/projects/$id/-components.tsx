import type { LucideIcon } from "lucide-react"

export function ComingSoonTab({
  icon: Icon,
  title,
  description,
}: {
  icon: LucideIcon
  title: string
  description: string
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-24 text-center px-6">
      <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-muted/50">
        <Icon className="h-5 w-5 text-muted-foreground/60" />
      </div>
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="text-xs text-muted-foreground mt-1 max-w-xs">{description}</p>
      </div>
      <span className="text-[10px] font-mono text-muted-foreground/50 border border-border/40 px-2 py-0.5 rounded-full mt-1">
        coming soon
      </span>
    </div>
  )
}
