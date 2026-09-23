import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ArrowRight, Check, Loader2, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { ResourcePanel } from "@/components/layout/resource-workbench"
import { domains as domainsApi, ApiError } from "@/lib/api"
import type { ApiDomain } from "@/lib/api/domains"

/**
 * Moving the primary to this domain.
 *
 * It moves a pointer, not the platform, and the confirmation has to say so or
 * it reads as a cutover. The current primary keeps serving its console, api and
 * headscale names: the console being clicked in right now is very likely one of
 * them, and every worker joined the mesh through the other. The new names start
 * answering beside the old ones; the old ones stop only when their domain is
 * retired and removed.
 */
export function MakePrimarySection({
  domain,
  current,
  orgId,
  token,
}: {
  domain: ApiDomain
  /** The domain that is primary now, if the list has loaded. */
  current?: ApiDomain
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const make = useMutation({
    mutationFn: () => domainsApi.makePrimary(orgId, domain.id, token),
    onSuccess: () => {
      setConfirming(false)
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
      qc.invalidateQueries({ queryKey: ["domain", orgId] })
    },
  })
  const d = domain.base_domain
  const was = current?.base_domain

  return (
    <ResourcePanel
      title="Make this the primary domain"
      action={
        <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setConfirming(true)}>
          Make primary
        </Button>
      }
    >
      <p className="text-xs text-muted-foreground leading-relaxed">
        The console, the API and the mesh control plane start answering here as well, new routes default to this domain,
        and new machines join through it. Nothing stops serving on {was ?? "the current primary"}.
      </p>

      <Dialog open={confirming} onOpenChange={(o) => !o && (make.reset(), setConfirming(false))}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Make {d} the primary domain?</DialogTitle>
            <DialogDescription>It moves which domain is primary. It does not move anything off the old one.</DialogDescription>
          </DialogHeader>

          {was && (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-mono text-muted-foreground">{was}</span>
              <ArrowRight className="size-3.5 text-muted-foreground/60" />
              <span className="font-mono font-medium text-foreground">{d}</span>
            </div>
          )}

          <ul className="space-y-2">
            {[
              `console.${d}, api.${d} and headscale.${d} start answering. ${
                domain.dns_mode === "delegation"
                  ? "Their records are in the zone this gateway serves, and the wildcard certificate covers them."
                  : "Each gets its certificate on first visit, through the wildcard A record."
              }`,
              "New routes default to this domain, and new machines join the mesh through its headscale.",
              was
                ? `${was} keeps serving its console, API and headscale names - this page included - and every node that joined through it keeps working.`
                : "The current primary keeps serving its names.",
            ].map((e) => (
              <li key={e} className="flex items-start gap-2.5 text-xs text-muted-foreground">
                <Check className="mt-0.5 size-3.5 shrink-0 text-primary" />
                <span className="leading-relaxed">{e}</span>
              </li>
            ))}
          </ul>

          <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-amber-400" />
            <p className="text-xs leading-relaxed text-amber-300/90">
              The mesh control plane restarts to take the new name, which pauses joining for a moment. Traffic between
              nodes is not interrupted. {was ? `Removing ${was} later means moving its nodes first - its page will list them.` : ""}
            </p>
          </div>

          {make.error && (
            <p className="text-xs text-destructive">
              {make.error instanceof ApiError ? make.error.message : "Could not make this the primary domain."}
            </p>
          )}

          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirming(false)} disabled={make.isPending}>
              Cancel
            </Button>
            <Button onClick={() => make.mutate()} disabled={make.isPending}>
              {make.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Make primary
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ResourcePanel>
  )
}
