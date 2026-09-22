import { ArrowRight, Check, TriangleAlert } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { NODE_ROLES } from "@/components/nodes/role-picker"
import type { MeshRole, Node } from "@/types"

/**
 * What changing a node's role will actually do, before it is done.
 *
 * Not a generic "are you sure": the two roles decide which of two things the
 * cluster is told, and each has a different consequence worth naming.
 *
 *   - The **builder label** is what build jobs select on. Take it away and
 *     builds stop being placed here - which is fine, unless this was the only
 *     node that had it, in which case every deploy from Git fails until another
 *     build node exists.
 *   - The **NoSchedule taint** goes on a builds-only node. It stops *new*
 *     placements and does not evict: what is already running here keeps
 *     running, and moves only when something next schedules it. That is the
 *     part people get wrong in both directions - they expect an outage that
 *     does not come, or expect a clean move that has not happened.
 *
 * So the dialog is built from the transition and from what else the cluster has,
 * rather than from a sentence per role.
 */
export function RoleChangeDialog({
  node,
  to,
  nodes,
  onConfirm,
  onCancel,
}: {
  node: Node
  /** The role being moved to, or null when nothing is pending. */
  to: MeshRole | null
  /** Every node in the org, for the questions that are about the cluster and
   *  not about this machine. */
  nodes: Node[]
  onConfirm: () => void
  onCancel: () => void
}) {
  if (!to) return null

  const from = node.meshRole
  const label = (r: MeshRole) => NODE_ROLES.find((x) => x.value === r)?.label ?? r

  const builds = (r: MeshRole) => r === "workload_builder" || r === "builder"
  const services = (r: MeshRole) => r === "workload_builder" || r === "workload"

  const losesBuilds = builds(from) && !builds(to)
  const gainsBuilds = !builds(from) && builds(to)
  const losesServices = services(from) && !services(to)
  const gainsServices = !services(from) && services(to)

  // What else the cluster has, ignoring this node and anything offline.
  const others = nodes.filter((n) => n.id !== node.id && n.status === "online" && n.meshRole !== "mesh")
  const otherBuilders = others.filter((n) => builds(n.meshRole)).length
  const otherRunners = others.filter((n) => services(n.meshRole)).length

  const effects: string[] = []
  if (losesBuilds) effects.push("Build jobs stop being placed here.")
  if (gainsBuilds) effects.push("Build jobs can be placed here.")
  if (losesServices)
    effects.push(
      "A NoSchedule taint goes on, so no new services are placed here. Anything already running keeps running and moves only when it is next scheduled - a redeploy, or a restart."
    )
  if (gainsServices) effects.push("The NoSchedule taint comes off, so services can be placed here again.")

  const warnings: string[] = []
  if (losesBuilds && otherBuilders === 0)
    warnings.push(
      "This is the only node that can build. Deploying from Git will fail until another node takes builds - deploying an existing image still works."
    )
  if (losesServices && otherRunners === 0)
    warnings.push(
      "This is the only node that runs services. Nothing new can be deployed until another node accepts them."
    )

  return (
    <Dialog open onOpenChange={(open) => !open && onCancel()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Change what {node.name} is for?</DialogTitle>
          <DialogDescription>Here is what changes when it does.</DialogDescription>
        </DialogHeader>

        {/* The transition itself, said once and plainly. */}
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{label(from)}</span>
          <ArrowRight className="size-3.5 text-muted-foreground/60" />
          <span className="font-medium text-foreground">{label(to)}</span>
        </div>

        <div className="space-y-3">
          <ul className="space-y-2">
            {effects.map((e) => (
              <li key={e} className="flex items-start gap-2.5 text-xs text-muted-foreground">
                <Check className="mt-0.5 size-3.5 shrink-0 text-primary" />
                <span className="leading-relaxed">{e}</span>
              </li>
            ))}
          </ul>

          {warnings.map((w) => (
            <div
              key={w}
              className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5"
            >
              <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-amber-400" />
              <p className="text-xs leading-relaxed text-amber-300/90">{w}</p>
            </div>
          ))}

          {!node.k8sMember && (
            <p className="text-xs text-muted-foreground">
              This node is not in the cluster yet, so none of this takes effect until it joins.
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button onClick={onConfirm}>Change the role</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
