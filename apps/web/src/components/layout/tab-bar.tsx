"use client"

import { Home, Database, Terminal, Activity, X } from "lucide-react"
import { useTabStore, type SessionTab } from "@/store/tab-store"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

const TAB_ICONS: Record<string, React.ElementType> = {
  explorer:          Database,
  terminal:          Terminal,
  metrics:           Activity,
  "service-terminal": Terminal,
}

export function TabBar() {
  const { tabs, activeTabId, setActiveTab, closeTab } = useTabStore()

  // Don't render the bar at all when no session tabs are open.
  if (tabs.length === 0) return null

  return (
    <div role="group" aria-label="Open sessions" className="flex items-end gap-0 border-b border-border/40 bg-background overflow-x-auto shrink-0 scrollbar-none">
      {/* Main tab — always first */}
      <Tab
        label="Main"
        icon={<Home className="h-3 w-3" />}
        active={activeTabId === null}
        onClick={() => setActiveTab(null)}
        closeable={false}
      />

      {tabs.map((tab) => {
        const Icon = TAB_ICONS[tab.type] ?? Terminal
        return (
          <Tab
            key={tab.id}
            label={tab.label}
            icon={<Icon className="h-3 w-3" />}
            active={activeTabId === tab.id}
            onClick={() => setActiveTab(tab.id)}
            onClose={() => closeTab(tab.id)}
            closeable
          />
        )
      })}
    </div>
  )
}

function Tab({
  label,
  icon,
  active,
  onClick,
  onClose,
  closeable,
}: {
  label: string
  icon: React.ReactNode
  active: boolean
  onClick: () => void
  onClose?: () => void
  closeable: boolean
}) {
  return (
    <div className={cn("group flex items-center shrink-0 border-b-2 border-r border-r-border/30 -mb-px", active ? "border-b-primary bg-secondary/40" : "border-b-transparent")}>
      <Button variant="ghost" onClick={onClick} aria-pressed={active} className={cn("gap-2 px-3 py-2 text-xs font-medium rounded-none", active ? "text-foreground" : "text-muted-foreground")}>
        {icon}<span className="max-w-[160px] truncate">{label}</span>
      </Button>
      {closeable && onClose && <button aria-label={`Close ${label}`} onClick={onClose} className="mr-2 rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"><X className="size-3" /></button>}
    </div>
  )
}
