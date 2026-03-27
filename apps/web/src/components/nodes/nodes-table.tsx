import { useNavigate } from "@tanstack/react-router"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { NodeStatusDot } from "./node-status-dot"
import { formatRelativeTime } from "@/lib/utils"
import type { Node } from "@/types"

interface NodesTableProps {
  nodes: Node[]
}

export function NodesTable({ nodes }: NodesTableProps) {
  const navigate = useNavigate()

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent border-border/60">
            <TableHead className="text-xs font-medium text-muted-foreground w-45">Name</TableHead>
            <TableHead className="text-xs font-medium text-muted-foreground">Mesh IP</TableHead>
            <TableHead className="text-xs font-medium text-muted-foreground">Role</TableHead>
            <TableHead className="text-xs font-medium text-muted-foreground">OS</TableHead>
            <TableHead className="text-xs font-medium text-muted-foreground">Resources</TableHead>
            <TableHead className="text-xs font-medium text-muted-foreground">K3s</TableHead>
            <TableHead className="text-xs font-medium text-muted-foreground text-right">Last Seen</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {nodes.map((node) => (
            <TableRow
              key={node.id}
              className="cursor-pointer hover:bg-muted/30 border-border/40 transition-colors"
              onClick={() => navigate({ to: "/nodes/$id", params: { id: node.id } })}
            >
              <TableCell className="py-3.5">
                <div className="flex items-center gap-2.5">
                  <NodeStatusDot status={node.status} />
                  <span className="text-sm font-medium text-foreground">{node.name}</span>
                </div>
              </TableCell>
              <TableCell className="py-3.5">
                <code className="text-xs font-mono text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">
                  {node.tailscaleIP}
                </code>
              </TableCell>
              <TableCell className="py-3.5">
                <Badge variant={node.k3sRole === "server" ? "default" : "secondary"} className="text-[10px] font-medium px-1.5 py-0 h-4.5">
                  {node.k3sRole}
                </Badge>
              </TableCell>
              <TableCell className="py-3.5">
                <span className="text-xs text-muted-foreground">{node.os}</span>
              </TableCell>
              <TableCell className="py-3.5">
                <span className="text-xs text-muted-foreground tabular-nums">
                  {node.cpuCores}c / {node.memoryGB}GB / {node.diskGB}GB
                </span>
              </TableCell>
              <TableCell className="py-3.5">
                <code className="text-xs font-mono text-muted-foreground/70">{node.k3sVersion}</code>
              </TableCell>
              <TableCell className="py-3.5 text-right">
                <span className={node.status === "online" ? "text-xs text-emerald-400/80" : "text-xs text-muted-foreground/50"}>
                  {formatRelativeTime(node.lastSeenAt)}
                </span>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
