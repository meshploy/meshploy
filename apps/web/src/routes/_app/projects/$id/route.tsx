import { OptionSelect } from "@/components/layout/option-select"
import { createFileRoute, Link, Outlet, useParams, useRouterState, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Box, Database, Globe, Layers, HardDrive, Variable, FileCog, Clock, Settings, Home, Plus, ArrowLeft, Loader2 } from "lucide-react"
import { projects as projectsApi, services as servicesApi, toProject } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { livePoll } from "@/lib/live-poll"
import { EnvironmentSwitcher, LevelDot } from "@/components/projects/environment-switcher"

export const Route = createFileRoute("/_app/projects/$id")({ component: ProjectLayout })
function ProjectLayout() {
  const { id } = useParams({ from: "/_app/projects/$id" })
  const token = useAuthStore(s => s.token)!
  const orgId = useOrgStore(s => s.currentOrg?.id)
  const pathname = useRouterState({ select: s => s.location.pathname })
  const navigate = useNavigate()
  const { data, isPending, error, refetch } = useQuery({ queryKey: ["project", orgId, id], queryFn: () => projectsApi.get(orgId!, id, token), enabled: !!orgId, refetchInterval: livePoll(() => false) })
  const serviceId = pathname.match(/\/services\/([^/]+)/)?.[1]
  const { data: service } = useQuery({ queryKey: ["service", orgId, id, serviceId], queryFn: () => servicesApi.get(orgId!, id, serviceId!, token), enabled: !!orgId && !!serviceId })
  if (isPending) return <div className="console-page flex items-center gap-3 text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />Loading project…</div>
  if (error || !data) return <div className="console-page space-y-4"><h1>Unable to load project</h1><p className="text-muted-foreground">{error?.message}</p><Button onClick={() => refetch()}>Try again</Button></div>
  const project = toProject(data)
  const tabs = [
    { segment: "", label: "Overview", icon: Home, count: null },
    { segment: "services", label: "Services", icon: Box, count: project.servicesCount },
    { segment: "databases", label: "Databases", icon: Database, count: project.databasesCount },
    { segment: "stacks", label: "Stacks", icon: Layers, count: project.stacksCount },
    { segment: "routes", label: "Routes", icon: Globe, count: project.routesCount },
    { segment: "volumes", label: "Volumes", icon: HardDrive, count: project.volumesCount },
    { segment: "variables", label: "Variable groups", icon: Variable, count: project.variablesCount },
    { segment: "config-files", label: "Config files", icon: FileCog, count: project.configFilesCount },
    { segment: "jobs", label: "Jobs", icon: Clock, count: project.jobsCount },
    { segment: "settings", label: "Settings", icon: Settings, count: null },
  ]
  let active = pathname.split(`/projects/${id}`)[1]?.split("/").filter(Boolean)[0] ?? ""
  if (service?.type === "database" && active === "services") active = "databases"
  const destination = (segment: string) => `/projects/${id}${segment ? `/${segment}` : "/"}`
  return <div className="project-layout">
    <aside className="project-navigation" aria-label="Project navigation">
      <Link to="/projects" className="flex items-center gap-2 px-2 text-xs text-muted-foreground"><ArrowLeft className="h-3.5 w-3.5" />Projects</Link>
      <div className="project-identity"><p className="font-semibold text-sm">{project.name}</p><p className="text-xs text-muted-foreground mt-1 font-mono">{project.slug}</p>{orgId && <EnvironmentSwitcher orgId={orgId} projectId={id} section={active} token={token} />}</div>
      <nav>{tabs.map(t => <Link key={t.segment} to={destination(t.segment)} activeOptions={{ exact: true }} className={active === t.segment ? "active" : ""} aria-current={active === t.segment ? "page" : undefined}><t.icon className="h-4 w-4" />{t.label}{t.count != null && <span className="project-count">{t.count}</span>}</Link>)}</nav>
      <Button className="w-full mt-6 gap-2" variant="outline" render={<Link to="/projects/$id/new" params={{ id }} search={{ type: "service" }} />}><Plus className="h-4 w-4" />New resource</Button>
    </aside>
    <div className="project-content">
      {/* On a phone the sidebar is gone, so the level switcher sits beside the section picker. */}
      <div className="project-mobile-navigation"><div className="flex flex-col gap-2 min-[480px]:flex-row min-[480px]:items-stretch">{orgId && <div className="min-[480px]:w-[38%] min-[480px]:min-w-[120px] min-[480px]:shrink-0"><EnvironmentSwitcher orgId={orgId} projectId={id} section={active} token={token} className="mt-0 h-full text-sm" /></div>}<OptionSelect label="Project section" value={active} onChange={value => navigate({ to: destination(value) })} className="w-full min-w-0 flex-1" options={[...tabs.map(t => ({value: t.segment,label: `${project.name} / ${t.label}${t.count != null ? ` (${t.count})` : ""}`})),{value:"new",label:"New resource"}]} /></div></div>
      {/* A level looks like its project on every tab, so say which one this is. */}
      {data.parent_project_id && <LevelBanner orgId={orgId!} projectId={id} name={data.env_name ?? ""} token={token} />}
      <Outlet />
    </div>
  </div>
}

/**
 * Says which level this is, since a level looks like its project on every tab,
 * and, when it has no database of its own, whose it uses: its services write
 * to that level's data, which is the one thing about a level that is not
 * contained in it.
 */
function LevelBanner({ orgId, projectId, name, token }: { orgId: string; projectId: string; name: string; token: string }) {
  const { data: levels = [] } = useQuery({
    queryKey: ["environments", orgId, projectId],
    queryFn: () => projectsApi.environments(orgId, projectId, token),
  })
  const here = levels.find(l => l.project_id === projectId)
  const borrowed = here && here.databases_count === 0
    ? levels.filter(l => l.level < here.level && l.databases_count > 0).sort((a, b) => b.level - a.level)[0]
    : undefined
  return <div role="status" className="mb-4 flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90">
    <span className="mt-1"><LevelDot production={false} /></span>
    <span>
      You are in <strong className="font-semibold">{name}</strong>. What you change here stays in {name}; production is not touched.
      {borrowed && <> {name} has no database of its own, so it uses <strong className="font-semibold">{borrowed.name}</strong>&apos;s: writes here change {borrowed.name}&apos;s data.</>}
    </span>
  </div>
}
