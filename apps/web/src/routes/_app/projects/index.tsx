import { Input } from "@/components/ui/input"
import { OptionSelect } from "@/components/layout/option-select"
import { useState } from "react"
import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { FolderKanban, Globe, Loader2, Plus, Server, ServerCrash, Search, LayoutGrid, List, ArrowUpRight } from "lucide-react"
import { projects as projectsApi, toProject } from "@/lib/api"
import type { Project } from "@/types"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin } from "@/store/org-store"
import { formatRelativeTime, projectColorHue } from "@/lib/utils"
import { Button } from "@/components/ui/button"

export const Route = createFileRoute("/_app/projects/")({
  component: ProjectsPage,
})

function ProjectsPage() {
  const [search, setSearch] = useState("")
  const [view, setView] = useState<"grid" | "list">("grid")
  const [sort, setSort] = useState("recent")
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const isAdmin = useIsAdmin()

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId!, token),
    enabled: !!orgId,
    select: (raw) => raw.map(toProject),
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading projects…</span>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Failed to load projects</p>
        <p className="text-xs text-muted-foreground/60">{(error as Error).message}</p><Button variant="outline" onClick={() => refetch()}>Try again</Button>
      </div>
    )
  }

  const projectList = data ?? []
  const visible = projectList.filter(p => `${p.name} ${p.slug}`.toLowerCase().includes(search.toLowerCase())).sort((a,b) => sort === "name" ? a.name.localeCompare(b.name) : b.createdAt.getTime() - a.createdAt.getTime())

  return (
    <div className="console-page p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Projects</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            A home for your services, data, and deployments.
          </p>
        </div>
        {isAdmin && (
          <Button size="sm" render={<Link to="/projects/new" />}>
            <Plus className="h-3.5 w-3.5 mr-1" />
            New project
          </Button>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-48 max-w-md"><Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" /><Input aria-label="Search projects" placeholder="Search projects…" value={search} onChange={e => setSearch(e.target.value)} className="h-10 w-full pl-10 pr-3" /></div>
        <OptionSelect label="Sort projects" value={sort} onChange={setSort} options={[{"value": "recent", "label": "Newest first"}, {"value": "name", "label": "Name A\u2013Z"}]} />
        <span className="ml-auto text-sm text-muted-foreground">{visible.length} projects</span>
        <div className="flex gap-1 rounded-lg border border-border p-1"><Button size="icon" variant={view === "grid" ? "secondary" : "ghost"} aria-label="Card view" aria-pressed={view === "grid"} onClick={() => setView("grid")}><LayoutGrid className="size-4" /></Button><Button size="icon" variant={view === "list" ? "secondary" : "ghost"} aria-label="List view" aria-pressed={view === "list"} onClick={() => setView("list")}><List className="size-4" /></Button></div>
      </div>
      {projectList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-16 flex flex-col items-center gap-3">
          <FolderKanban className="h-8 w-8 text-muted-foreground/30" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No projects yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              {isAdmin ? "Create a project to start deploying services" : "No projects have been shared with you yet"}
            </p>
          </div>
          {isAdmin && (
            <Button size="sm" render={<Link to="/projects/new" />} className="mt-1">
              <Plus className="h-3.5 w-3.5 mr-1" />
              New project
            </Button>
          )}
        </div>
      ) : (
        <div className={view === "grid" ? "grid gap-4 md:grid-cols-2 xl:grid-cols-3" : "grid gap-3"}>
          {visible.length === 0 && <div className="col-span-full rounded-xl border border-dashed border-border p-12 text-center"><p className="font-medium">No matching projects</p><p className="mt-2 text-sm text-muted-foreground">Try another name or clear your search.</p><Button className="mt-4" variant="outline" onClick={() => setSearch("")}>Clear search</Button></div>}
          {visible.map((project) => (
            <ProjectCard key={project.id} project={project} list={view === "list"} />
          ))}
        </div>
      )}

    </div>
  )
}

function ProjectCard({ project, list }: { project: Project; list: boolean }) {
  const hue = projectColorHue(project.id)

  return (
    <Link
      to="/projects/$id"
      params={{ id: project.id }}
      className={`listing-surface interactive-surface group flex gap-5 rounded-xl border border-border bg-card p-6 hover:border-primary/40 hover:bg-secondary/40 transition-all ${list ? "flex-col md:flex-row md:items-center md:justify-between" : "flex-col"}`}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div
            className="flex items-center justify-center w-9 h-9 rounded-md shrink-0"
            style={{
              background: `oklch(0.72 0.17 ${hue} / 0.15)`,
              border: `1px solid oklch(0.72 0.17 ${hue} / 0.3)`,
            }}
          >
            <FolderKanban
              className="h-4 w-4"
              style={{ color: `oklch(0.78 0.17 ${hue})` }}
            />
          </div>
          <div>
            <p className="text-sm font-semibold text-foreground leading-tight">{project.name}</p>
            <span className="text-xs text-muted-foreground">{project.slug}</span>
          </div>
        </div>
      </div>

      <div className={`flex flex-wrap items-center gap-4 ${list ? "" : "pt-5 border-t border-border/60"}`}>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Server className="h-3 w-3" />
          <span>{project.servicesCount} service{project.servicesCount !== 1 ? "s" : ""}</span>
        </div>
        {project.databasesCount > 0 && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span>{project.databasesCount} db{project.databasesCount !== 1 ? "s" : ""}</span>
          </div>
        )}
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Globe className="h-3 w-3" />
          <span>{project.routesCount} route{project.routesCount !== 1 ? "s" : ""}</span>
        </div>
        <span className="ml-auto text-xs text-muted-foreground">
          {formatRelativeTime(project.createdAt)}
        </span><ArrowUpRight className="size-4 text-muted-foreground group-hover:text-primary" />
      </div>
    </Link>
  )
}
