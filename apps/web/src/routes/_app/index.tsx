import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import {
  Server,
  Box,
  Globe,
  FolderKanban,
  ChevronRight,
  Plus,
  Loader2,
  AlertCircle,
} from "lucide-react"
import { nodes as nodesApi, projects as projectsApi, activity as activityApi, toNode, toProject, type ApiAttentionItem } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin } from "@/store/org-store"
import type { Node, Project } from "@/types"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { Button } from "@/components/ui/button"
import { projectColorHue } from "@/lib/utils"
import { ExposureNotice } from "@/components/system/exposure-notice"
import { StartHere } from "@/components/system/start-here"
import { RecentActivity } from "@/components/system/recent-activity"
import { MeshLoadSummary, NodeLoadBars } from "@/components/system/mesh-load"
import { useMeshLoad } from "@/components/system/use-mesh-load"
import { NeedsAttention } from "@/components/system/needs-attention"
import { DeliveryPanel, Sparkline } from "@/components/system/delivery-panel"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { StatLine } from "@/components/system/stat-line"
import { HelpButton } from "@/help/help-button"
import { LevelDot } from "@/components/projects/environment-switcher"

export const Route = createFileRoute("/_app/")({
  // `?start` renders the getting-started panel on a workspace that is past it.
  // Not a debug flag so much as the only way to look at the first-run screen
  // without emptying a real workspace, and something a support answer can point
  // someone at.
  validateSearch: (search: Record<string, unknown>): { start?: true } =>
    search.start === true || search.start === "1" || search.start === "true"
      ? { start: true }
      : {},
  component: OverviewPage,
})

function OverviewPage() {
  const { start } = Route.useSearch()
  const isAdmin = useIsAdmin()
  const token = useAuthStore((s) => s.token)!
  const org = useOrgStore((s) => s.currentOrg)
  const orgId = org?.id

  const { data: rawNodes = [], isLoading: nodesLoading, isError: nodesError, refetch: reloadNodes } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const nodeList = rawNodes.map(toNode)

  const { data: projectList = [], isLoading: projectsLoading, isError: projectsError, refetch: reloadProjects } = useQuery({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId!, token),
    enabled: !!orgId,
    select: (raw) => raw.map(toProject),
  })

  // What needs someone, and each count broken down across every level.
  const { data: overview, isLoading: overviewLoading, isError: overviewError } = useQuery({
    queryKey: ["overview", orgId],
    queryFn: () => activityApi.overview(orgId!, token),
    enabled: !!orgId,
    refetchInterval: 15_000,
  })
  const attention = overview?.attention ?? []
  const stats = overview?.stats ?? {}
  const sum = (kind: string) => Object.values(stats[kind] ?? {}).reduce((a, b) => a + b, 0)

  const onlineNodes = nodeList.filter((n) => n.status === "online").length
  const load = useMeshLoad(nodeList)
  // Across every level when the breakdown is in, production alone until then.
  const totalServices = overview ? sum("services") : projectList.reduce((s, p) => s + p.servicesCount, 0)
  const totalRoutes = overview ? sum("routes") : projectList.reduce((s, p) => s + p.routesCount, 0)
  const withServices = projectList.filter((p) => p.servicesCount > 0).length
  // Copies in environment levels, which the project list does not count.
  const inLevels = totalServices - projectList.reduce((s, p) => s + p.servicesCount, 0)

  if (nodesLoading || projectsLoading) return <div className="console-page flex items-center gap-3 text-muted-foreground"><Loader2 className="size-4 animate-spin" />Loading your workspace…</div>

  return (
    <div className="console-page p-6 space-y-6">
      <ExposureNotice />
      {(nodesError || projectsError) && <div role="alert" className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/40 p-4"><AlertCircle className="size-4 text-destructive" /><span className="text-sm">Some workspace data could not be loaded.</span><Button variant="outline" onClick={() => { reloadNodes(); reloadProjects() }}>Try again</Button></div>}

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight flex items-center gap-2" aria-labelledby="page-title"><span id="page-title">Workspace overview</span><HelpButton topic="overview" label="What the overview shows" /></h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Your projects, deployments, and nodes in one place.
          </p>
        </div>
        {isAdmin && <Button size="sm" render={<Link to="/projects/new" />}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          New project
        </Button>}
      </div>

      <StartHere nodes={nodeList} projects={projectList} forced={start} />

      {projectList.length > 0 && <NeedsAttention items={attention} loading={overviewLoading} failed={overviewError} />}
      {/* Stat cards. Held back on an empty workspace: four zeros say nothing that
          the panel above has not said better. */}
      {projectList.length > 0 && <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={<Server className="h-3.5 w-3.5" />}
          label="Nodes online"
          value={String(onlineNodes)}
          denom={`/ ${nodeList.length}`}
          sub={onlineNodes < nodeList.length ? `offline: ${nodeList.filter((n) => n.status !== "online").map((n) => n.name).join(", ")}` : "mesh nodes"}
          accent={nodeList.length > 0 && onlineNodes < nodeList.length ? "warn" : undefined}
        />
        <StatCard
          icon={<Box className="h-3.5 w-3.5" />}
          label="Services"
          value={String(totalServices)}
          sub={`in ${withServices} project${withServices !== 1 ? "s" : ""}${inLevels > 0 ? ` · ${inLevels} in ${withServices === 1 ? "its" : "their"} levels` : ""}`}
          breakdown={<StatLine kind="services" stats={stats.services} className="mt-0 border-0 pt-0" />}
        />
        <StatCard
          icon={<FolderKanban className="h-3.5 w-3.5" />}
          label="Projects"
          value={String(projectList.length)}
          sub={
            projectList.some((p) => p.levels.length > 0)
              ? `${projectList.filter((p) => p.levels.length > 0).length} with environment levels`
              : "in this workspace"
          }
        />
        <StatCard
          icon={<Globe className="h-3.5 w-3.5" />}
          label="Routes"
          value={String(totalRoutes)}
          sub={stats.routes ? "" : "public and internal access"}
          breakdown={<StatLine kind="routes" stats={stats.routes} className="mt-0 border-0 pt-0" />}
        />
      </div>}

      {projectList.length > 0 && overview?.delivery && <DeliveryPanel delivery={overview.delivery} />}

      <div className="flex items-center justify-between"><h2 className="text-base font-semibold">{org?.name ?? "Your workspace"}</h2>{isAdmin && <Link to="/cluster" className="text-sm text-muted-foreground hover:text-primary">View cluster →</Link>}</div>
      {/* Mesh topology + Projects */}
      {/* Both panels are held to one height and scroll inside it. Either list
          grows without warning - eight deployments, twenty projects - and a row
          of panels that changes shape as the workspace fills reads as broken
          rather than as full. */}
      <div className="grid gap-4 lg:grid-cols-3 lg:h-[21rem]">
        <div className="lg:col-span-2 min-h-0 h-[21rem] lg:h-auto">
          <RecentActivity />
        </div>

        <div className="quiet-surface flex h-[21rem] lg:h-auto min-h-0 flex-col rounded-xl border border-border/60 bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between shrink-0">
            <p className="text-sm font-semibold text-foreground">Projects</p>
            <Link to="/projects" className="text-xs text-muted-foreground hover:text-foreground transition-colors">
              All →
            </Link>
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto p-2">
            {projectList.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">No projects yet</div>
            ) : (
              <div className="flex flex-col gap-0.5">
                {projectList.map((p) => (
                  <ProjectRow key={p.id} project={p} waiting={attention.filter((a) => a.kind === "promotion_waiting" && a.project_id === p.id)} deploys={overview?.delivery?.projects?.[p.id]} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
      <div className="quiet-surface rounded-xl border border-border bg-card overflow-hidden">
        <div className="flex items-center justify-between border-b border-border px-5 py-4"><div><h2 className="text-sm font-semibold">Infrastructure</h2><div className="mt-1">{load.reporting > 0 ? <MeshLoadSummary load={load} /> : <p className="text-xs text-muted-foreground">Connectivity and capacity across your mesh</p>}</div></div><Link to="/nodes" className="text-xs text-muted-foreground hover:text-primary">Manage nodes →</Link></div>
        {nodeList.length === 0 ? <p className="p-6 text-sm text-muted-foreground">Connect your first node to start deploying workloads.</p> : nodeList.map(n => <Link key={n.id} to="/nodes/$id" params={{ id: n.id }} className="flex flex-wrap items-center gap-4 px-5 py-4 border-b border-border/50 last:border-0 hover:bg-secondary/40"><div className="flex items-center gap-3 min-w-40 flex-1"><Server className="size-4 text-muted-foreground" /><div><p className="text-sm font-medium">{n.name}</p><p className="text-xs text-muted-foreground mt-1">{n.k3sRole === "server" ? "Control plane" : "Worker"} · {n.tailscaleIP}</p></div></div>{n.status === "online" ? <NodeLoadBars load={load.byNode[n.id]} /> : <div className="text-xs text-muted-foreground">{n.cpuCores} CPU · {n.memoryGB} GB memory</div>}<span className="flex items-center gap-2 text-xs capitalize min-w-20"><NodeStatusDot status={n.status} />{n.status}</span><ChevronRight className="size-4 text-muted-foreground" /></Link>)}
      </div>
    </div>
  )
}

function StatCard({
  icon,
  label,
  value,
  denom,
  sub,
  breakdown,
  accent,
}: {
  icon: React.ReactNode
  label: string
  value: string
  denom?: string
  sub: string
  /** The count broken down, above sub. */
  breakdown?: React.ReactNode
  accent?: "warn"
}) {
  return (
    <div className="quiet-surface rounded-xl border border-border bg-card p-5 space-y-3">
      <div className="flex items-center gap-1.5 text-muted-foreground text-xs font-medium">
        {icon}
        {label}
      </div>
      <p className={`text-3xl font-semibold tabular-nums ${accent === "warn" ? "text-amber-400" : "text-foreground"}`}>
        {value}
        {denom && <span className="text-base font-normal text-muted-foreground ml-1">{denom}</span>}
      </p>
      {/* The breakdown draws nothing until there is something in it. */}
      <div className="space-y-1">
        {breakdown}
        {sub && <p className="text-xs text-muted-foreground">{sub}</p>}
      </div>
    </div>
  )
}

function ProjectRow({ project, waiting, deploys }: { project: Project; waiting: ApiAttentionItem[]; deploys?: number[] }) {
  const hue = projectColorHue(project.id)
  return (
    <Link
      to="/projects/$id"
      params={{ id: project.id }}
      className="flex items-center gap-3 px-3 py-2.5 rounded-md hover:bg-muted/50 transition-colors"
    >
      <div
        className="w-1.5 h-7 rounded-full shrink-0"
        style={{ background: `oklch(0.72 0.17 ${hue})` }}
      />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-foreground leading-tight truncate">{project.name}</p>
        <p className="text-xs text-muted-foreground">
          {project.servicesCount} service{project.servicesCount !== 1 ? "s" : ""} · {project.routesCount} route{project.routesCount !== 1 ? "s" : ""}
        </p>
        {/* Its levels, lowest first, the way promotion moves. */}
        {project.levels.length > 0 && (
          <p className="mt-1 flex items-center gap-1 whitespace-nowrap text-[11px] text-muted-foreground" data-testid="level-chain">
            {[...project.levels].reverse().map((l) => (
              <span key={l.id} className="inline-flex items-center gap-1">
                <LevelDot production={false} />
                {l.name}
                <span className="text-muted-foreground/50">→</span>
              </span>
            ))}
            <span className="inline-flex items-center gap-1">
              <LevelDot production />
              production
            </span>
          </p>
        )}
      </div>
      {/* Stacked, so the level chain keeps its line. */}
      <div className="flex shrink-0 flex-col items-end gap-1">
        {waiting.length > 0 && (
          <Tooltip>
            <TooltipTrigger
              render={<span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 text-[10px] text-sky-300" />}
            >
              {waiting.length === 1 ? "promotion waiting" : `${waiting.length} promotions waiting`}
            </TooltipTrigger>
            <TooltipContent>
              <div className="space-y-0.5">
                {waiting.map((w) => (
                  <p key={w.title}>{w.title}</p>
                ))}
              </div>
            </TooltipContent>
          </Tooltip>
        )}
        <Sparkline counts={deploys} />
      </div>
      <ChevronRight className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
    </Link>
  )
}

export function MeshGraph({ nodes, height = 220 }: { nodes: Node[]; height?: number }) {
  const gateway = nodes.find((n) => n.k3sRole === "server")
  const agents = nodes.filter((n) => n.k3sRole === "agent")

  const W = 560
  const H = height
  const cx = W / 2
  const cy = H / 2
  const R = Math.min(W, H) * 0.4

  if (nodes.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-sm text-muted-foreground"
        style={{ height }}
      >
        No nodes in the mesh yet
      </div>
    )
  }

  return (
    <svg className="mesh-graph" viewBox={`0 0 ${W} ${H}`} style={{ width: "100%", height }} role="img" aria-label="Mesh connections between the control plane and worker nodes">
      {agents.map((a, i) => {
        const t = (i / Math.max(agents.length, 1)) * Math.PI * 2 + 0.4
        const x = cx + Math.cos(t) * R
        const y = cy + Math.sin(t) * R * 0.82
        const live = a.status === "online" && gateway?.status === "online"
        return (
          <g key={a.id}>
            {gateway && <line
              x1={cx} y1={cy} x2={x} y2={y}
              stroke={live ? "oklch(0.72 0.17 160 / 0.35)" : "oklch(0.35 0.005 90 / 0.4)"}
              strokeWidth="1"
              strokeDasharray={live ? undefined : "3 3"}
            />}
            {live && (
              <circle r="2.5" fill="oklch(0.72 0.17 160)" opacity="0.85">
                <animateMotion
                  dur={`${3 + i * 0.5}s`}
                  repeatCount="indefinite"
                  path={`M${cx} ${cy} L${x} ${y}`}
                />
              </circle>
            )}
            <circle cx={x} cy={y} r="14" fill="oklch(0.13 0.005 90)" stroke="oklch(0.22 0.005 90)" />
            <circle cx={x} cy={y} r="4" fill={a.status === "online" ? "oklch(0.72 0.17 160)" : "oklch(0.40 0.005 90)"} />
            <text
              x={x} y={y + 27}
              fill="var(--muted-foreground)"
              fontSize="12"
              textAnchor="middle"
              fontFamily="Manrope, sans-serif"
            >
              {a.name}
            </text>
          </g>
        )
      })}
      {/* Gateway status is reported, not inferred from its role. */}
      {gateway ? <g>
      <circle cx={cx} cy={cy} r="20" fill={gateway.status === "online" ? "oklch(0.72 0.17 160 / 0.1)" : "oklch(0.3 0.01 90 / 0.1)"} stroke={gateway.status === "online" ? "oklch(0.72 0.17 160 / 0.4)" : "oklch(0.5 0.01 90 / 0.4)"} strokeWidth="1.5" />
      <circle cx={cx} cy={cy} r="7" fill={gateway.status === "online" ? "oklch(0.72 0.17 160)" : "oklch(0.55 0.01 90)"} />
      <text
        x={cx} y={cy + 36}
        fill="oklch(0.78 0.005 90)"
        fontSize="12"
        textAnchor="middle"
        fontWeight="600"
        fontFamily="Manrope, sans-serif"
      >
        {gateway?.name ?? "gateway"}
      </text>
      {gateway?.tailscaleIP && (
        <text
          x={cx} y={cy + 48}
          fill="var(--muted-foreground)"
          fontSize="11"
          textAnchor="middle"
          fontFamily="Manrope, sans-serif"
        >
          {gateway.tailscaleIP}
        </text>
      )}
      </g> : <text x={cx} y={cy} textAnchor="middle" fill="var(--muted-foreground)" fontSize="12">No control plane reported</text>}
    </svg>
  )
}
