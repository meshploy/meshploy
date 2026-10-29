import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { domains as domainsApi, projects as projectsApi, ApiError } from "@/lib/api"
import type { ApiDomain } from "@/lib/api/domains"

const EVERY = "__every__"

/**
 * Which projects may route on a domain: every project, or one and its levels.
 * The platform's own domains serve every project's console names, so they
 * cannot be reserved; the server refuses a project whose routes are not the
 * only ones on the domain, and says which to move.
 */
export function ReserveForProject({
  domain,
  orgId,
  token,
  readOnly,
}: {
  domain: ApiDomain
  orgId: string
  token: string
  readOnly: boolean
}) {
  const qc = useQueryClient()
  const { data: projects = [] } = useQuery({
    queryKey: ["projects", orgId, "name"],
    queryFn: () => projectsApi.list(orgId, token, { sort: "name" }),
    enabled: !!orgId,
  })
  const change = useMutation({
    mutationFn: (projectId: string | null) => domainsApi.setProject(orgId, domain.id, projectId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domain", orgId, domain.id] })
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
    },
  })
  const platform = domain.is_primary || !!domain.former_primary
  const current = domain.project_id ?? EVERY
  const name = (id: string) => projects.find((p) => p.id === id)?.name ?? "a project"

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground leading-relaxed">
        {platform
          ? "The platform is served on this domain, so every project may use it."
          : domain.project_id
            ? `Only ${name(domain.project_id)} and its levels may put routes on this domain; no other project is offered it.`
            : "Every project may put routes on this domain. Reserve it for one to keep the others off it."}
      </p>
      {!platform && (
        <Select
          value={current}
          onValueChange={(v) => {
            const next = !v || v === EVERY ? null : v
            if (next !== (domain.project_id ?? null)) change.mutate(next)
          }}
          disabled={readOnly || change.isPending}
        >
          <SelectTrigger className="w-full! sm:w-80! h-9 text-sm bg-background border-border/60">
            <SelectValue>{current === EVERY ? "Every project" : name(current)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={EVERY}>Every project</SelectItem>
            {projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {change.isPending && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" /> Saving…
        </p>
      )}
      {change.error && (
        <p className="text-xs text-destructive">
          {change.error instanceof ApiError ? change.error.message : "Could not change who may use this domain."}
        </p>
      )}
    </div>
  )
}
