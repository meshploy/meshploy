import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Loader2, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { CopyValue } from "@/components/domains/dns-records"
import { domains as domainsApi, ApiError } from "@/lib/api"
import type { ApiControlNode, ApiDomain } from "@/lib/api/domains"

/**
 * The machines whose control connection still goes through a former primary's
 * headscale name.
 *
 * This is the one blocker the console cannot clear by itself. The API has no
 * way into a worker, and moving a Tailscale client to another control URL means
 * re-authenticating it there - a step for the operator to run, deliberately,
 * on one node first. So the page gives the command, and records that it was run
 * only when told. "Mark as moved" says what the operator did; it does not do it,
 * and it cannot check it.
 */
export function ControlNodes({
  nodes,
  domain,
  primary,
  orgId,
  token,
}: {
  nodes: ApiControlNode[]
  domain: ApiDomain
  primary?: ApiDomain
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [marking, setMarking] = useState<ApiControlNode | null>(null)
  const mark = useMutation({
    mutationFn: (n: ApiControlNode) => domainsApi.markNodeMoved(orgId, n.id, token),
    onSuccess: () => {
      setMarking(null)
      qc.invalidateQueries({ queryKey: ["domain-nodes", orgId, domain.id] })
    },
  })
  const target = primary ? `https://headscale.${primary.base_domain}` : "https://headscale.<primary domain>"

  if (nodes.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        No node reaches the mesh through headscale.{domain.base_domain} any more.
      </p>
    )
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground leading-relaxed">
        These joined the mesh through <span className="font-mono">headscale.{domain.base_domain}</span>, and drop off
        it if that name goes. Move each to the primary&apos;s headscale by running this on the node, with a pre-auth key
        from the{" "}
        <Link to="/cluster" className="underline hover:text-foreground">
          Cluster
        </Link>{" "}
        page:
      </p>
      <div className="rounded-md border border-border/60 bg-muted/10 px-3 py-2.5 text-xs">
        <CopyValue value={`sudo tailscale up --login-server=${target} --force-reauth --authkey=<pre-auth key>`} />
      </div>
      <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
        <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-amber-400" />
        <p className="text-xs leading-relaxed text-amber-300/90">
          Move one node first and check it kept its mesh address (<span className="font-mono">tailscale ip -4</span>)
          before the rest: a node that comes back on a different address loses its place in the cluster.
        </p>
      </div>

      <div className="divide-y divide-border/50 rounded-md border border-border/60">
        {nodes.map((n) => (
          <div key={n.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
            <div className="min-w-0">
              <Link to="/nodes/$id" params={{ id: n.id }} className="text-xs font-medium text-foreground hover:underline">
                {n.name}
              </Link>
              <p className="font-mono text-[11px] text-muted-foreground">{n.tailscale_ip}</p>
            </div>
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setMarking(n)}>
              Mark as moved
            </Button>
          </div>
        ))}
      </div>

      <Dialog open={!!marking} onOpenChange={(o) => !o && (mark.reset(), setMarking(null))}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Has {marking?.name} been moved?</DialogTitle>
            <DialogDescription>
              Only mark a node you have moved. This records what you did on the machine - it does not move it, and it
              cannot check. A node marked by mistake drops off the mesh when {domain.base_domain} is removed.
            </DialogDescription>
          </DialogHeader>
          {mark.error && (
            <p className="text-xs text-destructive">
              {mark.error instanceof ApiError ? mark.error.message : "Could not record the move."}
            </p>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setMarking(null)} disabled={mark.isPending}>
              Cancel
            </Button>
            <Button onClick={() => marking && mark.mutate(marking)} disabled={mark.isPending}>
              {mark.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              It has been moved
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
