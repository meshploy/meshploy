import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, RefreshCw, ShieldAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { RecordTable, Step } from "@/components/domains/dns-records"
import { useGatewayPublicIp } from "@/components/domains/use-gateway-ip"
import { routes as routesApi, ApiError } from "@/lib/api"
import type { ApiDbRoute } from "@/lib/api/routes"

/**
 * Proving a custom hostname belongs to whoever added it.
 *
 * A custom hostname is a whole name, not a subdomain of a base domain this
 * organisation has already proved, so it is proved on its own: a TXT record at
 * `_meshploy-verify.<hostname>` carrying the route's token. Until it is found,
 * the certificate check Caddy makes before issuing refuses the name - so a
 * route whose DNS already points here still fails its TLS handshake, and this
 * panel is the only place that says why.
 *
 * The check existed on the API for a long time with nothing calling it, which
 * meant a custom-domain route created in the console could never get a
 * certificate. This is that missing half.
 *
 * Unlike a base domain under NS delegation, the two records have no order: DNS
 * for a custom hostname stays with its provider, so the proof is always looked
 * up there.
 */
export function CustomDomainOwnership({
  route,
  orgId,
  projectId,
  token,
}: {
  route: ApiDbRoute
  orgId: string
  projectId: string
  token: string
}) {
  const qc = useQueryClient()
  const publicIp = useGatewayPublicIp(orgId, token)
  const verify = useMutation({
    mutationFn: () => routesApi.verifyHostname(orgId, projectId, route.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["route", orgId, projectId, route.id] })
      qc.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["custom-domains", orgId] })
    },
    onError: () => {
      // A route with no token yet is issued one by the failed check; show it.
      qc.invalidateQueries({ queryKey: ["route", orgId, projectId, route.id] })
    },
  })

  if (route.domain_id || route.custom_domain_verified) return null

  const host = route.hostname
  return (
    <section className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4 space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-2.5 min-w-0">
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
          <div className="space-y-1 min-w-0">
            <p className="text-xs font-medium text-amber-400">No certificate until ownership is proved</p>
            <p className="text-[11px] leading-relaxed text-muted-foreground/80">
              <span className="font-mono">{host}</span> is a custom hostname, so it has to be proved on its own.
              Until then requests to it fail the TLS handshake, even once its DNS points here.
            </p>
          </div>
        </div>
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
      </div>

      <ol className="space-y-5">
        <Step n={1} title="Prove you own it" done={false} detail="Add this at the DNS provider for the hostname.">
          {route.custom_domain_verify_token ? (
            <RecordTable
              records={[{ name: `_meshploy-verify.${host}`, type: "TXT", value: route.custom_domain_verify_token }]}
            />
          ) : (
            <p className="text-xs text-muted-foreground">
              This route predates verification records. Check now issues one.
            </p>
          )}
        </Step>
        <Step
          n={2}
          title="Point it at this gateway"
          done={false}
          detail="In any order with the record above. A CNAME to a name that resolves here works too, except at the root of a domain."
        >
          <RecordTable records={[{ name: host, type: "A", value: publicIp || "<gateway public IP>" }]} />
        </Step>
      </ol>
    </section>
  )
}
