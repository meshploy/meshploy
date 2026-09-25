import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Box, Database, Globe, Layers, HardDrive, Variable, FileCog, Clock, ArrowUpRight, Plus } from "lucide-react"
import { projects } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { EnvironmentBoard } from "@/components/projects/environment-board"

export const Route = createFileRoute("/_app/projects/$id/")({ component: ProjectOverview })

function ProjectOverview() {
  const { id } = Route.useParams()
  const token = useAuthStore(s => s.token)!
  const orgId = useOrgStore(s => s.currentOrg?.id)
  const { data: project } = useQuery({ queryKey: ["project", orgId, id], queryFn: () => projects.get(orgId!, id, token), enabled: !!orgId })
  const resources = [
    { name: "Services", path: "services", icon: Box, count: project?.services_count, description: "Applications and long-running workloads" },
    { name: "Databases", path: "databases", icon: Database, count: project?.databases_count, description: "Managed data for your applications" },
    { name: "Stacks", path: "stacks", icon: Layers, count: project?.stacks_count, description: "Related resources deployed together" },
    { name: "Routes", path: "routes", icon: Globe, count: project?.routes_count, description: "Domains and access to your services" },
    { name: "Volumes", path: "volumes", icon: HardDrive, count: project?.volumes_count, description: "Persistent storage across deployments" },
    { name: "Variable groups", path: "variables", icon: Variable, count: project?.variables_count, description: "Shared environment and secrets" },
    { name: "Config files", path: "config-files", icon: FileCog, count: project?.config_files_count, description: "Files mounted into your services" },
    { name: "Jobs", path: "jobs", icon: Clock, count: project?.jobs_count, description: "Scheduled tasks and one-off runs" },
  ] as const
  return <div className="console-page space-y-7">
    <div className="flex flex-wrap items-start justify-between gap-4"><div><h1>Project overview</h1><p className="mt-2 text-sm text-muted-foreground">Everything running in {project?.name ?? "this project"}, in one place.</p></div><Button render={<Link to="/projects/$id/new" params={{ id }} search={{ type: "service" }} />}><Plus className="size-4" />New resource</Button></div>
    {orgId && <EnvironmentBoard orgId={orgId} projectId={id} token={token} />}
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">{resources.map(({ name, path, icon: Icon, count, description }) => <Link key={path} to={`/projects/$id/${path}`} params={{ id }} className="overview-resource-card interactive-surface group rounded-xl border border-border bg-card p-5 transition-colors hover:border-primary/40 hover:bg-secondary/40"><div className="flex items-center justify-between"><span className="accent-icon-tile"><Icon className="size-5" /></span><ArrowUpRight className="size-4 text-muted-foreground group-hover:text-primary" /></div><div className="mt-7 flex items-center justify-between"><h2 className="text-sm font-semibold">{name}</h2><span className="text-3xl font-semibold tabular-nums">{count ?? 0}</span></div><p className="mt-2 text-xs text-muted-foreground">{description}</p></Link>)}</div>
    <div className="quiet-surface rounded-xl border border-border bg-card p-5 flex flex-wrap items-center justify-between gap-4"><div><h2 className="text-sm font-semibold">Project configuration</h2><p className="mt-1 text-sm text-muted-foreground">Manage the project name, build cache, and access.</p></div><Button variant="outline" render={<Link to="/projects/$id/settings" params={{ id }} />}>Project settings<ArrowUpRight className="size-4" /></Button></div>
  </div>
}
