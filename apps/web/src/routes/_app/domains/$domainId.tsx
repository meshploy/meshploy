import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, ArrowRight, Check, Loader2, RefreshCw, TriangleAlert } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { ResourcePanel } from "@/components/layout/resource-workbench"
import { DnsModePicker, dnsModeLabel } from "@/components/domains/dns-mode-picker"
import { DnsRecords } from "@/components/domains/dns-records"
import { MoveRouteDialog } from "@/components/domains/move-route-dialog"
import { MakePrimarySection } from "@/components/domains/make-primary"
import { ControlNodes } from "@/components/domains/control-nodes"
import { ProviderRegistrations } from "@/components/domains/provider-registrations"
import { CIDeployHooks } from "@/components/domains/ci-deploy-hooks"
import { useGatewayPublicIp } from "@/components/domains/use-gateway-ip"
import { domains as domainsApi, routes as routesApi, ApiError } from "@/lib/api"
import type { ApiDomain, ApiDomainRoute, DnsMode } from "@/lib/api/domains"
import { useAuthStore } from "@/store/auth-store"
import { TermInfo } from "@/help/term"
import { useOrgStore, useIsAdmin } from "@/store/org-store"

export const Route = createFileRoute("/_app/domains/$domainId")({
  component: DomainPage,
})

/** The platform's own hostnames, reserved on every base domain and served on
 *  the primary alone. */
const PLATFORM_SUBDOMAINS = [
  { name: "console", what: "The console" },
  { name: "api", what: "The API" },
  { name: "headscale", what: "The mesh control plane nodes join through" },
]

function DomainPage() {
  const { domainId } = Route.useParams()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const isAdmin = useIsAdmin()

  const { data: domain, isLoading, error } = useQuery({
    queryKey: ["domain", orgId, domainId],
    queryFn: () => domainsApi.get(orgId, domainId, token),
    enabled: !!orgId,
  })
  const { data: routes = [] } = useQuery({
    queryKey: ["domain-routes", orgId, domainId],
    queryFn: () => domainsApi.routes(orgId, domainId, token),
    enabled: !!orgId,
  })
  const publicIp = useGatewayPublicIp(orgId, token)
  const { data: allDomains = [] } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })
  const primary = allDomains.find((d) => d.is_primary)
  // Only a domain that is or was primary ever had machines join through it.
  const { data: controlNodes = [] } = useQuery({
    queryKey: ["domain-nodes", orgId, domainId],
    queryFn: () => domainsApi.nodes(orgId, domainId, token),
    enabled: !!orgId && isAdmin && !!domain && (domain.is_primary || !!domain.former_primary),
  })
  // CI jobs deploying through this domain's names, as observed.
  const { data: deployHooks = [] } = useQuery({
    queryKey: ["domain-deploy-hooks", orgId, domainId],
    queryFn: () => domainsApi.deployHooks(orgId, domainId, token),
    enabled: !!orgId && isAdmin && !!domain && (domain.is_primary || !!domain.former_primary),
  })
  // And the providers told to call this domain's api. and console. names.
  const { data: registrations = [] } = useQuery({
    queryKey: ["domain-integrations", orgId, domainId],
    queryFn: () => domainsApi.integrations(orgId, domainId, token),
    enabled: !!orgId && isAdmin && !!domain && (domain.is_primary || !!domain.former_primary),
  })

  if (isLoading) {
    return (
      <div className="console-page flex items-center gap-2 text-muted-foreground text-sm">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>Loading…</span>
      </div>
    )
  }
  if (error || !domain) {
    return (
      <div className="console-page space-y-3">
        <BackLink />
        <p className="text-sm text-muted-foreground">This domain does not exist, or is not in this organisation.</p>
      </div>
    )
  }

  return (
    <div className="console-page space-y-6">
      <div className="space-y-3">
        <BackLink />
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-xl font-semibold tracking-tight font-mono break-all">{domain.base_domain}</h1>
          {domain.is_primary && <Badge variant="secondary">Primary</Badge>}
          {domain.is_primary && <TermInfo id="domains.primary" />}
          {domain.former_primary && <TermInfo id="domains.former-primary" />}
          {!domain.verified && (
            <Badge variant="outline" className="border-amber-500/40 text-amber-400">
              Not verified
            </Badge>
          )}
          {domain.former_primary && <Badge variant="secondary">Former primary</Badge>}
          {domain.retiring_at && (
            <Badge variant="outline" className="border-amber-500/40 text-amber-400">
              Retiring
            </Badge>
          )}
        </div>
        <p className="text-sm text-muted-foreground">
          {dnsModeLabel(domain.dns_mode)} · {routes.length} {routes.length === 1 ? "route" : "routes"}
        </p>
      </div>

      {domain.retiring_at && (
        <RetiringBanner domain={domain} orgId={orgId} token={token} canChange={isAdmin} />
      )}
      {/* While retiring, what still holds the domain is the work in hand, so it
          leads the page. */}
      {domain.retiring_at && (
        <ResourcePanel title="Still on this domain">
          <RoutesOnDomain rows={routes} domain={domain} retiring canChange={isAdmin} orgId={orgId} token={token} />
        </ResourcePanel>
      )}
      {domain.retiring_at && domain.former_primary && isAdmin && (
        <ResourcePanel title="Nodes joined through this domain">
          <ControlNodes nodes={controlNodes} domain={domain} primary={primary} orgId={orgId} token={token} />
        </ResourcePanel>
      )}
      {domain.retiring_at && domain.former_primary && isAdmin && (
        <ResourcePanel title="CI jobs deploying through this domain">
          <CIDeployHooks hooks={deployHooks} domain={domain} orgId={orgId} token={token} />
        </ResourcePanel>
      )}
      {domain.retiring_at && domain.former_primary && isAdmin && (
        <ResourcePanel title="Git providers calling this domain">
          <ProviderRegistrations registrations={registrations} domain={domain} orgId={orgId} token={token} />
        </ResourcePanel>
      )}

      <ResourcePanel
        title="DNS records"
        action={!domain.verified && isAdmin ? <VerifyButton domain={domain} orgId={orgId} token={token} /> : undefined}
      >
        <DnsRecords
          domain={domain.base_domain}
          mode={domain.dns_mode}
          verified={domain.verified}
          verifyToken={domain.verify_token}
          publicIp={publicIp}
        />
      </ResourcePanel>

      <ResourcePanel title="DNS mode" action={<TermInfo id={domain.dns_mode === "ondemand" ? "domains.on-demand" : "domains.delegation"} />}>
        <DnsModeSection domain={domain} orgId={orgId} token={token} readOnly={!isAdmin} />
      </ResourcePanel>

      <ResourcePanel title="Platform subdomains">
        <PlatformSubdomains domain={domain} />
      </ResourcePanel>

      {!domain.retiring_at && (
        <ResourcePanel title="Routes">
          <RoutesOnDomain rows={routes} domain={domain} retiring={false} canChange={isAdmin} orgId={orgId} token={token} />
        </ResourcePanel>
      )}

      {isAdmin && !domain.is_primary && domain.verified && !domain.retiring_at && (
        <MakePrimarySection domain={domain} current={primary} orgId={orgId} token={token} />
      )}

      {isAdmin && !domain.is_primary && (
        <LifecycleSection
          domain={domain}
          routeCount={routes.length}
          nodeCount={controlNodes.length}
          integrationCount={registrations.length}
          deployHookCount={deployHooks.length}
          orgId={orgId}
          token={token}
        />
      )}
    </div>
  )
}

/**
 * Said at the top, because "retiring" sounds like something has been switched
 * off. It has not: only new routes are refused. Reversible from here too -
 * people press Start retiring to see what is holding a domain, which is a fair
 * way to find out, and they need the way back to be as easy.
 */
function RetiringBanner({
  domain,
  orgId,
  token,
  canChange,
}: {
  domain: ApiDomain
  orgId: string
  token: string
  canChange: boolean
}) {
  const qc = useQueryClient()
  const stop = useMutation({
    mutationFn: () => domainsApi.stopRetiring(orgId, domain.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domain", orgId, domain.id] })
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
    },
  })
  const since = domain.retiring_at ? new Date(domain.retiring_at) : null
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3">
      <div className="space-y-1 min-w-0">
        <p className="text-xs font-medium text-amber-400">
          Retiring{since ? ` since ${since.toLocaleDateString(undefined, { day: "numeric", month: "long", year: "numeric" })}` : ""}
        </p>
        <p className="text-[11px] leading-relaxed text-muted-foreground/80">
          New routes can no longer use this domain. Everything already on it keeps serving, and its certificates keep
          renewing, until it is removed.
        </p>
      </div>
      {canChange && (
        <Button size="sm" variant="outline" className="h-7 text-xs shrink-0" onClick={() => stop.mutate()} disabled={stop.isPending}>
          {stop.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
          Stop retiring
        </Button>
      )}
    </div>
  )
}

function BackLink() {
  return (
    <Link to="/domains" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
      <ArrowLeft className="h-3.5 w-3.5" />
      Domains
    </Link>
  )
}

function VerifyButton({ domain, orgId, token }: { domain: ApiDomain; orgId: string; token: string }) {
  const qc = useQueryClient()
  const verify = useMutation({
    mutationFn: () => domainsApi.verify(orgId, domain.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domain", orgId, domain.id] })
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
    },
  })
  return (
    // The result sits beside the button rather than under it, so a failed
    // check does not push the panel's header taller. A span, not a p: the panel
    // header styles every paragraph in it as its subtitle.
    <div className="flex flex-1 items-center justify-end gap-3 min-w-0">
      {verify.error && (
        <span
              className="min-w-0 truncate text-right text-[11px] text-amber-400"
              role="status"
              title={`${(verify.error instanceof ApiError ? verify.error.message : "Not found yet").replace(/\.$/, "")}. Try again in a few minutes.`}
            >
              {/* A phone has room for the gist, not the sentence. */}
              <span className="sm:hidden">Not found yet</span>
              <span className="hidden sm:inline">
                {(verify.error instanceof ApiError ? verify.error.message : "Not found yet").replace(/\.$/, "")}. Try again in a few minutes.
              </span>
            </span>
      )}
      <Button
        size="sm"
        variant="outline"
        className="gap-1.5 h-7 text-xs shrink-0"
        onClick={() => verify.mutate()}
        disabled={verify.isPending}
      >
        {verify.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
        Check now
      </Button>
    </div>
  )
}

/**
 * Switching modes reconfigures the edge, so it is confirmed with what the
 * switch actually does - not "are you sure", which tells nobody anything. The
 * part people get wrong is that the DNS change is theirs to make: the console
 * switches what this gateway expects, and the records at the provider have to
 * follow, or the domain stops resolving here.
 */
function DnsModeSection({
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
  const [pending, setPending] = useState<DnsMode | null>(null)
  const change = useMutation({
    mutationFn: (mode: DnsMode) => domainsApi.setDnsMode(orgId, domain.id, mode, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domain", orgId, domain.id] })
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
      setPending(null)
    },
  })

  return (
    <>
      <DnsModePicker
        value={domain.dns_mode}
        onChange={(m) => m !== domain.dns_mode && setPending(m)}
        disabled={readOnly || change.isPending}
      />
      {pending && (
        <ModeChangeDialog
          domain={domain}
          to={pending}
          busy={change.isPending}
          error={change.error}
          onConfirm={() => change.mutate(pending)}
          onCancel={() => {
            change.reset()
            setPending(null)
          }}
        />
      )}
    </>
  )
}

function ModeChangeDialog({
  domain,
  to,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  domain: ApiDomain
  to: DnsMode
  busy: boolean
  error: Error | null
  onConfirm: () => void
  onCancel: () => void
}) {
  const toOnDemand = to === "ondemand"
  const effects = toOnDemand
    ? [
        "This gateway stops answering DNS for the domain, so it no longer needs the NS record.",
        "Public routes get certificates one hostname at a time, as each is first visited.",
      ]
    : [
        "This gateway starts answering DNS for the domain, and obtains one wildcard certificate for it.",
        "Internal routes get a trusted certificate instead of one signed by this gateway.",
      ]
  const yourPart = toOnDemand
    ? `At your provider, replace the NS record with A records for ${domain.base_domain} and *.${domain.base_domain} pointing at this gateway.`
    : `At the parent zone or registrar, point the NS record for ${domain.base_domain} at ns1.${domain.base_domain}, with its glue record.`
  const warning = toOnDemand
    ? "Internal routes on this domain will use a certificate from this gateway's own authority. Traffic is still encrypted, but clients warn until that authority is trusted."
    : "Until the NS record points here, nothing on this domain resolves to this gateway."

  return (
    <Dialog open onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Switch {domain.base_domain} to {dnsModeLabel(to)}?</DialogTitle>
          <DialogDescription>Here is what changes, and what is yours to change.</DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{dnsModeLabel(domain.dns_mode)}</span>
          <ArrowRight className="size-3.5 text-muted-foreground/60" />
          <span className="font-medium text-foreground">{dnsModeLabel(to)}</span>
        </div>

        <div className="space-y-3">
          <ul className="space-y-2">
            {effects.map((e) => (
              <li key={e} className="flex items-start gap-2.5 text-xs text-muted-foreground">
                <Check className="mt-0.5 size-3.5 shrink-0 text-primary" />
                <span className="leading-relaxed">{e}</span>
              </li>
            ))}
          </ul>
          <div className="rounded-lg border border-border/60 bg-muted/20 px-3 py-2.5">
            <p className="text-xs font-medium text-foreground">Yours to do</p>
            <p className="mt-0.5 text-xs text-muted-foreground leading-relaxed">{yourPart}</p>
          </div>
          <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-amber-400" />
            <p className="text-xs leading-relaxed text-amber-300/90">{warning}</p>
          </div>
          {error && (
            <p className="text-xs text-destructive">
              {error instanceof ApiError ? error.message : "Could not change the mode."}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={onConfirm} disabled={busy}>
            {busy && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
            Switch mode
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PlatformSubdomains({ domain }: { domain: ApiDomain }) {
  const serving = domain.is_primary || !!domain.former_primary
  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground leading-relaxed">
        {domain.is_primary
          ? "This is the primary domain, so the platform is served here."
          : domain.former_primary
            ? "Still served here, though this is no longer the primary domain: consoles may be open on them, and nodes joined through its headscale. They stop when this domain is removed."
            : "Reserved on this domain, so no route can take them, but not served: they follow the primary domain."}
      </p>
      <div className="divide-y divide-border/50 rounded-md border border-border/60">
        {PLATFORM_SUBDOMAINS.map((s) => (
          <div key={s.name} className="flex items-center justify-between gap-3 px-3 py-2">
            <div className="min-w-0">
              <p className="font-mono text-xs text-foreground truncate">
                {s.name}.{domain.base_domain}
              </p>
              <p className="text-[11px] text-muted-foreground">{s.what}</p>
            </div>
            <span className={serving ? "text-xs text-emerald-400" : "text-xs text-muted-foreground"}>
              {serving ? "Serving" : "Reserved"}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function RoutesOnDomain({
  rows,
  domain,
  retiring,
  canChange,
  orgId,
  token,
}: {
  rows: ApiDomainRoute[]
  domain: ApiDomain
  retiring: boolean
  canChange: boolean
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [moving, setMoving] = useState<ApiDomainRoute | null>(null)
  const [deleting, setDeleting] = useState<ApiDomainRoute | null>(null)
  const { data: allDomains = [] } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: retiring && canChange,
  })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["domain-routes", orgId] })
    qc.invalidateQueries({ queryKey: ["routes"] })
  }
  const del = useMutation({
    mutationFn: (r: ApiDomainRoute) => routesApi.delete(orgId, r.project_id, r.id, token),
    onSuccess: () => {
      setDeleting(null)
      refresh()
    },
  })

  if (rows.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        {retiring ? "Nothing holds this domain any more. It can be removed." : "No routes use this domain yet."}
      </p>
    )
  }
  const showActions = retiring && canChange
  return (
    <div className="space-y-3">
      {retiring && (
        <p className="text-xs text-muted-foreground leading-relaxed">
          Move each route to another base domain, or delete it. A paused route counts: pausing stops traffic but keeps
          the name.
        </p>
      )}
      <div className="overflow-x-auto">
        <table className="w-full min-w-[520px] text-xs">
          <thead>
            <tr className="border-b border-border/60 text-left text-[11px] text-muted-foreground">
              <th className="py-1.5 pr-3 font-medium">Hostname</th>
              <th className="py-1.5 pr-3 font-medium">Project</th>
              <th className="py-1.5 pr-3 font-medium">Zone</th>
              <th className="py-1.5 pr-3 font-medium">State</th>
              {showActions && <th className="py-1.5 font-medium sr-only">Actions</th>}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id} className="border-b border-border/40 last:border-0 align-middle">
                <td className="py-2 pr-3">
                  <Link
                    to="/projects/$id/routes/$routeId"
                    params={{ id: r.project_id, routeId: r.id }}
                    className="font-mono text-foreground hover:underline break-all"
                  >
                    {r.hostname}
                  </Link>
                  {r.redirects_to && (
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      Redirects to <span className="font-mono">{r.redirects_to}</span>
                    </p>
                  )}
                </td>
                <td className="py-2 pr-3 text-muted-foreground">{r.project_name}</td>
                <td className="py-2 pr-3 text-muted-foreground capitalize">{r.zone}</td>
                <td className="py-2 pr-3">
                  {r.published ? (
                    <span className="text-emerald-400">Serving</span>
                  ) : (
                    <span className="text-muted-foreground">Paused</span>
                  )}
                </td>
                {showActions && (
                  <td className="py-2 text-right whitespace-nowrap">
                    {/* A redirect left by a move exists only to point
                        elsewhere, so it is deleted when its grace period is
                        over, not moved. */}
                    {r.redirects_to ? (
                      <Button size="sm" variant="ghost" className="h-7 text-xs text-destructive" onClick={() => setDeleting(r)}>
                        Delete redirect
                      </Button>
                    ) : (
                      <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setMoving(r)}>
                        Move
                      </Button>
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {moving && (
        <MoveRouteDialog
          route={moving}
          from={domain}
          domains={allDomains}
          orgId={orgId}
          token={token}
          onMoved={() => {
            setMoving(null)
            refresh()
          }}
          onCancel={() => setMoving(null)}
        />
      )}

      <Dialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Delete the redirect on {deleting?.hostname}?</DialogTitle>
            <DialogDescription>
              Links to the old name stop working. Requests there get nothing from this server once it is gone.
            </DialogDescription>
          </DialogHeader>
          {del.error && (
            <p className="text-xs text-destructive">
              {del.error instanceof ApiError ? del.error.message : "Could not delete the redirect."}
            </p>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleting(null)} disabled={del.isPending}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => deleting && del.mutate(deleting)} disabled={del.isPending}>
              {del.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Delete redirect
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

/**
 * How a domain goes, in the order it has to.
 *
 * A domain that has served routes is retired first, not removed with a click:
 * retiring stops anything new attaching, the checklist above shows what still
 * holds it, and Remove appears when that is empty. A domain that was never
 * verified skips all of it - nothing was served on it, and it is usually a
 * typo.
 */
function LifecycleSection({
  domain,
  routeCount,
  nodeCount,
  integrationCount,
  deployHookCount,
  orgId,
  token,
}: {
  domain: ApiDomain
  routeCount: number
  /** Nodes whose control connection goes through this domain's headscale. */
  nodeCount: number
  /** Git provider registrations still pointing at this domain. */
  integrationCount: number
  /** CI deploy webhooks last called through this domain. */
  deployHookCount: number
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [confirming, setConfirming] = useState(false)
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["domain", orgId, domain.id] })
    qc.invalidateQueries({ queryKey: ["domains", orgId] })
  }
  const retire = useMutation({
    mutationFn: () => domainsApi.retire(orgId, domain.id, token),
    onSuccess: invalidate,
  })
  const remove = useMutation({
    mutationFn: () => domainsApi.remove(orgId, domain.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
      navigate({ to: "/domains" })
    },
  })

  if (domain.verified && !domain.retiring_at) {
    return (
      <ResourcePanel
        title="Retire this domain"
        action={
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => retire.mutate()} disabled={retire.isPending}>
            {retire.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
            Start retiring
          </Button>
        }
      >
        <p className="text-xs text-muted-foreground leading-relaxed">
          The first step to removing it. New routes can no longer use it, so what still holds it can only go down, and
          this page lists each one with a way to move it. Nothing stops serving, and you can stop retiring at any time.
        </p>
        {retire.error && (
          <p className="mt-2 text-xs text-destructive">
            {retire.error instanceof ApiError ? retire.error.message : "Could not start retiring the domain."}
          </p>
        )}
      </ResourcePanel>
    )
  }

  const holding = routeCount + nodeCount + integrationCount + deployHookCount
  const blocked = domain.verified && holding > 0
  return (
    <ResourcePanel
      title="Remove this domain"
      action={
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs border-destructive/40 text-destructive hover:bg-destructive/10"
          disabled={blocked}
          onClick={() => setConfirming(true)}
        >
          Remove
        </Button>
      }
    >
      <p className="text-xs text-muted-foreground leading-relaxed">
        {!domain.verified
          ? "This domain was never verified, so nothing was ever served on it. It can be removed now."
          : blocked
            ? `${listed([
                routeCount > 0 && `${routeCount} ${routeCount === 1 ? "route" : "routes"}`,
                nodeCount > 0 && `${nodeCount} ${nodeCount === 1 ? "node" : "nodes"}`,
                integrationCount > 0 &&
                  `${integrationCount} git provider ${integrationCount === 1 ? "registration" : "registrations"}`,
                deployHookCount > 0 && `${deployHookCount} CI ${deployHookCount === 1 ? "job" : "jobs"}`,
              ])} still ${holding === 1 ? "holds" : "hold"} this domain. Clear ${
                holding === 1 ? "it" : "them"
              } above, and Remove becomes available.`
            : "Nothing holds this domain any more. Removing it stops this gateway serving it."}
      </p>

      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Remove {domain.base_domain}?</DialogTitle>
            <DialogDescription>
              This gateway stops serving it. The DNS records at your provider are not touched - remove them there too, or
              the name keeps pointing at this server.
            </DialogDescription>
          </DialogHeader>
          {remove.error && (
            <p className="text-xs text-destructive">
              {remove.error instanceof ApiError ? remove.error.message : "Could not remove the domain."}
            </p>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirming(false)} disabled={remove.isPending}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => remove.mutate()} disabled={remove.isPending}>
              {remove.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Remove domain
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ResourcePanel>
  )
}

/** "a", "a and b", "a, b and c". */
function listed(parts: (string | false)[]): string {
  const p = parts.filter(Boolean) as string[]
  return p.length <= 1 ? (p[0] ?? "") : `${p.slice(0, -1).join(", ")} and ${p[p.length - 1]}`
}
