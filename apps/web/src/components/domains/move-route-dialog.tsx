import { useState } from "react"
import { useMutation } from "@tanstack/react-query"
import { ArrowRight, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { cn } from "@/lib/utils"
import { routes as routesApi, ApiError } from "@/lib/api"
import { usableForNewRoutes, type ApiDomain, type ApiDomainRoute } from "@/lib/api/domains"

/** The hostname a subdomain has in a zone of a base domain - the same shape the
 *  API builds. */
export function hostnameOn(route: ApiDomainRoute, d: ApiDomain): string {
  if (route.zone === "internal") return `${route.subdomain}.${d.internal_subdomain}.${d.base_domain}`
  if (route.zone === "preview") return `${route.subdomain}.${d.preview_subdomain}.${d.base_domain}`
  return `${route.subdomain}.${d.base_domain}`
}

/**
 * Moving a route off a domain that is being retired.
 *
 * The route keeps its subdomain, zone and targets; only the domain under it
 * changes. The redirect is the part worth deciding on: links to the old name
 * are out there, and a redirect keeps them working - but it is a route on the
 * old domain, so it holds the domain until it is deleted. That is a grace
 * period, and the page says so rather than letting it look like the move did
 * not finish.
 */
export function MoveRouteDialog({
  route,
  from,
  domains,
  orgId,
  token,
  onMoved,
  onCancel,
}: {
  route: ApiDomainRoute
  from: ApiDomain
  domains: ApiDomain[]
  orgId: string
  token: string
  onMoved: () => void
  onCancel: () => void
}) {
  const targets = domains.filter((d) => d.id !== from.id && usableForNewRoutes(d))
  const [to, setTo] = useState(targets[0]?.id ?? "")
  // Redirects are an HTTP answer from the public edge.
  const canRedirect = route.zone === "public"
  const [keepRedirect, setKeepRedirect] = useState(canRedirect)
  const target = targets.find((d) => d.id === to)

  const move = useMutation({
    mutationFn: () =>
      routesApi.move(orgId, route.project_id, route.id, { domain_id: to, keep_redirect: canRedirect && keepRedirect }, token),
    onSuccess: onMoved,
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Move {route.hostname}</DialogTitle>
          <DialogDescription>
            It keeps its subdomain, zone and targets. Only the domain under it changes.
          </DialogDescription>
        </DialogHeader>

        {targets.length === 0 ? (
          <p className="text-xs text-muted-foreground leading-relaxed">
            There is no other verified base domain to move it to. Add one first, or delete this route if it is no longer
            needed.
          </p>
        ) : (
          <div className="space-y-4">
            <div className="space-y-2" role="radiogroup" aria-label="Move to">
              {targets.map((d) => {
                const selected = d.id === to
                return (
                  <button
                    key={d.id}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    onClick={() => setTo(d.id)}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
                      selected ? "border-primary bg-primary/5" : "border-border/60 bg-card hover:border-border"
                    )}
                  >
                    <span
                      className={cn(
                        "size-3.5 shrink-0 rounded-full border-2",
                        selected ? "border-primary bg-primary/30" : "border-border"
                      )}
                    />
                    <span className="font-mono text-xs text-foreground">{d.base_domain}</span>
                    {d.is_primary && <span className="text-[11px] text-muted-foreground">primary</span>}
                  </button>
                )
              })}
            </div>

            {target && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="font-mono text-muted-foreground break-all">{route.hostname}</span>
                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/60" />
                <span className="font-mono text-foreground break-all">{hostnameOn(route, target)}</span>
              </div>
            )}

            {canRedirect ? (
              <label className="flex items-start gap-2.5 rounded-lg border border-border/60 px-3 py-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={keepRedirect}
                  onChange={(e) => setKeepRedirect(e.target.checked)}
                  className="mt-0.5 accent-primary"
                />
                <span className="space-y-0.5">
                  <span className="block text-xs font-medium text-foreground">Keep the old name redirecting</span>
                  <span className="block text-xs text-muted-foreground leading-relaxed">
                    {route.hostname} answers with a 301 to the new name, so existing links keep working. It stays on
                    this domain until you delete it.
                  </span>
                </span>
              </label>
            ) : (
              <p className="text-xs text-muted-foreground leading-relaxed">
                An {route.zone} route cannot leave a redirect behind, so the old name stops answering when it moves.
              </p>
            )}

            {move.error && (
              <p className="text-xs text-destructive">
                {move.error instanceof ApiError ? move.error.message : "Could not move the route."}
              </p>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={move.isPending}>
            Cancel
          </Button>
          {targets.length > 0 && (
            <Button onClick={() => move.mutate()} disabled={!to || move.isPending}>
              {move.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Move route
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
