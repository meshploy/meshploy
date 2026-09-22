import { useQuery } from "@tanstack/react-query"
import { ShieldAlert } from "lucide-react"
import { system as systemApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"

/**
 * What an internal route's certificate will be, on a gateway that manages its
 * own DNS.
 *
 * A public certificate for `*.internal.<domain>` is obtained by DNS-01: Caddy
 * proves control of the name by writing a record into the challenge zone, which
 * only works under NS delegation, where the gateway runs the authoritative DNS
 * for that zone. On a gateway installed with `--dns-mode=ondemand`, DNS stays
 * with the operator's provider and there is no zone to write into - and HTTP-01
 * cannot stand in for it, because the whole point of the internal zone is that
 * it is not reachable from the internet. So Caddy signs those names with its
 * own local CA instead.
 *
 * "NS delegation" is the wording install.sh and the docs use for the choice, so
 * it is the wording here: the operator met those words when they picked, and a
 * notice that renames the decision is a notice they have to re-derive.
 *
 * The route works. What differs is that nothing trusts the issuer until it is
 * told to, and a browser will say so in the way browsers do. That is worth
 * knowing before the route is created rather than at the certificate warning.
 *
 * Silent on a delegated gateway, and on anything that did not answer: an
 * advisory that appears when it does not apply teaches people to ignore it.
 */
export function SelfSignedNotice() {
  const token = useAuthStore((s) => s.token)!

  const { data } = useQuery({
    queryKey: ["system-exposure"],
    queryFn: () => systemApi.exposure(token),
    enabled: !!token,
    // Recorded at install; polling it would be noise. Shared with the exposure
    // notice, so this costs nothing on a page that has already asked.
    staleTime: Infinity,
  })

  if (data?.dns_mode !== "ondemand") return null

  return (
    <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3">
      <ShieldAlert className="h-4 w-4 shrink-0 text-amber-400 mt-0.5" />
      <div className="space-y-1.5 min-w-0">
        <p className="text-xs font-medium text-amber-400">
          Internal routes on this server use a self-signed certificate
        </p>
        <p className="text-[11px] leading-relaxed text-muted-foreground/80">
          Without <strong className="font-medium text-foreground/80">NS delegation</strong> there is no
          DNS zone here to prove control of, and an internal name does not answer from the internet to
          prove it another way. Caddy signs these with its own authority.
        </p>
        <p className="text-[11px] leading-relaxed text-muted-foreground/80">
          Traffic is still encrypted. Clients warn until that authority is trusted; public routes are
          unaffected.
        </p>
      </div>
    </div>
  )
}
