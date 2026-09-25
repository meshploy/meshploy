import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, Pause, Play } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { routes as routesApi, tcpRoutes as tcpRoutesApi } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

/** Published or paused, the same way for a hostname and a TCP port. */
export function PublishStateBadge({ published, awaitingDeploy, className }: { published: boolean; awaitingDeploy?: boolean; className?: string }) {
  return (
    <Badge
      className={cn(
        "text-[11px] px-1.5 py-0 h-4.5 border",
        published
          ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/20"
          : "bg-muted text-muted-foreground border-border",
        className
      )}
    >
      {/* A route copied into an environment level waits for its service's
          first deploy there, which publishes it: not paused by anyone. */}
      {published ? "published" : awaitingDeploy ? "waits for first deploy" : "paused"}
    </Badge>
  )
}

/**
 * Publish or pause a route. Pausing takes something live off the air, so it
 * asks first; publishing does not.
 */
export function PublishToggle({ kind, routeId, projectId, published, label }: {
  kind: "http" | "tcp"
  routeId: string
  projectId: string
  published: boolean
  /** What is being paused, for the confirmation: a hostname or ":port". */
  label: string
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)

  const api = kind === "http" ? routesApi : tcpRoutesApi
  const change = useMutation({
    mutationFn: async (publish: boolean): Promise<unknown> =>
      publish ? api.publish(orgId, projectId, routeId, token) : api.pause(orgId, projectId, routeId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [kind === "http" ? "routes" : "tcp-routes", orgId, projectId] })
      qc.invalidateQueries({ queryKey: [kind === "http" ? "route" : "tcp-route", orgId, projectId, routeId] })
      setConfirming(false)
    },
  })

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="gap-1.5"
        disabled={change.isPending}
        onClick={(e) => {
          e.stopPropagation()
          if (published) setConfirming(true)
          else change.mutate(true)
        }}
      >
        {change.isPending && !confirming
          ? <Loader2 className="size-3 animate-spin" />
          : published ? <Pause className="size-3" /> : <Play className="size-3" />}
        {published ? "Pause" : "Publish"}
      </Button>
      <Dialog open={confirming} onOpenChange={(open) => { if (!change.isPending) setConfirming(open) }}>
        <DialogContent onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>Pause {label}?</DialogTitle>
            <DialogDescription>
              {kind === "http"
                ? "Visitors get a 404 and no certificate is issued until you publish it again. The route and its paths are kept."
                : "The gateway closes the port and refuses connections until you publish it again. The route is kept."}
              {" "}Takes effect within 30 seconds.
            </DialogDescription>
          </DialogHeader>
          {change.isError && <p className="text-sm text-destructive">{(change.error as Error).message}</p>}
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={change.isPending} onClick={() => setConfirming(false)}>Cancel</Button>
            <Button variant="destructive" disabled={change.isPending} onClick={() => change.mutate(false)}>
              {change.isPending && <Loader2 className="size-3 animate-spin" />}Pause
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
