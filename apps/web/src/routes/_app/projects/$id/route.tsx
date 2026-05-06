import { createFileRoute, Link, Outlet, useParams, useRouterState } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ServerCrash } from "lucide-react"
import { cn } from "@/lib/utils"
import { projects as projectsApi, services as servicesApi, toProject } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id")({
  component: ProjectLayout,
})

function ProjectLayout() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  // Wizard is full-screen — bypass layout entirely
  const isWizard = pathname.endsWith("/new")
  if (isWizard) return <Outlet />

  const { data: rawProject, isLoading, isError } = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const project = rawProject ? toProject(rawProject) : undefined

  // Detect when we're inside a service detail so we can highlight the correct
  // project-level tab. A database service lives at /services/$serviceId but
  // should activate the "Databases" tab, not "Services".
  const serviceDetailMatch = pathname.match(/\/services\/([^/?#]+)/)
  const detailServiceId = serviceDetailMatch?.[1]
  const { data: detailService } = useQuery({
    queryKey: ["service", orgId, projectId, detailServiceId],
    queryFn: () => servicesApi.get(orgId!, projectId, detailServiceId!, token),
    enabled: !!orgId && !!detailServiceId,
  })
  const inDatabaseDetail = detailService?.type === "database"

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading project…</span>
      </div>
    )
  }

  if (isError || !project) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Project not found</p>
      </div>
    )
  }

  const tabs = [
    { label: "Services",  count: project.servicesCount,  to: "/projects/$id/services"  as const },
    { label: "Databases", count: project.databasesCount, to: "/projects/$id/databases" as const },
    { label: "Stacks",    count: project.stacksCount,    to: "/projects/$id/stacks"    as const },
    { label: "Routes",    count: project.routesCount,    to: "/projects/$id/routes"    as const },
    { label: "Secrets",   count: project.secretsCount,   to: "/projects/$id/secrets"   as const },
    { label: "Jobs",      count: project.jobsCount,      to: "/projects/$id/jobs"      as const },
    { label: "Settings",  count: null,                   to: "/projects/$id/settings"  as const },
  ]

  return (
    <div className="flex flex-col min-h-full">
      {/* Header */}
      <div className="border-b border-border/60 bg-background">
        <div className="px-6 pt-5 pb-0">
          {/* Project name + slug */}
          <div className="flex items-center gap-2.5 mb-4">
            <h1 className="text-lg font-semibold tracking-tight">{project.name}</h1>
            <code className="text-xs font-mono text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">
              {project.slug}
            </code>
          </div>

          {/* Tab strip with counts */}
          {/* activeSegment: immediate child of /projects/:id/ in the current URL */}
          {(() => {
            const activeSegment = pathname.split(`/projects/${projectId}/`)[1]?.split("/")[0] ?? ""
            return (
          <nav className="flex items-center gap-0 -mb-px">
            {tabs.map(({ label, count, to }) => {
              const seg = to.split("/").at(-1)!
              const isActive =
                label === "Databases" && inDatabaseDetail ? true :
                label === "Services"  && inDatabaseDetail ? false :
                activeSegment === seg
              return (
              <Link
                key={label}
                to={to}
                params={{ id: projectId }}
                className={cn(
                  "px-4 py-2.5 text-sm border-b-2 transition-colors whitespace-nowrap",
                  isActive
                    ? "text-foreground border-foreground/25"
                    : "text-muted-foreground border-transparent hover:text-foreground hover:border-border/60"
                )}
              >
                {label}
                {count != null && (
                  <span className="ml-1.5 text-[11px] text-muted-foreground/60 tabular-nums">· {count}</span>
                )}
              </Link>
              )
            })}
          </nav>
            )
          })()}
        </div>
      </div>

      {/* Tab content */}
      <div className="flex-1">
        <Outlet />
      </div>
    </div>
  )
}
