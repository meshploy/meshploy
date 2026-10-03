import { Loader2, Network } from "lucide-react"
import { Switch } from "@/components/ui/switch"
import type { MemberReach, ReachPort } from "@/lib/api"

function where(p: ReachPort) {
  return p.on === "gateway" ? `port ${p.mesh_port} on the gateway` : `port ${p.mesh_port} on the cluster's machines`
}

/**
 * Under a member on an Access page: whether their own machines reach this on
 * the mesh, and exactly what that opens. Seeing it in the console and
 * connecting to it from a laptop are separate.
 */
export function MeshReachLine({ reach, pending, onChange }: { reach: MemberReach; pending: boolean; onChange: (reach: boolean) => void }) {
  const listening = reach.services.filter((s) => s.ports.length > 0 || (s.routes ?? []).length > 0)
  const databaseDefault = !reach.chosen && reach.services.some((s) => s.database && !s.reach)
  const machines = reach.machines.length === 0 ? "Their machines" : reach.machines.join(", ")

  let text: string
  if (listening.length === 0) {
    text = "Nothing here has a port or an internal route on the mesh, so there is nothing to reach."
  } else if (!reach.reach) {
    text = `${machines} cannot connect to it on the mesh.${databaseDefault ? " Databases stay off until you switch this on." : ""}`
  } else {
    text = `${machines} can connect to ${listening.map((s) => `${s.name} ${[...s.ports.map((p) => `${p.port} (${where(p)})`), ...(s.routes ?? [])].join(", ")}`).join("; ")}.`
  }

  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/40 bg-muted/10 px-3 py-2.5">
      <Network className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium">From their machines</p>
        <p className="text-xs text-muted-foreground leading-relaxed">{text}</p>
        {reach.machines.length === 0 && listening.length > 0 && (
          <p className="text-[11px] text-muted-foreground/70">They have no machine on the mesh yet; this applies once they join one.</p>
        )}
      </div>
      {pending && <Loader2 className="mt-0.5 size-3.5 animate-spin text-muted-foreground" />}
      <Switch aria-label="Reach from their machines" checked={reach.reach} disabled={pending || listening.length === 0} onCheckedChange={(v) => onChange(Boolean(v))} />
    </div>
  )
}
