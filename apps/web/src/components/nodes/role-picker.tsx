import { cn } from "@/lib/utils"
import type { MeshRole } from "@/types"

/**
 * What a node is for, and the one place it is described.
 *
 * Two screens set this - the cluster page when minting a provisioning token,
 * and a node's own page when changing it later - and they used to disagree
 * about what the options were called. "Worker + Builder" on one and "Services
 * and builds" on the other is the same setting wearing two names, which leaves
 * the reader to work out that they match.
 *
 * The shape is the one a database's network access uses: the choices side by
 * side with what each means, rather than four words and a line of prose that
 * changes under them. It matters more here than it looks, because these are not
 * synonyms - "builds are kept off it" and "nothing of yours is scheduled here"
 * are different promises, and a picker that shows one description at a time
 * makes them impossible to compare.
 */
export const NODE_ROLES: { value: MeshRole; label: string; detail: string }[] = [
  {
    value: "workload_builder",
    label: "Services and builds",
    detail: "The default. Runs your applications and the build jobs that produce them.",
  },
  {
    value: "workload",
    label: "Services only",
    detail: "Runs applications. Builds are kept off it, so a build cannot crowd out what is serving.",
  },
  {
    value: "builder",
    label: "Builds only",
    detail: "Tainted, so nothing of yours is ever scheduled here. For a machine bought to compile.",
  },
  {
    value: "mesh",
    label: "Mesh only",
    detail: "Joins the WireGuard mesh but not the cluster. Nothing runs on it; routes can still reach its ports.",
  },
]

/** The roles a node already in the cluster can be moved between. Mesh-only is
 *  absent on purpose: leaving the cluster is not a toggle, it means uninstalling
 *  K3s on the machine. */
export const CLUSTER_ROLES = NODE_ROLES.filter((r) => r.value !== "mesh")

export function RolePicker({
  value,
  onChange,
  options = NODE_ROLES,
  busy,
  label = "What this node is for",
}: {
  value: MeshRole
  onChange: (role: MeshRole) => void
  options?: typeof NODE_ROLES
  /** The role a change is in flight for, so the row it is on can say so. */
  busy?: MeshRole | null
  label?: string
}) {
  return (
    <div className="space-y-2" role="radiogroup" aria-label={label}>
      {options.map((r) => {
        const selected = value === r.value
        const pending = busy === r.value
        return (
          <button
            key={r.value}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={!!busy}
            onClick={() => onChange(r.value)}
            className={cn(
              "flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
              selected ? "border-primary bg-primary/5" : "border-border/60 bg-card",
              !selected && !busy && "hover:border-border hover:bg-muted/20",
              busy && "opacity-60 cursor-wait"
            )}
          >
            <span
              className={cn(
                "mt-0.5 size-3.5 shrink-0 rounded-full border-2",
                selected ? "border-primary bg-primary/30" : "border-border",
                pending && "animate-pulse"
              )}
            />
            <span className="min-w-0 space-y-0.5">
              <span className="block text-xs font-medium text-foreground">{r.label}</span>
              <span className="block text-xs text-muted-foreground">{r.detail}</span>
            </span>
          </button>
        )
      })}
    </div>
  )
}
