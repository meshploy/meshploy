import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { CopyValue } from "@/components/domains/dns-records"
import { domains as domainsApi, ApiError } from "@/lib/api"
import type { ApiDomain, ApiHookMoveResult, ApiIntegrationRegistration } from "@/lib/api/domains"

const PROVIDER: Record<string, string> = { github: "GitHub", gitlab: "GitLab", gitea: "Gitea", bitbucket: "Bitbucket" }

/**
 * Git provider registrations that still point at a domain being retired.
 *
 * A provider calls the address it was given once - a GitHub App's webhook and
 * callbacks, an OAuth app's redirect, the push hook on each repository. Those
 * live at the provider, so removing the domain does not update them: pushes
 * stop arriving and connecting a repository fails, with nothing said here.
 *
 * Push hooks Meshploy moves itself, through the provider's API. The other two
 * cannot be changed that way - GitHub has no API for an App's callback URLs, and
 * an OAuth app is the operator's own - so the page gives the exact new values,
 * and records the change only when told it was made.
 */
export function ProviderRegistrations({
  registrations,
  domain,
  orgId,
  token,
}: {
  registrations: ApiIntegrationRegistration[]
  domain: ApiDomain
  orgId: string
  token: string
}) {
  if (registrations.length === 0) {
    return <p className="text-xs text-muted-foreground">No git provider calls this domain any more.</p>
  }
  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground leading-relaxed">
        These providers were given addresses on {domain.base_domain}. If it goes before they are changed, pushes stop
        deploying and connecting a repository fails - and nothing here would say why.
      </p>
      {registrations.map((r) => (
        <Registration key={r.integration_id + r.kind} reg={r} domain={domain} orgId={orgId} token={token} />
      ))}
    </div>
  )
}

function Registration({
  reg,
  domain,
  orgId,
  token,
}: {
  reg: ApiIntegrationRegistration
  domain: ApiDomain
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const [result, setResult] = useState<ApiHookMoveResult | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: ["domain-integrations", orgId, domain.id] })

  const move = useMutation({
    mutationFn: () => domainsApi.moveHooks(orgId, reg.integration_id, token),
    onSuccess: (res) => {
      setResult(res)
      refresh()
    },
  })
  const mark = useMutation({
    mutationFn: () => domainsApi.markRegistrationUpdated(orgId, reg.integration_id, { kind: reg.kind, domain_id: domain.id }, token),
    onSuccess: () => {
      setConfirming(false)
      refresh()
    },
  })

  const provider = PROVIDER[reg.provider] ?? reg.provider
  const title =
    reg.kind === "github_app"
      ? `${reg.name}: the GitHub App's URLs`
      : reg.kind === "oauth_redirect"
        ? `${reg.name}: the ${provider} OAuth application's redirect`
        : `${reg.name}: push webhooks on ${reg.repos?.length ?? 0} ${reg.repos?.length === 1 ? "repository" : "repositories"}`
  const where =
    reg.kind === "github_app"
      ? "In GitHub, open the App's settings and change these to the new values."
      : reg.kind === "oauth_redirect"
        ? `In ${provider}, open the OAuth application and change its redirect URI to the new value.`
        : "Meshploy can move these itself: it adds a hook at the new address on each repository, then deletes the old one."

  return (
    <div className="rounded-lg border border-border/60 px-3 py-3 space-y-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-0.5">
          <p className="text-xs font-medium text-foreground">{title}</p>
          <p className="text-xs text-muted-foreground leading-relaxed">{where}</p>
        </div>
        {reg.automatic ? (
          <Button size="sm" variant="outline" className="h-7 text-xs shrink-0" onClick={() => move.mutate()} disabled={move.isPending}>
            {move.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
            Move webhooks
          </Button>
        ) : (
          <Button size="sm" variant="outline" className="h-7 text-xs shrink-0" onClick={() => setConfirming(true)}>
            Mark as updated
          </Button>
        )}
      </div>

      <div className="space-y-2">
        {reg.urls.map((u) => (
          <div key={u.label} className="rounded-md border border-border/50 bg-muted/10 px-3 py-2 text-xs space-y-1">
            <p className="text-[11px] text-muted-foreground">{u.label}</p>
            <p className="font-mono text-muted-foreground/70 break-all line-through decoration-muted-foreground/40">{u.current}</p>
            <CopyValue value={u.new} />
          </div>
        ))}
      </div>

      {reg.repos && reg.repos.length > 0 && (
        <p className="text-[11px] text-muted-foreground">
          <span className="font-mono">{reg.repos.join(", ")}</span>
        </p>
      )}

      {move.error && (
        <p className="text-xs text-destructive">
          {move.error instanceof ApiError ? move.error.message : "Could not move the webhooks."}
        </p>
      )}
      {result && result.failed.length > 0 && (
        <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-amber-400" />
          <div className="space-y-1 text-xs text-amber-300/90">
            <p>
              {result.moved.length} moved; {result.failed.length} could not be. Change{" "}
              {result.failed.length === 1 ? "that hook" : "those hooks"} by hand to the new address above, then mark
              this as updated.
            </p>
            {result.failed.map((f) => (
              <p key={f.repo} className="font-mono text-[11px] break-all">
                {f.repo}: {f.error}
              </p>
            ))}
            <Button size="sm" variant="outline" className="h-7 text-xs mt-1" onClick={() => setConfirming(true)}>
              Mark as updated
            </Button>
          </div>
        </div>
      )}

      <Dialog open={confirming} onOpenChange={(o) => !o && (mark.reset(), setConfirming(false))}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Changed at {provider}?</DialogTitle>
            <DialogDescription>
              Only mark it once the provider has the new address. This records what you did - it cannot check.
              {reg.kind === "oauth_redirect" &&
                " Meshploy starts sending the new redirect URI straight away, and the provider refuses it until it matches."}
            </DialogDescription>
          </DialogHeader>
          {mark.error && (
            <p className="text-xs text-destructive">
              {mark.error instanceof ApiError ? mark.error.message : "Could not record the change."}
            </p>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirming(false)} disabled={mark.isPending}>
              Cancel
            </Button>
            <Button onClick={() => mark.mutate()} disabled={mark.isPending}>
              {mark.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              It has been changed
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
