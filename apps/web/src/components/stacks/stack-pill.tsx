import { Link } from "@tanstack/react-router"
import { Layers } from "lucide-react"
import { useQuery } from "@tanstack/react-query"
import { stacks as stacksApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { cn } from "@/lib/utils"

/**
 * useStackNames maps stack id → name for one project.
 *
 * Resources carry only `stack_id`, so the name is resolved here rather than
 * denormalised onto every resource DTO. The query key matches the stacks list
 * used elsewhere in the project, so the list is fetched once and shared.
 */
export function useStackNames(orgId: string | undefined, projectId: string | undefined) {
  const token = useAuthStore((s) => s.token)!
  const { data: stacks = [] } = useQuery({
    queryKey: ["stacks", orgId, projectId],
    queryFn: () => stacksApi.list(orgId!, projectId!, token),
    enabled: !!orgId && !!projectId,
  })
  return new Map(stacks.map((s) => [s.id, s.name]))
}

/**
 * StackPill marks a resource as belonging to a stack.
 *
 * Stack-owned resources are otherwise indistinguishable from ones created by
 * hand, which matters because they are not independent: a stack apply can
 * recreate or replace them, and a destroy removes them. The pill links to the
 * stack so the spec that governs the resource is one click away.
 *
 * Renders nothing when the resource has no stack, so it can be dropped into a
 * list without every caller repeating the check.
 */
export function StackPill({
  stackId,
  stackNames,
  orgId,
  projectId,
  className,
}: {
  stackId: string | null | undefined
  stackNames: Map<string, string>
  orgId: string | undefined
  projectId: string | undefined
  className?: string
}) {
  if (!stackId) return null
  // The name can be missing while the stacks list is still loading, or if the
  // stack was deleted and the link outlived it. Fall back to a neutral label
  // rather than hiding the fact that the resource came from one.
  const name = stackNames.get(stackId) ?? "stack"

  const pill = (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border border-border/60 bg-muted/40",
        "px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground leading-none",
        "hover:text-foreground hover:border-border transition-colors max-w-[10rem]",
        className
      )}
      title={`From stack: ${name}`}
    >
      <Layers className="h-2.5 w-2.5 shrink-0" />
      <span className="truncate">{name}</span>
    </span>
  )

  if (!orgId || !projectId) return pill
  return (
    <Link
      to="/projects/$id/stacks/$stackId"
      params={{ id: projectId, stackId }}
      onClick={(e) => e.stopPropagation()}
    >
      {pill}
    </Link>
  )
}
