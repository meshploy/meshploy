import { useRouterState, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronRight, Home } from "lucide-react"
import { UserMenu } from "./user-menu"
import { projects as projectsApi, services as servicesApi, nodes as nodesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

const SEGMENT_LABELS: Record<string, string> = {
  projects:     "Projects",
  nodes:        "Nodes",
  cluster:      "Cluster",
  integrations: "Integrations",
  settings:     "Settings",
  services:     "Services",
  deployments:  "Deployments",
  routes:       "Routes",
  jobs:         "Jobs",
  "cron-jobs":  "Cron Jobs",
  databases:    "Databases",
  pipelines:    "Pipelines",
  domains:      "Domains",
  config:       "Config",
  logs:         "Logs",
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

type ResourceType = "project" | "service" | "deployment" | "node" | "static" | "uuid"

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

  if (entry.type === "static") return <>{SEGMENT_LABELS[entry.segment] ?? entry.segment}</>
  if (entry.type === "deployment") return <>{entry.segment.slice(0, 8)}</>
  if (entry.type === "project") return <>{projectQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "service") return <>{serviceQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  if (entry.type === "node") return <>{nodeQuery.data?.name ?? entry.segment.slice(0, 8)}</>
  return <>{entry.segment.slice(0, 8)}</>
}

function Breadcrumb() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const segments = pathname.split("/").filter(Boolean)
  const entries = parsePath(segments)

  return (
    <nav className="flex items-center gap-1 text-sm">
      <Link to="/" className="text-muted-foreground hover:text-foreground transition-colors">
        <Home className="h-3.5 w-3.5" />
      </Link>
      {entries.map((entry, i) => {
        const isLast = i === entries.length - 1
        return (
          <span key={entry.href} className="flex items-center gap-1">
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40" />
            {isLast ? (
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
  return (
    <header className="flex items-center h-14 px-6 border-b border-border/40 bg-background/80 backdrop-blur-sm shrink-0 sticky top-0 z-10">
      <div className="flex-1">
        <Breadcrumb />
      </div>
      <div className="flex items-center gap-3">
        <UserMenu />
      </div>
    </header>
  )
}
