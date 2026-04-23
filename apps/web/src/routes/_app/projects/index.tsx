import { createFileRoute, Link } from "@tanstack/react-router"
import { useState } from "react"
import type React from "react"
import { useQuery } from "@tanstack/react-query"
import { FolderKanban, Globe, Loader2, Plus, Server, ServerCrash } from "lucide-react"
import { projects as projectsApi, toProject } from "@/lib/api"
import type { Project } from "@/types"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import { NewProjectModal } from "@/components/projects/new-project-modal"

export const Route = createFileRoute("/_app/projects/")({
  component: ProjectsPage,
})

function ProjectsPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const [modalOpen, setModalOpen] = useState(false)

  const { data, isLoading, isError, error } = useQuery({
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
        <p className="text-xs text-muted-foreground/60">{(error as Error).message}</p>
      </div>
    )
  }

  const projectList = data ?? []

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Projects</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {projectList.length} project{projectList.length !== 1 ? "s" : ""} · each maps to a K3s namespace
          </p>
        </div>
        <button
          onClick={() => setModalOpen(true)}
          className="flex items-center gap-1.5 text-sm bg-primary text-primary-foreground px-3 py-1.5 rounded-md hover:bg-primary/90 transition-colors"
        >
          <Plus className="h-3.5 w-3.5" />
          New Project
        </button>
      </div>

      {projectList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-16 flex flex-col items-center gap-3">
          <FolderKanban className="h-8 w-8 text-muted-foreground/30" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No projects yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Create a project to start deploying services</p>
          </div>
          <button
            onClick={() => setModalOpen(true)}
            className="flex items-center gap-1.5 text-sm bg-primary text-primary-foreground px-3 py-1.5 rounded-md hover:bg-primary/90 transition-colors mt-1"
          >
            <Plus className="h-3.5 w-3.5" />
            New Project
          </button>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {projectList.map((project) => (
            <ProjectCard key={project.id} project={project} />
          ))}
        </div>
      )}

      <NewProjectModal open={modalOpen} onOpenChange={setModalOpen} />
    </div>
  )
}

function ProjectCard({ project }: { project: Project }) {
  const counts = [
    { icon: <Server className="h-3 w-3" />, label: "service", n: project.servicesCount },
    { icon: <Globe className="h-3 w-3" />, label: "route", n: project.routesCount },
    ...(project.databasesCount > 0
      ? [{ icon: null as React.ReactNode, label: "db", n: project.databasesCount }]
      : []),
  ]

  return (
    <Link
      to="/projects/$id"
      params={{ id: project.id }}
      className="group flex flex-col gap-4 rounded-lg border border-border/60 bg-card p-5 hover:border-border hover:bg-card/80 transition-all"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-8 h-8 rounded-md bg-primary/10 shrink-0">
            <FolderKanban className="h-4 w-4 text-primary" />
          </div>
          <div>
            <p className="text-sm font-medium text-foreground leading-tight">{project.name}</p>
            <code className="text-[10px] font-mono text-muted-foreground">ns/{project.slug}</code>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-4 pt-1 border-t border-border/40">
        {counts.map(({ icon, label, n }) => (
          <div key={label} className="flex items-center gap-1.5 text-xs text-muted-foreground">
            {icon}
            <span>{n} {label}{n !== 1 ? "s" : ""}</span>
          </div>
        ))}
        <span className="ml-auto text-[11px] text-muted-foreground/40">
          {formatRelativeTime(project.createdAt)}
        </span>
      </div>
    </Link>
  )
}
