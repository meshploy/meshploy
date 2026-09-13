import { OptionSelect } from "@/components/layout/option-select"
import { createFileRoute, Link, Outlet, useParams, useRouterState, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Box, Database, Globe, Layers, HardDrive, Variable, FileCog, Clock, Settings, Home, Plus, ArrowLeft, Loader2 } from "lucide-react"
import { projects as projectsApi, services as servicesApi, toProject } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { livePoll } from "@/lib/live-poll"

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
      <div className="project-identity"><p className="font-semibold text-sm">{project.name}</p><p className="text-xs text-muted-foreground mt-1 font-mono">{project.slug}</p></div>
      <nav>{tabs.map(t => <Link key={t.segment} to={destination(t.segment)} activeOptions={{ exact: true }} className={active === t.segment ? "active" : ""} aria-current={active === t.segment ? "page" : undefined}><t.icon className="h-4 w-4" />{t.label}{t.count != null && <span className="project-count">{t.count}</span>}</Link>)}</nav>
      <Button className="w-full mt-6 gap-2" variant="outline" render={<Link to="/projects/$id/new" params={{ id }} search={{ type: "service" }} />}><Plus className="h-4 w-4" />New resource</Button>
    </aside>
    <div className="project-content">
      <div className="project-mobile-navigation"><OptionSelect label="Project section" value={active} onChange={value => navigate({ to: destination(value) })} className="w-full" options={[...tabs.map(t => ({value: t.segment,label: `${project.name} / ${t.label}${t.count != null ? ` (${t.count})` : ""}`})),{value:"new",label:"New resource"}]} /></div>
      <Outlet />
    </div>
  </div>
}
