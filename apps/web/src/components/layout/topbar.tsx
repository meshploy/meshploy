import { useTabStore } from "@/store/tab-store"
import { useUIStore } from "@/store/ui-store"
import { useRouterState, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Box, Check, ChevronDown, ChevronRight, Clock, Database, Globe, HardDrive, Home, Layers, Menu, Search, Server } from "lucide-react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useState } from "react"
import { Input } from "@/components/ui/input"
import { StatusPill } from "@/components/layout/resource-workbench"
import { useNavigate } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import { UserMenu } from "./user-menu"
import { projects as projectsApi, services as servicesApi, nodes as nodesApi, volumes as volumesApi, routes as routesApi, jobs as jobsApi, stacks as stacksApi, orgs as orgsApi, variableGroups, configFiles, agents } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

const SEGMENT_LABELS: Record<string, string> = {
  projects:     "Projects",
  nodes:        "Nodes",
  cluster:      "Cluster",
  integrations: "Integrations",
  settings:     "Settings",
  users:        "Users",
  services:     "Services",
  deployments:  "Deployments",
  routes:       "Routes",
  jobs:         "Jobs",
  databases:    "Databases",
  stacks:       "Stacks",
  volumes:      "Volumes",
  secrets:      "Secrets",
  pipelines:    "Pipelines",
  domains:      "Domains",
  config:       "Configuration",
  "config-files": "Config files",
  variables: "Variables",
  agents: "Agents",
  logs:         "Logs",
  account:      "Account",
  new:          "New",
  overview: "Overview",
  pods: "Pods",
  backups: "Backups",
  permissions: "Permissions",
  editor: "Editor",
  runs: "Runs",
  templates: "Templates",
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

type ResourceType = "project" | "service" | "deployment" | "node" | "volume" | "route" | "job" | "stack" | "member" | "variable-group" | "config-file" | "agent" | "static" | "uuid"

interface BreadcrumbEntry {
  segment: string
  href: string
  type: ResourceType
  projectId?: string
  serviceId?: string
}

function parsePath(segments: string[]): BreadcrumbEntry[] {
  const entries: BreadcrumbEntry[] = []
  let projectId: string | undefined
  let serviceId: string | undefined

  for (let i = 0; i < segments.length; i++) {
    const segment = segments[i]
    const href = "/" + segments.slice(0, i + 1).join("/")
    const prev = segments[i - 1]

    if (!UUID_RE.test(segment)) {
      entries.push({ segment, href, type: "static" })
      continue
    }

    if (prev === "projects") {
      projectId = segment
      entries.push({ segment, href, type: "project" })
    } else if (prev === "services") {
      serviceId = segment
      entries.push({ segment, href, type: "service", projectId })
    } else if (prev === "deployments") {
      entries.push({ segment, href, type: "deployment", projectId, serviceId })
    } else if (prev === "nodes") {
      entries.push({ segment, href, type: "node" })
    } else if (prev === "volumes") {
      entries.push({ segment, href, type: "volume", projectId })
    } else if (prev === "routes") {
      entries.push({ segment, href, type: "route", projectId })
    } else if (prev === "jobs") {
      entries.push({ segment, href, type: "job", projectId })
    } else if (prev === "stacks") {
      entries.push({ segment, href, type: "stack", projectId })
    } else if (prev === "variables") {
      entries.push({ segment, href, type: "variable-group", projectId })
    } else if (prev === "config-files") {
      entries.push({ segment, href, type: "config-file", projectId })
    } else if (prev === "agents") {
      entries.push({ segment, href, type: "agent" })
    } else if (prev === "users") {
      entries.push({ segment, href, type: "member" })
    } else {
      entries.push({ segment, href, type: "uuid" })
    }
  }

  return entries
}

// Resolves one breadcrumb entry to a display label.
// Each entry is its own component so hooks run unconditionally.
function BreadcrumbLabel({ entry }: { entry: BreadcrumbEntry }) {
  const token = useAuthStore((s) => s.token)
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const projectQuery = useQuery({
    queryKey: ["project", orgId, entry.segment],
    queryFn: () => projectsApi.get(orgId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "project",
    staleTime: 5 * 60 * 1000,
  })

  const serviceQuery = useQuery({
    queryKey: ["service", orgId, entry.projectId, entry.segment],
    queryFn: () => servicesApi.get(orgId!, entry.projectId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "service" && !!entry.projectId,
    staleTime: 5 * 60 * 1000,
  })

  const nodeQuery = useQuery({
    queryKey: ["node", orgId, entry.segment],
    queryFn: () => nodesApi.get(orgId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "node",
    staleTime: 5 * 60 * 1000,
  })

  const volumeQuery = useQuery({
    queryKey: ["volume", orgId, entry.projectId, entry.segment],
    queryFn: () => volumesApi.get(orgId!, entry.projectId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "volume" && !!entry.projectId,
    staleTime: 5 * 60 * 1000,
  })

  const routeQuery = useQuery({
    queryKey: ["route", orgId, entry.projectId, entry.segment],
    queryFn: () => routesApi.get(orgId!, entry.projectId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "route" && !!entry.projectId,
    staleTime: 5 * 60 * 1000,
  })

  const jobQuery = useQuery({
    queryKey: ["job", orgId, entry.projectId, entry.segment],
    queryFn: () => jobsApi.get(orgId!, entry.projectId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "job" && !!entry.projectId,
    staleTime: 5 * 60 * 1000,
  })

  const stackQuery = useQuery({
    queryKey: ["stack", orgId, entry.projectId, entry.segment],
    queryFn: () => stacksApi.get(orgId!, entry.projectId!, entry.segment, token!),
    enabled: !!orgId && !!token && entry.type === "stack" && !!entry.projectId,
    staleTime: 5 * 60 * 1000,
  })

  const memberQuery = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId!, token!),
    enabled: !!orgId && !!token && entry.type === "member",
    staleTime: 5 * 60 * 1000,
    select: (members) => members.find((m) => m.user_id === entry.segment),
  })

  const groupQuery = useQuery({ queryKey: ["variable-group", orgId, entry.projectId, entry.segment], queryFn: () => variableGroups.get(orgId!, entry.projectId!, entry.segment, token!), enabled: !!orgId && !!token && entry.type === "variable-group" })
  const fileQuery = useQuery({ queryKey: ["config-file", orgId, entry.projectId, entry.segment], queryFn: () => configFiles.get(orgId!, entry.projectId!, entry.segment, token!), enabled: !!orgId && !!token && entry.type === "config-file" })
  const agentQuery = useQuery({ queryKey: ["agents", orgId], queryFn: () => agents.list(orgId!, token!), enabled: !!orgId && !!token && entry.type === "agent", select: list => list.find(a => a.id === entry.segment) })
  if (entry.type === "variable-group") return <>{groupQuery.data?.name ?? "Variable group"}</>
  if (entry.type === "config-file") return <>{fileQuery.data?.name ?? "Config file"}</>
  if (entry.type === "agent") return <>{agentQuery.data?.name ?? "Agent"}</>
  if (entry.type === "static") return <>{SEGMENT_LABELS[entry.segment] ?? entry.segment}</>
  if (entry.type === "deployment") return <>{entry.segment.slice(0, 8)}</>
  if (entry.type === "project") return <>{projectQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "service") return <>{serviceQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "node") return <>{nodeQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "volume") return <>{volumeQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "route") return <>{routeQuery.data?.hostname ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "job") return <>{jobQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "stack") return <>{stackQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "member") return <>{memberQuery.data?.user_name ?? entry.segment.slice(0, 8)}</>
  return <>{entry.segment.slice(0, 8)}</>
}

// ── Switching between siblings ────────────────────────────────────────────────

// A project's sections, so switching projects can keep the one you are on.
const PROJECT_SECTIONS = ["services", "databases", "stacks", "jobs", "volumes", "routes", "variables", "config-files", "settings"]

const TYPE_ICONS: Partial<Record<ResourceType, typeof Box>> = {
  project: Layers, service: Box, stack: Layers, job: Clock, volume: HardDrive, route: Globe, node: Server,
}

// Which entry types open a switcher, and what the list is called.
const SIBLING_LABELS: Partial<Record<ResourceType, string>> = {
  project: "Projects",
  service: "Resources",
  stack:   "Resources",
  job:     "Resources",
  volume:  "Resources",
  route:   "Resources",
  node:    "Nodes",
}

// A resource segment lists everything in the project, not only its own kind:
// moving from a service to a stack is the same act as moving between services.
const PROJECT_RESOURCE_TYPES: ResourceType[] = ["service", "stack", "job", "volume", "route"]

// state is what the row says under the name: the resource's own status, or for
// a domain route -- which has none -- the zone it is served in.
type Resource = { id: string; name: string; type: ResourceType; kind: string; state: string }

// The tabs each kind has. Anything deeper than a tab belongs to the resource
// being left, so only the first segment is ever carried across.
const RESOURCE_TABS: Record<string, string[]> = {
  application: ["overview", "deployments", "config", "pods", "logs", "settings", "permissions"],
  database:    ["overview", "deployments", "config", "pods", "backups", "logs", "settings", "permissions"],
  stack:       ["editor", "services", "variables", "permissions"],
  job:         ["config", "runs", "permissions"],
  volume:      [],
  route:       [],
}

const RESOURCE_PATHS: Partial<Record<ResourceType, string>> = {
  service: "services", stack: "stacks", job: "jobs", volume: "volumes", route: "routes",
}

/** Where a resource lives, keeping the tab you are on when it has one. */
function resourceHref(projectId: string, target: Resource, tab: string | undefined) {
  const base = `/projects/${projectId}/${RESOURCE_PATHS[target.type]}/${target.id}`
  return tab && RESOURCE_TABS[target.kind]?.includes(tab) ? `${base}/${tab}` : base
}

type Sibling = { id: string; name: string }

/** Where a sibling of a project or node lives, keeping the section you are on. */
function siblingHref(entry: BreadcrumbEntry, pathname: string, id: string) {
  const base = entry.href.slice(0, entry.href.lastIndexOf("/") + 1) + id
  const [first] = pathname.slice(entry.href.length).split("/").filter(Boolean)
  if (!first) return base
  // Another project has the same sections, but none of the same resources; a
  // node page has no sections at all.
  if (entry.type === "project" && PROJECT_SECTIONS.includes(first)) return `${base}/${first}`
  return base
}

/** Everything in a project, in one list. Loaded when a menu opens, from the
    same queries the pages use, so it is usually already in the cache. */
function useProjectResources(projectId: string | undefined, open: boolean): Resource[] {
  const token = useAuthStore((s) => s.token)
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const on = open && !!orgId && !!token && !!projectId
  const p = projectId ?? ""

  const services = useQuery({ queryKey: ["services", orgId, p], queryFn: () => servicesApi.list(orgId!, p, token!), enabled: on })
  const stacks = useQuery({ queryKey: ["stacks", orgId, p], queryFn: () => stacksApi.list(orgId!, p, token!), enabled: on })
  const jobs = useQuery({ queryKey: ["jobs", orgId, p], queryFn: () => jobsApi.list(orgId!, p, token!), enabled: on })
  const volumes = useQuery({ queryKey: ["volumes", orgId, p], queryFn: () => volumesApi.list(orgId!, p, token!), enabled: on })
  const routeList = useQuery({ queryKey: ["routes", orgId, p], queryFn: () => routesApi.list(orgId!, p, token!), enabled: on })

  return [
    ...(services.data ?? []).map((s): Resource => ({
      id: s.id, name: s.name, type: "service",
      kind: s.type === "database" ? "database" : "application",
      state: s.status,
    })),
    ...(stacks.data ?? []).map((s): Resource => ({ id: s.id, name: s.name, type: "stack", kind: "stack", state: s.status })),
    ...(jobs.data ?? []).map((j): Resource => ({ id: j.id, name: j.name, type: "job", kind: "job", state: j.status })),
    ...(volumes.data ?? []).map((v): Resource => ({ id: v.id, name: v.name, type: "volume", kind: "volume", state: v.status })),
    ...(routeList.data ?? []).map((r): Resource => ({ id: r.id, name: r.hostname, type: "route", kind: "route", state: r.zone })),
  ]
}

/** The projects, or the nodes: the segments whose siblings are their own kind. */
function useSiblings(entry: BreadcrumbEntry, open: boolean): Sibling[] {
  const token = useAuthStore((s) => s.token)
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const on = (type: ResourceType) => open && !!orgId && !!token && entry.type === type

  const projects = useQuery({ queryKey: ["projects", orgId], queryFn: () => projectsApi.list(orgId!, token!), enabled: on("project") })
  const nodeList = useQuery({ queryKey: ["nodes", orgId], queryFn: () => nodesApi.list(orgId!, token!), enabled: on("node") })

  if (entry.type === "project") return (projects.data ?? []).map((p) => ({ id: p.id, name: p.name }))
  if (entry.type === "node") return (nodeList.data ?? []).map((n) => ({ id: n.id, name: n.name }))
  return []
}

/** The row shared by both menus: an icon, what it is called, and what it is. */
function SwitcherItem({ icon: Icon, name, state, current, onSelect }: {
  icon: typeof Box
  name: string
  state?: string
  current: boolean
  onSelect: () => void
}) {
  return (
    <DropdownMenuItem className="gap-2.5 px-2 py-2" onClick={onSelect}>
      <span className="flex items-center justify-center w-7 h-7 rounded-md bg-muted/60 shrink-0">
        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
      </span>
      <span className="flex-1 min-w-0">
        <span className="block truncate text-sm text-foreground">{name}</span>
        {state && (
          <span className="mt-0.5 flex">
            <StatusPill status={state} />
          </span>
        )}
      </span>
      {current && <Check className="h-4 w-4 text-primary shrink-0" />}
    </DropdownMenuItem>
  )
}

/** The find field. Its keystrokes are kept from the menu's own typeahead, which
    would otherwise jump the highlight around as you type. */
function SwitcherSearch({ value, onChange, placeholder, onEnter }: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  onEnter: () => void
}) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 border-b border-border/40">
      <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
      <Input
        autoFocus
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") return
          e.stopPropagation()
          if (e.key === "Enter") onEnter()
        }}
        // The field is the whole row here, so its own border and the console's
        // focus outline would draw a box inside a box. It is focused the moment
        // the menu opens, and the caret says so.
        className="h-7! min-h-0 border-0! bg-transparent! px-0 text-sm shadow-none focus-visible:ring-0! focus-visible:outline-none!"
      />
      <span className="shrink-0 rounded border border-border/60 bg-muted/40 px-1.5 py-0.5 text-[10px] text-muted-foreground">
        Esc
      </span>
    </div>
  )
}

/** The resource segment: everything in this project, with where you are kept. */
function ResourceSwitcher({ entry, isLast, pathname }: { entry: BreadcrumbEntry; isLast: boolean; pathname: string }) {
  const [open, setOpen] = useState(false)
  const [find, setFind] = useState("")
  const navigate = useNavigate()
  const projectId = entry.projectId!
  const resources = useProjectResources(projectId, open)

  // The tab within the resource, which follows you when the target has it.
  const [tab] = pathname.slice(entry.href.length).split("/").filter(Boolean)
  const matches = resources.filter((r) => r.name.toLowerCase().includes(find.toLowerCase()))
  const go = (r: Resource) => {
    setOpen(false)
    navigate({ to: resourceHref(projectId, r, tab) as "/nodes" })
  }

  // Grouped while browsing, flat while searching: groups are how you find a
  // kind, a query is how you find a name.
  const groups = find
    ? [{ label: "", items: matches }]
    : PROJECT_RESOURCE_TYPES.map((type) => ({
        label: SEGMENT_LABELS[RESOURCE_PATHS[type]!] ?? "",
        items: matches.filter((r) => r.type === type),
      })).filter((g) => g.items.length > 0)

  return (
    <DropdownMenu open={open} onOpenChange={(o) => { setOpen(o); if (!o) setFind("") }}>
      <SwitcherTrigger entry={entry} isLast={isLast} />
      <DropdownMenuContent align="start" sideOffset={6} className="w-[300px] p-0">
        <SwitcherSearch
          value={find}
          onChange={setFind}
          placeholder="Find a resource…"
          onEnter={() => matches[0] && go(matches[0])}
        />
        <div className="max-h-[320px] overflow-y-auto p-1">
          {resources.length === 0 && (
            <div className="px-2 py-3 text-xs text-muted-foreground/60">Loading…</div>
          )}
          {resources.length > 0 && matches.length === 0 && (
            <div className="px-2 py-3 text-xs text-muted-foreground/60">Nothing matches “{find}”.</div>
          )}
          {groups.map((group) => (
            <div key={group.label}>
              {group.label && (
                <DropdownMenuLabel className="px-2 pt-2 text-xs text-muted-foreground">
                  {group.label}
                </DropdownMenuLabel>
              )}
              {group.items.map((r) => (
                <SwitcherItem
                  key={r.id}
                  icon={r.kind === "database" ? Database : TYPE_ICONS[r.type] ?? Box}
                  name={r.name}
                  state={r.state}
                  current={r.id === entry.segment}
                  onSelect={() => go(r)}
                />
              ))}
            </div>
          ))}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The project and node segments, which list their own kind. */
function SiblingSwitcher({ entry, isLast, pathname }: { entry: BreadcrumbEntry; isLast: boolean; pathname: string }) {
  const [open, setOpen] = useState(false)
  const [find, setFind] = useState("")
  const navigate = useNavigate()
  const siblings = useSiblings(entry, open)
  const matches = siblings.filter((s) => s.name.toLowerCase().includes(find.toLowerCase()))
  const go = (id: string) => {
    setOpen(false)
    navigate({ to: siblingHref(entry, pathname, id) as "/nodes" })
  }

  return (
    <DropdownMenu open={open} onOpenChange={(o) => { setOpen(o); if (!o) setFind("") }}>
      <SwitcherTrigger entry={entry} isLast={isLast} />
      <DropdownMenuContent align="start" sideOffset={6} className="w-[280px] p-0">
        <SwitcherSearch
          value={find}
          onChange={setFind}
          placeholder={entry.type === "project" ? "Find a project…" : "Find a node…"}
          onEnter={() => matches[0] && go(matches[0].id)}
        />
        <div className="max-h-[320px] overflow-y-auto p-1">
          {siblings.length === 0 && (
            <div className="px-2 py-3 text-xs text-muted-foreground/60">Loading…</div>
          )}
          {matches.map((s) => (
            <SwitcherItem
              key={s.id}
              icon={TYPE_ICONS[entry.type] ?? Box}
              name={s.name}
              current={s.id === entry.segment}
              onSelect={() => go(s.id)}
            />
          ))}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The segment itself: the name you are on, with a chevron to say it opens. */
function SwitcherTrigger({ entry, isLast }: { entry: BreadcrumbEntry; isLast: boolean }) {
  return (
    <DropdownMenuTrigger
      className={cn(
        "flex items-center gap-1 rounded-md px-1.5 py-0.5 -mx-0.5 transition-colors outline-none",
        "hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring/50",
        isLast ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
      )}
    >
      <BreadcrumbLabel entry={entry} />
      <ChevronDown className="h-3 w-3 shrink-0 opacity-50" />
    </DropdownMenuTrigger>
  )
}

function Breadcrumb() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const segments = pathname.split("/").filter(Boolean)
  const entries = parsePath(segments)

  return (
    <nav onClick={e => { if (!e.metaKey && !e.ctrlKey && (e.target as Element).closest("a")) useTabStore.getState().setActiveTab(null) }} aria-label="Breadcrumb" className="flex items-center gap-1 text-xs overflow-x-auto whitespace-nowrap">
      <Link to="/" aria-label="Overview" className="text-muted-foreground hover:text-foreground transition-colors">
        <Home className="h-3.5 w-3.5" />
      </Link>
      {entries.map((entry, i) => {
        const isLast = i === entries.length - 1
        return (
          <span key={entry.href} className="flex items-center gap-1">
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40" />
            {PROJECT_RESOURCE_TYPES.includes(entry.type) && entry.projectId ? (
              <ResourceSwitcher entry={entry} isLast={isLast} pathname={pathname} />
            ) : SIBLING_LABELS[entry.type] ? (
              <SiblingSwitcher entry={entry} isLast={isLast} pathname={pathname} />
            ) : isLast ? (
              <span className="font-medium text-foreground">
                <BreadcrumbLabel entry={entry} />
              </span>
            ) : (
              <Link
                to={entry.href as "/nodes"}
                className="text-muted-foreground hover:text-foreground transition-colors"
              >
                <BreadcrumbLabel entry={entry} />
              </Link>
            )}
          </span>
        )
      })}
    </nav>
  )
}

export function Topbar() {
  const { setMobileNavOpen } = useUIStore()
  return (
    <header className="flex items-center h-(--console-topbar-height) px-4 md:px-6 border-b border-border/40 bg-background/80 backdrop-blur-sm shrink-0 sticky top-0 z-40">
      <button className="mr-3 md:hidden p-2 rounded-lg hover:bg-muted" aria-label="Open navigation" onClick={() => setMobileNavOpen(true)}><Menu className="h-5 w-5" /></button>
      <div className="flex-1 min-w-0">
        <Breadcrumb />
      </div>
      <div className="flex items-center gap-3">
        <UserMenu />
      </div>
    </header>
  )
}
