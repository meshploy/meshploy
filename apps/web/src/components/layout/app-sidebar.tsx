import { useTabStore } from "@/store/tab-store"
import { useState, useEffect } from "react"
import { cn } from "@/lib/utils"
import { useRouterState, Link } from "@tanstack/react-router"
import {
  Bot,
  ChevronLeft,
  ChevronRight,
  Download,
  FolderKanban,
  Globe,
  Home,
  LayoutTemplate,
  Loader2,
  Network,
  Plug,
  Server,
  Settings,
  Users,
  Radar,
  Import,
} from "lucide-react"
import { useUIStore } from "@/store/ui-store"
import { useIsAdmin } from "@/store/org-store"
import { Separator } from "@/components/ui/separator"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useQuery } from "@tanstack/react-query"
import { system } from "@/lib/api/system"
import { entitlements as entitlementsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { eeNavItems } from "@/ee"
import { UpgradeDialog, upgradeInProgress } from "@/components/system/upgrade-dialog"

type NavItem = {
  href: string
  icon: React.ElementType
  label: string
  exact?: boolean
  // A page a member cannot open. Showing a link that bounces them back is
  // worse than not showing it.
  adminOnly?: boolean
}

type NavGroup = {
  label?: string
  adminOnly?: boolean
  items: NavItem[]
}

// Two different questions, so two groups: what am I running, and what is it
// running on. The first needs no label - it is where everything starts - and
// the second says what Nodes, Cluster and Discovery have in common, which is
// not obvious from their names alone.
const NAV_GROUPS: NavGroup[] = [
  {
    items: [
      { href: "/", icon: Home, label: "Overview", exact: true },
      { href: "/projects", icon: FolderKanban, label: "Projects", exact: false },
      { href: "/templates", icon: LayoutTemplate, label: "Templates", exact: false },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { href: "/nodes", icon: Server, label: "Nodes", exact: false },
      { href: "/cluster", icon: Network, label: "Cluster", exact: false, adminOnly: true },
      { href: "/discovery", icon: Radar, label: "Discovery", exact: false },
      { href: "/domains", icon: Globe, label: "Domains", exact: false },
    ],
  },
  {
    label: "System",
    adminOnly: true,
    items: [
      { href: "/integrations", icon: Plug, label: "Integrations", exact: false },
      { href: "/users", icon: Users, label: "Users", exact: false },
      { href: "/agents", icon: Bot, label: "Agents", exact: false },
      { href: "/settings", icon: Settings, label: "Settings", exact: false },
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
  const { sidebarCollapsed: preferredCollapsed, toggleSidebar, mobileNavOpen, setMobileNavOpen } = useUIStore()
  const projectContext = /^\/projects\/[^/]+/.test(pathname) && !pathname.startsWith("/projects/new")
  const [expandProjectRail, setExpandProjectRail] = useState(false)
  const sidebarCollapsed = (projectContext ? !expandProjectRail : preferredCollapsed) && !mobileNavOpen
  useEffect(() => { setMobileNavOpen(false) }, [pathname, setMobileNavOpen])
  const token = useAuthStore((s) => s.token)
  const isAdmin = useIsAdmin()
  const [upgradeOpen, setUpgradeOpen] = useState(false)
  const { data: ver } = useQuery({
    queryKey: ["system-version"],
    queryFn: () => system.versionInfo(token!),
    enabled: !!token,
    // The API caches the check for two minutes, so asking it this often never
    // reaches GitHub; it is what lets a new build appear without a reload.
    staleTime: 60 * 1000,
    refetchInterval: 2 * 60 * 1000,
    retry: false,
  })
  // Any member may read this, so everyone sees an upgrade under way; it polls
  // quickly only while one runs, and the upgrade dialog shares it.
  const { data: upgradeStatus } = useQuery({
    queryKey: ["system-upgrade"],
    queryFn: () => system.upgradeStatus(token!),
    enabled: !!token,
    retry: false,
    refetchInterval: (q) => (upgradeInProgress(q.state.data) ? 5000 : 2 * 60 * 1000),
  })
  const upgrading = upgradeInProgress(upgradeStatus)

  // Entitlements are only needed to decide which EE nav items to show, so this
  // costs a stock Community build nothing: eeNavItems is empty there and the
  // query never runs.
  const { data: ent } = useQuery({
    queryKey: ["entitlements"],
    queryFn: () => entitlementsApi.get(token!),
    enabled: !!token && eeNavItems.length > 0,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  // EE navigation is supplied by the build overlay. In the open-source build
  // this is empty, so the group below renders nothing and the bundle contains
  // no EE labels or routes.
  //
  // An item with no `feature` shows on any active licence; one that names a
  // feature shows only when the licence includes it, so a partial licence
  // reveals only what it covers.
  const eeGroup: NavGroup | null =
    eeNavItems.length > 0 && ent?.licensed && !ent.expired
      ? {
          items: eeNavItems.filter(
            (i) => !i.feature || ent.features.includes(i.feature)
          ),
        }
      : null

  // Migration is a page a server has for a few days of its life, so the link
  // appears only while there is a migration to see: one was planned, and it has
  // not been finished. Owner-only, like the page.
  const { data: migration } = useQuery({
    queryKey: ["migration"],
    queryFn: () => system.migration(token!),
    enabled: !!token && isAdmin,
    staleTime: 60 * 1000,
    refetchInterval: query => query.state.data?.plan && !query.state.data.status?.finished ? 5_000 : false,
    retry: false,
    throwOnError: false,
  })
  const migrating = isAdmin && !!migration?.plan && !migration.status?.finished
  const migrationStatus = migration?.status
  const migrationGroups = migrationStatus?.groups ?? []
  const movedGroups = migrationGroups.filter(group => group.moved).length
  // A group that cannot move is left where it is and does not hold the cutover
  // up (the migration page's own rule), so it is not work still to do. Counting
  // it would leave the bar short of "ready to finish" for a step that never
  // comes.
  const movableGroups = migrationGroups.filter(group => group.moved || group.can_move).length
  const waitingGroups = movableGroups - movedGroups
  // Completed steps out of all of them - prepare, each movable group, cutover,
  // finish - and not a time estimate. Finishing hides this card, so while it
  // shows the bar stops one step short of full.
  const migrationTotal = movableGroups + 3
  const migrationDone = Number(!!migrationStatus?.prepared) + movedGroups + Number(!!migrationStatus?.cut_over)
  // The same four stages the migration page shows, each filled 0-1. With no
  // movable group the groups stage has nothing to do, so it fills with prepare.
  const migrationStages = [
    { label: "prepare", fill: migrationStatus?.prepared ? 1 : 0 },
    { label: "groups", fill: movableGroups > 0 ? movedGroups / movableGroups : migrationStatus?.prepared ? 1 : 0 },
    { label: "cutover", fill: migrationStatus?.cut_over ? 1 : 0 },
    { label: "finish", fill: migrationStatus?.finished ? 1 : 0 },
  ]
  const migrationBusy = Object.values(migration?.requests ?? {}).some(r => r.state === "queued" || r.state === "running")
  // What happens next, the way the migration page would say it - not a tally
  // of what already did.
  const migrationBlocked = !!migration && !migration.agent_reporting
  const migrationLabel = migrationBlocked
    ? "Host agent not reporting"
    : migrationBusy
      ? "Working…"
      : !migrationStatus?.prepared
        ? "Ready to prepare"
        : waitingGroups > 0
          ? `Moving groups · ${movedGroups}/${movableGroups}`
          : !migrationStatus?.cut_over
            ? "Ready to cut over"
            : "Ready to finish"
  const navGroups = eeGroup && eeGroup.items.length > 0 ? [...NAV_GROUPS, eeGroup] : NAV_GROUPS

  return (
    <aside aria-label="Main navigation"
      className={cn(
        "global-sidebar flex flex-col h-dvh border-r border-sidebar-border bg-sidebar shrink-0 transition-[width] duration-200 ease-in-out",
        sidebarCollapsed ? "w-[68px]" : "w-[236px]",
        mobileNavOpen && "mobile-open"
      )}
    >
      {/* Logo */}
      <div className={cn("sidebar-brand flex items-center h-(--console-topbar-height) px-4 border-b border-sidebar-border shrink-0", sidebarCollapsed ? "justify-center" : "gap-2.5")}>
        <div className="flex items-center justify-center w-7 h-7 rounded-md bg-primary/15 shrink-0">
          <MeshMark className="w-4 h-4 text-primary" />
        </div>
        {!sidebarCollapsed && (
          <span className="font-semibold tracking-tight text-sidebar-foreground text-sm">meshploy</span>
        )}
      </div>

      {/* Navigation */}
      <nav onClick={e => { if (!e.metaKey && !e.ctrlKey && (e.target as Element).closest("a")) useTabStore.getState().setActiveTab(null) }} className="flex flex-col p-2 flex-1 min-h-0 overflow-y-auto gap-4">
        {navGroups.filter((g) => !g.adminOnly || isAdmin).map((group, gi) => (
          <div key={gi} className="flex flex-col gap-0.5">
            {/* Group label — only in expanded mode */}
            {group.label && !sidebarCollapsed && (
              <p className="px-3 pb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                {group.label}
              </p>
            )}
            {/* Separator between groups in collapsed mode */}
            {group.label && sidebarCollapsed && (
              <Separator className="mb-1 bg-sidebar-border/60" />
            )}
            {group.items.filter((item) => !item.adminOnly || isAdmin).map((item) => {
              const isActive = item.exact ? pathname === item.href : pathname.startsWith(item.href)

              if (sidebarCollapsed) {
                return (
                  <Tooltip key={item.href}>
                    <TooltipTrigger
                      render={
                        <Link
                          to={item.href}
                          aria-label={item.label}
                          className={cn(
                            "flex items-center justify-center h-10 w-10 rounded-lg mx-auto transition-colors",
                            isActive
                              ? "active-navigation"
                              : "text-muted-foreground hover:text-sidebar-foreground hover:bg-sidebar-accent"
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
                          aria-label={item.label}
                  className={cn(
                    "relative flex items-center gap-2.5 h-10 px-3 rounded-md text-sm transition-colors",
                    isActive
                      ? "active-navigation before:absolute before:-left-2 before:top-2 before:bottom-2 before:w-0.5 before:bg-primary before:rounded-r-sm"
                      : "text-muted-foreground hover:text-sidebar-foreground hover:bg-sidebar-accent"
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
        {migrating && (
          <Tooltip>
            <TooltipTrigger render={
              <Link to="/migration" aria-label={`Migration: ${migrationLabel}`}
                aria-current={pathname.startsWith("/migration") ? "page" : undefined}
                onClick={event => { if (!event.metaKey && !event.ctrlKey) useTabStore.getState().setActiveTab(null) }}
                className={cn("block rounded-md border border-sidebar-border mb-3 transition-colors hover:bg-sidebar-accent", sidebarCollapsed ? "p-2" : "p-3", pathname.startsWith("/migration") && "active-navigation")}
              />
            }>
              <span className={cn("flex items-center text-sm gap-2", sidebarCollapsed && "justify-center")}>
                <Import className="h-4 w-4 shrink-0 text-primary" />
                {!sidebarCollapsed && <><span>Migration</span><ChevronRight className="ml-auto h-3.5 w-3.5 text-muted-foreground" /></>}
              </span>
              {!sidebarCollapsed && (
                <span className={cn("block text-[11px] mt-2", migrationBlocked ? "text-amber-400" : "text-muted-foreground")}>
                  {migrationLabel}
                </span>
              )}
              {/* One segment per stage, so a single step - cut over, finish -
                  reads as its own stage rather than a notch on one long bar.
                  The groups share one segment that fills as they move, which
                  stays legible at twenty groups. The stage in hand pulses while
                  its step runs, since cutover and finish are the long ones and
                  would otherwise sit still until done. */}
              <span role="progressbar" aria-label="Migration steps completed" aria-valuemin={0} aria-valuemax={migrationTotal} aria-valuenow={migrationDone} aria-valuetext={migrationLabel}
                className="flex gap-0.5 mt-2">
                {migrationStages.map((stage, i) => {
                  const current = i === migrationStages.findIndex(st => st.fill < 1)
                  return (
                    <span key={stage.label} data-stage={stage.label} data-current={current || undefined}
                      className={cn("block h-1 flex-1 rounded-full bg-sidebar-border overflow-hidden",
                        current && migrationBusy && "animate-pulse motion-reduce:animate-none")}>
                      <span className="block h-full bg-primary rounded-full transition-[width] duration-300 motion-reduce:transition-none"
                        style={{ width: `${Math.round(stage.fill * 100)}%` }} />
                    </span>
                  )
                })}
              </span>
            </TooltipTrigger>
            {sidebarCollapsed && <TooltipContent side="right">Migration · {migrationLabel}</TooltipContent>}
          </Tooltip>
        )}
        <Separator className="mb-2 bg-sidebar-border" />

        {(upgrading || ver?.update_available) && (
          sidebarCollapsed ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={() => setUpgradeOpen(true)}
                    className="flex items-center justify-center h-8 w-9 mx-auto rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors relative"
                  />
                }
              >
                {upgrading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
                {!upgrading && <span className="absolute top-1 right-1 h-1.5 w-1.5 rounded-full bg-emerald-400" />}
              </TooltipTrigger>
              <TooltipContent side="right" className="text-xs">
                {upgrading
                  ? "Upgrade in progress"
                  : ver?.channel === "edge"
                    ? `Edge update available: ${ver.latest}`
                    : `Update available: v${ver?.latest}`}
              </TooltipContent>
            </Tooltip>
          ) : (
            <button
              type="button"
              onClick={() => setUpgradeOpen(true)}
              className="flex items-center gap-2 h-8 w-full px-3 rounded-md text-xs text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
            >
              {upgrading ? <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" /> : <Download className="h-3.5 w-3.5 shrink-0" />}
              {/* Named distinctly: an edge update is unreleased code from main,
                  not a version someone cut and reviewed. Taking it is a different
                  decision, so it should not read the same. */}
              <span>{upgrading ? "Upgrading…" : ver?.channel === "edge" ? "Edge update" : "Update available"}</span>
              {!upgrading && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-emerald-400" />}
            </button>
          )
        )}

        {!sidebarCollapsed && ver && (() => {
          const label = (
            <>
              v{ver.current.replace(/^v/, "")}
              {/* An edge build is not the release it names: it was cut after it,
                  from main. Saying so is what distinguishes it from a stable build
                  of the same version, and explains why no update is offered. */}
              {ver.channel === "edge" && (
                <span className="ml-1.5 text-muted-foreground">· edge</span>
              )}
            </>
          )
          // Settings is in the admins' part of the sidebar, so only they get
          // the way to the channels from here.
          return isAdmin ? (
            <Link
              to="/settings"
              hash="server"
              title="Version and release channel"
              className="block px-3 text-[10px] text-muted-foreground hover:text-sidebar-foreground/70 transition-colors"
            >
              {label}
            </Link>
          ) : (
            <p className="px-3 text-[10px] text-muted-foreground">{label}</p>
          )
        })()}
        <Tooltip>
          <TooltipTrigger
            aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
            onClick={() => projectContext ? setExpandProjectRail(!expandProjectRail) : toggleSidebar()}
            className={cn(
              "desktop-collapse flex items-center h-8 w-full rounded-md text-xs text-muted-foreground hover:text-sidebar-foreground/70 hover:bg-sidebar-accent transition-colors",
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

      {ver && <UpgradeDialog open={upgradeOpen} onOpenChange={setUpgradeOpen} version={ver} />}
    </aside>
  )
}
