"use client"

import { Home, Database, Terminal, X } from "lucide-react"
import { useTabStore, type SessionTab } from "@/store/tab-store"
import { cn } from "@/lib/utils"

const TAB_ICONS: Record<string, React.ElementType> = {
  explorer: Database,
  terminal: Terminal,
}

export function TabBar() {
  const { tabs, activeTabId, setActiveTab, closeTab } = useTabStore()

  // Don't render the bar at all when no session tabs are open.
  if (tabs.length === 0) return null

  return (
    <div className="flex items-end gap-0 border-b border-border/40 bg-background px-2 overflow-x-auto shrink-0 scrollbar-none">
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
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group relative flex items-center gap-1.5 px-3 py-2 text-xs font-medium transition-colors shrink-0 border-b-2 -mb-px",
        active
          ? "border-primary text-foreground"
          : "border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/30"
      )}
    >
      {icon}
      <span className="max-w-[120px] truncate">{label}</span>
      {closeable && onClose && (
        <span
          role="button"
          tabIndex={-1}
          onClick={(e) => { e.stopPropagation(); onClose() }}
          className={cn(
            "ml-0.5 rounded p-0.5 transition-colors",
            active
              ? "text-muted-foreground hover:text-foreground hover:bg-muted/50"
              : "opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-foreground"
          )}
        >
          <X className="h-2.5 w-2.5" />
        </span>
      )}
    </button>
  )
}
