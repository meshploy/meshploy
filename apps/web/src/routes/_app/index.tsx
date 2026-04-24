import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import {
  Server,
  Box,
  Globe,
  FolderKanban,
  ChevronRight,
  Plus,
} from "lucide-react"
import { nodes as nodesApi, projects as projectsApi, toNode, toProject } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { Node, Project } from "@/types"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { projectColorHue } from "@/lib/utils"

export const Route = createFileRoute("/_app/")({
  component: OverviewPage,
})

function OverviewPage() {
  const token = useAuthStore((s) => s.token)!
  const org = useOrgStore((s) => s.currentOrg)
  const orgId = org?.id

  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const nodeList = rawNodes.map(toNode)

  const { data: projectList = [] } = useQuery({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId!, token),
    enabled: !!orgId,
    select: (raw) => raw.map(toProject),
  })

  const onlineNodes = nodeList.filter((n) => n.status === "online").length
  const totalServices = projectList.reduce((s, p) => s + p.servicesCount, 0)
  const totalRoutes = projectList.reduce((s, p) => s + p.routesCount, 0)

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Overview</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {org?.name} · {nodeList.length} node{nodeList.length !== 1 ? "s" : ""} · {projectList.length} project{projectList.length !== 1 ? "s" : ""}
          </p>
        </div>
        <Button size="sm" render={<Link to="/projects" />}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          New project
        </Button>
      </div>

      {/* Stat cards */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={<Server className="h-3.5 w-3.5" />}
          label="Nodes online"
          value={String(onlineNodes)}
          denom={`/ ${nodeList.length}`}
          sub="mesh nodes"
          accent={nodeList.length > 0 && onlineNodes < nodeList.length ? "warn" : undefined}
        />
        <StatCard
          icon={<Box className="h-3.5 w-3.5" />}
          label="Services"
          value={String(totalServices)}
          sub={`across ${projectList.length} project${projectList.length !== 1 ? "s" : ""}`}
        />
        <StatCard
          icon={<FolderKanban className="h-3.5 w-3.5" />}
          label="Projects"
          value={String(projectList.length)}
          sub="K3s namespaces"
        />
        <StatCard
          icon={<Globe className="h-3.5 w-3.5" />}
          label="Routes"
          value={String(totalRoutes)}
          sub="public + internal"
        />
      </div>

      {/* Mesh topology + Projects */}
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2 rounded-lg border border-border/60 bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Mesh topology</p>
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4.5">WireGuard</Badge>
          </div>
          <div className="p-4">
            <MeshGraph nodes={nodeList} />
          </div>
        </div>

        <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Projects</p>
            <Link to="/projects" className="text-xs text-muted-foreground hover:text-foreground transition-colors">
              All →
            </Link>
          </div>
          <div className="p-2">
            {projectList.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">No projects yet</div>
            ) : (
              <div className="flex flex-col gap-0.5">
                {projectList.map((p) => (
                  <ProjectRow key={p.id} project={p} />
                ))}
              </div>
            )}
          </div>
        </div>
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
  accent,
}: {
  icon: React.ReactNode
  label: string
  value: string
  denom?: string
  sub: string
  accent?: "warn"
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-1.5">
      <div className="flex items-center gap-1.5 text-muted-foreground text-xs font-medium">
        {icon}
        {label}
      </div>
      <p className={`text-2xl font-semibold tabular-nums ${accent === "warn" ? "text-amber-400" : "text-foreground"}`}>
        {value}
        {denom && <span className="text-base font-normal text-muted-foreground ml-1">{denom}</span>}
      </p>
      <p className="text-xs text-muted-foreground">{sub}</p>
    </div>
  )
}

function ProjectRow({ project }: { project: Project }) {
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
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: "100%", height }} aria-hidden>
      {agents.map((a, i) => {
        const t = (i / Math.max(agents.length, 1)) * Math.PI * 2 + 0.4
        const x = cx + Math.cos(t) * R
        const y = cy + Math.sin(t) * R * 0.82
        const live = a.status === "online"
        return (
          <g key={a.id}>
            <line
              x1={cx} y1={cy} x2={x} y2={y}
              stroke={live ? "oklch(0.72 0.17 160 / 0.35)" : "oklch(0.35 0.005 90 / 0.4)"}
              strokeWidth="1"
              strokeDasharray={live ? undefined : "3 3"}
            />
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
            <circle cx={x} cy={y} r="4" fill={live ? "oklch(0.72 0.17 160)" : "oklch(0.40 0.005 90)"} />
            <text
              x={x} y={y + 27}
              fill="oklch(0.50 0.005 90)"
              fontSize="9.5"
              textAnchor="middle"
              fontFamily="ui-monospace, monospace"
            >
              {a.name}
            </text>
          </g>
        )
      })}
      {/* Gateway */}
      <circle cx={cx} cy={cy} r="20" fill="oklch(0.72 0.17 160 / 0.1)" stroke="oklch(0.72 0.17 160 / 0.4)" strokeWidth="1.5" />
      <circle cx={cx} cy={cy} r="7" fill="oklch(0.72 0.17 160)" />
      <text
        x={cx} y={cy + 36}
        fill="oklch(0.78 0.005 90)"
        fontSize="10"
        textAnchor="middle"
        fontWeight="600"
        fontFamily="ui-monospace, monospace"
      >
        {gateway?.name ?? "gateway"}
      </text>
      {gateway?.tailscaleIP && (
        <text
          x={cx} y={cy + 48}
          fill="oklch(0.48 0.005 90)"
          fontSize="9"
          textAnchor="middle"
          fontFamily="ui-monospace, monospace"
        >
          {gateway.tailscaleIP}
        </text>
      )}
    </svg>
  )
}
