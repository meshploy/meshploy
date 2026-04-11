import { cn } from "@/lib/utils"
import { useRouterState, Link } from "@tanstack/react-router"
import {
  FolderKanban,
  Server,
  Network,
  Plug,
  Settings,
  ChevronLeft,
  ChevronRight,
} from "lucide-react"
import { useUIStore } from "@/store/ui-store"
import { Separator } from "@/components/ui/separator"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { OrgSwitcher } from "./org-switcher"

const NAV_ITEMS = [
  { href: "/projects", icon: FolderKanban, label: "Projects" },
  { href: "/nodes", icon: Server, label: "Nodes" },
  { href: "/cluster", icon: Network, label: "Cluster" },
  { href: "/integrations", icon: Plug, label: "Integrations" },
  { href: "/settings", icon: Settings, label: "Settings" },
] as const

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const { sidebarCollapsed, toggleSidebar } = useUIStore()

  return (
    <aside
      className={cn(
        "flex flex-col h-screen border-r border-sidebar-border bg-sidebar shrink-0 transition-[width] duration-200 ease-in-out",
        sidebarCollapsed ? "w-[60px]" : "w-[220px]"
      )}
    >
      {/* Logo */}
      <div className={cn("flex items-center h-14 px-4 border-b border-sidebar-border shrink-0", sidebarCollapsed ? "justify-center" : "gap-2.5")}>
        <div className="flex items-center justify-center w-7 h-7 rounded-md bg-primary shrink-0">
          <Network className="w-4 h-4 text-primary-foreground" />
        </div>
        {!sidebarCollapsed && (
          <span className="font-semibold tracking-tight text-sidebar-foreground text-sm">meshploy</span>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex flex-col gap-0.5 p-2 flex-1 overflow-y-auto">
        {NAV_ITEMS.map((item) => {
          const isActive = pathname.startsWith(item.href)

          if (sidebarCollapsed) {
            return (
              <Tooltip key={item.href}>
                <TooltipTrigger
                  render={
                    <Link
                      to={item.href}
                      className={cn(
                        "flex items-center justify-center h-9 w-9 rounded-md mx-auto transition-colors",
                        isActive
                          ? "bg-sidebar-primary/15 text-sidebar-primary"
                          : "text-sidebar-foreground/50 hover:text-sidebar-foreground hover:bg-sidebar-accent"
                      )}
                    />
                  }
                >
                  <item.icon className="h-4 w-4" />
                </TooltipTrigger>
                <TooltipContent side="right" className="text-xs">{item.label}</TooltipContent>
              </Tooltip>
            )
          }

          return (
            <Link
              key={item.href}
              to={item.href}
              className={cn(
                "flex items-center gap-2.5 h-9 px-3 rounded-md text-sm transition-colors",
                isActive
                  ? "bg-sidebar-primary/10 text-sidebar-primary font-medium"
                  : "text-sidebar-foreground/50 hover:text-sidebar-foreground hover:bg-sidebar-accent"
              )}
            >
              <item.icon className="h-4 w-4 shrink-0" />
              {item.label}
            </Link>
          )
        })}
      </nav>

      {/* Bottom: org switcher + collapse toggle */}
      <div className="p-2 shrink-0 space-y-1">
        <Separator className="mb-2 bg-sidebar-border" />
        {!sidebarCollapsed && (
          <div className="px-1 pb-1">
            <OrgSwitcher />
          </div>
        )}
        <Tooltip>
          <TooltipTrigger
            onClick={toggleSidebar}
            className={cn(
              "flex items-center h-8 w-full rounded-md text-xs text-sidebar-foreground/40 hover:text-sidebar-foreground/70 hover:bg-sidebar-accent transition-colors",
              sidebarCollapsed ? "justify-center" : "gap-2 px-3"
            )}
          >
            {sidebarCollapsed ? (
              <ChevronRight className="h-3.5 w-3.5" />
            ) : (
              <>
                <ChevronLeft className="h-3.5 w-3.5" />
                <span>Collapse</span>
              </>
            )}
          </TooltipTrigger>
          {sidebarCollapsed && (
            <TooltipContent side="right" className="text-xs">Expand sidebar</TooltipContent>
          )}
        </Tooltip>
      </div>
    </aside>
  )
}
