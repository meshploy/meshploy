import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Box, Database, Globe, Layers, HardDrive, Variable, FileCog, Clock, ArrowUpRight, Plus } from "lucide-react"
import { projects } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { EnvironmentBoard } from "@/components/projects/environment-board"

export const Route = createFileRoute("/_app/projects/$id/")({ component: ProjectOverview })

function ProjectOverview() {
  const { id } = Route.useParams()
  const token = useAuthStore(s => s.token)!
  const orgId = useOrgStore(s => s.currentOrg?.id)
  const { data: project } = useQuery({ queryKey: ["project", orgId, id], queryFn: () => projects.get(orgId!, id, token), enabled: !!orgId })
  const resources = [
    { name: "Services", path: "services", stats: "services", icon: Box, count: project?.services_count, description: "Applications and long-running workloads" },
    { name: "Databases", path: "databases", stats: "databases", icon: Database, count: project?.databases_count, description: "Managed data for your applications" },
    { name: "Stacks", path: "stacks", stats: "stacks", icon: Layers, count: project?.stacks_count, description: "Related resources deployed together" },
    { name: "Routes", path: "routes", stats: "routes", icon: Globe, count: project?.routes_count, description: "Domains and access to your services" },
    { name: "Volumes", path: "volumes", stats: "volumes", icon: HardDrive, count: project?.volumes_count, description: "Persistent storage across deployments" },
    { name: "Variable groups", path: "variables", stats: "variables", icon: Variable, count: project?.variables_count, description: "Shared environment and secrets" },
    { name: "Config files", path: "config-files", stats: "config_files", icon: FileCog, count: project?.config_files_count, description: "Files mounted into your services" },
    { name: "Jobs", path: "jobs", stats: "jobs", icon: Clock, count: project?.jobs_count, description: "Scheduled tasks and one-off runs" },
  ] as const
  return <div className="console-page space-y-7">
    <div className="flex flex-wrap items-start justify-between gap-4"><div><h1>Project overview</h1><p className="mt-2 text-sm text-muted-foreground">Everything running in {project?.name ?? "this project"}, in one place.</p></div><Button render={<Link to="/projects/$id/new" params={{ id }} search={{ type: "service" }} />}><Plus className="size-4" />New resource</Button></div>
    {orgId && <EnvironmentBoard orgId={orgId} projectId={id} token={token} />}
    <section aria-labelledby="resources-heading" className="space-y-3"><div><h2 id="resources-heading" className="text-sm font-semibold">Resources</h2><p className="mt-1 text-xs text-muted-foreground">What this level holds, and how it is doing.</p></div>
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">{resources.map(({ name, path, stats, icon: Icon, count, description }) => <Link key={path} to={`/projects/$id/${path}`} params={{ id }} className="overview-resource-card interactive-surface group rounded-xl border border-border bg-card p-5 transition-colors hover:border-primary/40 hover:bg-secondary/40"><div className="flex items-center justify-between"><span className="accent-icon-tile"><Icon className="size-5" /></span><ArrowUpRight className="size-4 text-muted-foreground group-hover:text-primary" /></div><div className="mt-7 flex items-center justify-between"><h2 className="text-sm font-semibold">{name}</h2><span className="text-3xl font-semibold tabular-nums">{count ?? 0}</span></div><p className="mt-2 text-xs text-muted-foreground">{description}</p><StatLine kind={stats} stats={project?.stats?.[stats]} /></Link>)}</div></section>
    <div className="quiet-surface rounded-xl border border-border bg-card p-5 flex flex-wrap items-center justify-between gap-4"><div><h2 className="text-sm font-semibold">Project configuration</h2><p className="mt-1 text-sm text-muted-foreground">Manage the project name, build cache, and access.</p></div><Button variant="outline" render={<Link to="/projects/$id/settings" params={{ id }} />}>Project settings<ArrowUpRight className="size-4" /></Button></div>
  </div>
}

// Each kind's breakdown, in the order worth reading it, and how each part looks.
const STAT_ORDER: Record<string, string[]> = {
  services: ["running", "deploying", "failed", "stopped"],
  databases: ["running", "deploying", "failed", "stopped"],
  stacks: ["running", "applying", "failed", "idle", "destroyed"],
  routes: ["https", "internal", "tcp", "paused"],
  volumes: ["ready", "idle", "failed"],
  variables: ["shared", "published"],
  config_files: ["attached", "unused"],
  jobs: ["scheduled", "manual", "failed"],
}
const STAT_LABEL: Record<string, string> = { https: "HTTPS", tcp: "TCP", published: "from services" }
// Green is live, amber on its way, red failed, grey inactive. A part that is a
// kind rather than a state (a scheduled job, a shared group) is blue.
const STAT_TONE: Record<string, string> = {
  running: "bg-emerald-400", ready: "bg-emerald-400", https: "bg-emerald-400", internal: "bg-emerald-400", tcp: "bg-emerald-400", attached: "bg-emerald-400",
  deploying: "bg-amber-400", applying: "bg-amber-400",
  failed: "bg-red-400",
  stopped: "bg-muted-foreground/50", idle: "bg-muted-foreground/50", paused: "bg-muted-foreground/50", unused: "bg-muted-foreground/50", destroyed: "bg-muted-foreground/50",
}

function StatLine({ kind, stats }: { kind: string; stats?: Record<string, number> }) {
  const parts = (STAT_ORDER[kind] ?? []).filter((k) => stats?.[k])
  if (!stats || parts.length === 0) return null
  return (
    <p className="mt-3 flex flex-wrap gap-x-3 gap-y-1 border-t border-border/60 pt-3 text-xs text-muted-foreground" data-testid={`stats-${kind}`}>
      {parts.map((k) => (
        <span key={k} className={cn("inline-flex items-center gap-1.5", k === "failed" && "text-red-300")}>
          <span className={cn("size-1.5 rounded-full", STAT_TONE[k] ?? "bg-sky-400")} />
          <span className="tabular-nums text-foreground/90">{stats[k]}</span> {STAT_LABEL[k] ?? k}
        </span>
      ))}
    </p>
  )
}
