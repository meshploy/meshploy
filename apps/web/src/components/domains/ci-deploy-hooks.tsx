import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { formatRelativeTime } from "@/lib/utils"
import { domains as domainsApi, ApiError } from "@/lib/api"
import type { ApiDeployHook, ApiDomain } from "@/lib/api/domains"

/**
 * CI jobs that still deploy through a domain being retired.
 *
 * A deploy webhook is a URL someone pasted into their CI; Meshploy cannot see
 * or change it there. What it sees is where each call arrives, so this lists
 * services whose webhook was last called through this domain. Updating the URL
 * in the job is the fix, and there is nothing to mark: the next call arrives
 * through the new name and the row goes by itself.
 *
 * The one exception is a job that no longer exists. It will never call again
 * to clear itself, so it can be forgotten - and if it was not dead after all,
 * its next call puts it straight back.
 */
export function CIDeployHooks({
  hooks,
  domain,
  orgId,
  token,
}: {
  hooks: ApiDeployHook[]
  domain: ApiDomain
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [forgetting, setForgetting] = useState<ApiDeployHook | null>(null)
  const forget = useMutation({
    mutationFn: (h: ApiDeployHook) => domainsApi.forgetDeployHookCall(orgId, h.service_id, token),
    onSuccess: () => {
      setForgetting(null)
      qc.invalidateQueries({ queryKey: ["domain-deploy-hooks", orgId, domain.id] })
    },
  })

  if (hooks.length === 0) {
    return <p className="text-xs text-muted-foreground">No CI job deploys through {domain.base_domain}.</p>
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground leading-relaxed">
        These services were last deployed by a CI job calling their webhook through {domain.base_domain}. Copy the new
        URL from the service&apos;s build settings into the job; its next deploy arrives through the new name, and the
        row goes by itself.
      </p>
      <div className="divide-y divide-border/50 rounded-md border border-border/60">
        {hooks.map((h) => (
          <div key={h.service_id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
            <div className="min-w-0">
              <Link
                to="/projects/$id/services/$serviceId/config"
                params={{ id: h.project_id, serviceId: h.service_id }}
                className="text-xs font-medium text-foreground hover:underline"
              >
                {h.service_name}
              </Link>
              <p className="text-[11px] text-muted-foreground">
                {h.project_name} · last called through <span className="font-mono">{h.host}</span>
                {h.called_at ? ` ${formatRelativeTime(new Date(h.called_at))}` : ""}
              </p>
            </div>
            <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setForgetting(h)}>
              The job is gone
            </Button>
          </div>
        ))}
      </div>

      <Dialog open={!!forgetting} onOpenChange={(o) => !o && (forget.reset(), setForgetting(null))}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Forget {forgetting?.service_name}&apos;s last caller?</DialogTitle>
            <DialogDescription>
              For a CI job that no longer exists, and so will never call again to clear itself. If it does still exist,
              its next call puts it straight back here - and if {domain.base_domain} has been removed by then, that
              deploy fails.
            </DialogDescription>
          </DialogHeader>
          {forget.error && (
            <p className="text-xs text-destructive">
              {forget.error instanceof ApiError ? forget.error.message : "Could not forget it."}
            </p>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setForgetting(null)} disabled={forget.isPending}>
              Cancel
            </Button>
            <Button onClick={() => forgetting && forget.mutate(forgetting)} disabled={forget.isPending}>
              {forget.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Forget it
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
