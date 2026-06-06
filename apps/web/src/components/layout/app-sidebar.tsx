import { cn } from "@/lib/utils"
import { useRouterState, Link } from "@tanstack/react-router"
import {
  ChevronLeft,
  ChevronRight,
  Download,
  FolderKanban,
  Home,
  Network,
  Plug,
  Server,
  Settings,
  Users,
} from "lucide-react"
import { useUIStore } from "@/store/ui-store"
import { useIsAdmin } from "@/store/org-store"
import { Separator } from "@/components/ui/separator"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useQuery } from "@tanstack/react-query"
import { system } from "@/lib/api/system"
import { useAuthStore } from "@/store/auth-store"

type NavItem = {
  href: string
  icon: React.ElementType
  label: string
  exact: boolean
}

type NavGroup = {
  label?: string
  adminOnly?: boolean
  items: NavItem[]
}

const NAV_GROUPS: NavGroup[] = [
  {
    items: [
      { href: "/", icon: Home, label: "Overview", exact: true },
      { href: "/projects", icon: FolderKanban, label: "Projects", exact: false },
      { href: "/nodes", icon: Server, label: "Nodes", exact: false },
      { href: "/cluster", icon: Network, label: "Cluster", exact: false },
    ],
  },
  {
    label: "System",
    adminOnly: true,
    items: [
      { href: "/integrations", icon: Plug, label: "Integrations", exact: false },
      { href: "/settings", icon: Settings, label: "Settings", exact: false },
      { href: "/users", icon: Users, label: "Users", exact: false },
    ],
  },
]

function MeshMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 100 100" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <polyline points="18,78 18,22 50,58 82,22 82,78" />
      <line x1="18" y1="78" x2="50" y2="58" opacity="0.45" />
      <line x1="82" y1="78" x2="50" y2="58" opacity="0.45" />
      <circle cx="18" cy="78" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="18" cy="22" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="50" cy="58" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="82" cy="22" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="82" cy="78" r="5.5" fill="currentColor" stroke="none" />
    </svg>
  )
}

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const { sidebarCollapsed, toggleSidebar } = useUIStore()
  const token = useAuthStore((s) => s.token)
  const isAdmin = useIsAdmin()
  const { data: ver } = useQuery({
    queryKey: ["system-version"],
    queryFn: () => system.versionInfo(token!),
    enabled: !!token,
    staleTime: 60 * 60 * 1000,
    retry: false,
  })

  return (
    <aside
      className={cn(
        "flex flex-col h-screen border-r border-sidebar-border bg-sidebar shrink-0 transition-[width] duration-200 ease-in-out",
        sidebarCollapsed ? "w-[60px]" : "w-[220px]"
      )}
    >
      {/* Logo */}
      <div className={cn("flex items-center h-14 px-4 border-b border-sidebar-border shrink-0", sidebarCollapsed ? "justify-center" : "gap-2.5")}>
        <div className="flex items-center justify-center w-7 h-7 rounded-md bg-primary/15 shrink-0">
          <MeshMark className="w-4 h-4 text-primary" />
        </div>
        {!sidebarCollapsed && (
          <span className="font-semibold tracking-tight text-sidebar-foreground text-sm">meshploy</span>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex flex-col p-2 flex-1 gap-4">
        {NAV_GROUPS.filter((g) => !g.adminOnly || isAdmin).map((group, gi) => (
          <div key={gi} className="flex flex-col gap-0.5">
            {/* Group label — only in expanded mode */}
            {group.label && !sidebarCollapsed && (
              <p className="px-3 pb-1 text-[10px] font-medium uppercase tracking-wider text-sidebar-foreground/30">
                {group.label}
              </p>
            )}
            {/* Separator between groups in collapsed mode */}
            {group.label && sidebarCollapsed && (
              <Separator className="mb-1 bg-sidebar-border/60" />
            )}
            {group.items.map((item) => {
              const isActive = item.exact ? pathname === item.href : pathname.startsWith(item.href)

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
                              ? "bg-sidebar-accent text-sidebar-foreground"
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
                    "relative flex items-center gap-2.5 h-9 px-3 rounded-md text-sm transition-colors",
                    isActive
                      ? "bg-sidebar-accent text-sidebar-foreground before:absolute before:-left-2 before:top-2 before:bottom-2 before:w-0.5 before:bg-primary before:rounded-r-sm"
                      : "text-sidebar-foreground/50 hover:text-sidebar-foreground hover:bg-sidebar-accent"
                  )}
                >
                  <item.icon className="h-4 w-4 shrink-0" />
                  {item.label}
                </Link>
              )
            })}
          </div>
        ))}
      </nav>

      {/* Bottom: version + collapse toggle */}
      <div className="p-2 shrink-0 space-y-1">
        <Separator className="mb-2 bg-sidebar-border" />

        {ver?.update_available && (
          sidebarCollapsed ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <a
                    href={ver.release_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center justify-center h-8 w-9 mx-auto rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors relative"
                  />
                }
              >
                <Download className="h-3.5 w-3.5" />
                <span className="absolute top-1 right-1 h-1.5 w-1.5 rounded-full bg-emerald-400" />
              </TooltipTrigger>
              <TooltipContent side="right" className="text-xs">
                Update available — v{ver.latest}
              </TooltipContent>
            </Tooltip>
          ) : (
            <a
              href={ver.release_url}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2 h-8 w-full px-3 rounded-md text-xs text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
            >
              <Download className="h-3.5 w-3.5 shrink-0" />
              <span>Update available</span>
              <span className="ml-auto h-1.5 w-1.5 rounded-full bg-emerald-400" />
            </a>
          )
        )}

        {!sidebarCollapsed && ver && (
          <p className="px-3 text-[10px] text-sidebar-foreground/30">
            v{ver.current}
          </p>
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
