import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Boxes, ChevronRight, Network } from "lucide-react"
import { nodes as nodesApi, type ApiHostContainer, type ApiHostContainers } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table"
import { formatRelativeTime } from "@/lib/utils"

// What else runs on this machine.
//
// Read-only. The table lives on the Discovery page; the node page carries a
// summary line that links to it, so one reader is never shown the same list
// twice. Acting on a container - importing it, later starting or stopping it -
// is a named request the host agent picks up, and none exist yet.

const thCls = "px-4 py-2.5 font-medium text-muted-foreground/70 text-[11px]"
const tdCls = "px-4 py-3"

const STATE_STYLES: Record<string, string> = {
  running: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  restarting: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  paused: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  exited: "bg-muted text-muted-foreground border-border",
  created: "bg-muted text-muted-foreground border-border",
  dead: "bg-destructive/10 text-destructive border-destructive/20",
}

function ports(c: ApiHostContainer): string {
  if (!c.ports?.length) return ""
  return c.ports
    .map((p) => `${p.host_ip && p.host_ip !== "0.0.0.0" ? `${p.host_ip}:` : ""}${p.host_port}→${p.port}${p.protocol && p.protocol !== "tcp" ? `/${p.protocol}` : ""}`)
    .join(", ")
}

/** A line on the node page, not a second table.
 *
 *  Discovery lists every node's containers and endpoints together; repeating
 *  the table here would be two things to keep in step for one reader. What the
 *  node page owes is the fact that there *is* something else on this machine,
 *  and a way to go and look. */
export function HostContainers({ orgId, nodeId, token }: { orgId: string; nodeId: string; token: string }) {
  const { data } = useQuery<ApiHostContainers>({
    queryKey: ["node-containers", orgId, nodeId],
    queryFn: () => nodesApi.listContainers(orgId, nodeId, token),
    refetchInterval: 60_000,
    retry: false,
    throwOnError: false,
  })

  // No agent, no runtime, or nothing else running: nothing to say.
  if (!data?.available || data.containers.length === 0) return null

  const running = data.containers.filter((c) => c.state === "running").length

  return (
    <Link
      to="/discovery"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-border bg-muted/10 px-4 py-3 text-xs text-muted-foreground transition-colors hover:border-border/80 hover:bg-muted/20"
    >
      <span className="inline-flex items-center gap-1.5 text-foreground">
        <Boxes className="h-3.5 w-3.5" />
        {data.containers.length} {data.containers.length === 1 ? "container" : "containers"} Meshploy does not manage
      </span>
      <span>{running} running</span>
      {data.groups.length > 0 && (
        <span>{data.groups.length} {data.groups.length === 1 ? "project" : "projects"}</span>
      )}
      {data.checked_at && <span>Checked {formatRelativeTime(new Date(data.checked_at))}</span>}
      <span className="ml-auto inline-flex items-center gap-1 text-primary">
        Open in Discovery
        <ChevronRight className="h-3 w-3" />
      </span>
    </Link>
  )
}

function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex gap-2">
      <dt className="shrink-0 text-muted-foreground/70">{label}</dt>
      <dd className={`min-w-0 break-all ${mono ? "font-mono text-[11px] text-foreground/80" : "text-foreground/80"}`}>{value}</dd>
    </div>
  )
}

/** The table itself, so the node page and the Discovery page show one thing. */
export function ContainerTable({ containers }: { containers: ApiHostContainer[] }) {
  const [open, setOpen] = useState<string | null>(null)

  if (containers.length === 0) {
    return (
      <div className="console-data-table rounded-xl border border-border p-8 text-center text-sm text-muted-foreground/50">
        Nothing here that Meshploy does not run.
      </div>
    )
  }

  return (
      <div className="console-data-table rounded-xl border border-border overflow-hidden">
        <Table>
          <TableHeader className="bg-muted/20">
            <TableRow className="border-b border-border/40 hover:bg-transparent">
              <TableHead className={thCls}>Container</TableHead>
              <TableHead className={thCls}>State</TableHead>
              <TableHead className={thCls}>Project</TableHead>
              <TableHead className={thCls}>Published</TableHead>
              <TableHead className={thCls}>Memory</TableHead>
              <TableHead className={thCls}>CPU</TableHead>
              <TableHead className={thCls}>Started</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {containers.map((c) => {
              const expanded = open === c.id
              const published = ports(c)
              return [
                <TableRow
                  key={c.id}
                  className="border-b border-border/30 cursor-pointer"
                  onClick={() => setOpen(expanded ? null : c.id)}
                >
                  <TableCell className={tdCls}>
                    <div className="flex items-center gap-1.5">
                      <ChevronRight className={`h-3 w-3 text-muted-foreground/60 transition-transform ${expanded ? "rotate-90" : ""}`} />
                      <code className="font-mono text-[11px] text-foreground">{c.name}</code>
                      {c.host_network && (
                        <Badge variant="outline" className="text-[11px] px-1.5 py-0 h-4 gap-1">
                          <Network className="h-2.5 w-2.5" />
                          host network
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className={tdCls}>
                    <Badge className={`text-[11px] px-1.5 py-0 h-4 border ${STATE_STYLES[c.state] ?? STATE_STYLES.created}`}>
                      {c.state}
                    </Badge>
                    {(c.restart_count ?? 0) > 0 && (
                      <span className="ml-2 text-[11px] text-muted-foreground">{c.restart_count} restarts</span>
                    )}
                  </TableCell>
                  <TableCell className={`${tdCls} text-xs`}>
                    {c.group
                      ? <span className="text-foreground/80">{c.group}{c.kind === "swarm" ? " (swarm)" : ""}</span>
                      : <span className="text-muted-foreground/40">—</span>}
                  </TableCell>
                  <TableCell className={`${tdCls} font-mono text-[11px] text-muted-foreground`}>
                    {published || <span className="text-muted-foreground/40">—</span>}
                  </TableCell>
                  <TableCell className={`${tdCls} tabular-nums text-xs text-muted-foreground`}>
                    {c.memory_mb ? `${c.memory_mb} MB` : <span className="text-muted-foreground/40">—</span>}
                  </TableCell>
                  <TableCell className={`${tdCls} tabular-nums text-xs text-muted-foreground`}>
                    {c.cpu_percent ? `${c.cpu_percent}%` : <span className="text-muted-foreground/40">—</span>}
                  </TableCell>
                  <TableCell className={`${tdCls} text-xs text-muted-foreground`}>
                    {c.started_at ? formatRelativeTime(new Date(c.started_at)) : "—"}
                  </TableCell>
                </TableRow>,
                expanded ? (
                  <TableRow key={`${c.id}-detail`} className="border-b border-border/30 hover:bg-transparent">
                    <TableCell colSpan={7} className="px-4 py-3 bg-muted/10">
                      <dl className="grid gap-x-8 gap-y-2 text-xs sm:grid-cols-2">
                        <Detail label="Image" value={c.image} mono />
                        <Detail label="Network" value={c.network_mode || "—"} mono />
                        {c.status && <Detail label="Status" value={c.status} />}
                        {c.health && <Detail label="Health" value={c.health} />}
                        {c.compose_service && <Detail label="Compose service" value={c.compose_service} mono />}
                        {c.volumes?.length ? <Detail label="Volumes" value={c.volumes.join(", ")} mono /> : null}
                        {c.bind_sources?.length ? <Detail label="Host paths" value={c.bind_sources.join(", ")} mono /> : null}
                      </dl>
                      {c.host_network && (
                        <p className="mt-3 text-xs text-muted-foreground">
                          On the host&apos;s network, so it cannot become a Meshploy service. It keeps running here; Meshploy can still serve its hostname or publish its port.
                        </p>
                      )}
                    </TableCell>
                  </TableRow>
                ) : null,
              ]
            })}
          </TableBody>
        </Table>
      </div>
  )
}
