import { Link } from "@tanstack/react-router"
import { cn } from "@/lib/utils"
import type { ApiDNSProvider } from "@/lib/api/dns-providers"
import { RecordTable, type DnsRecord } from "@/components/domains/dns-records"

/**
 * Who keeps an on-demand domain's records: the operator by hand, or a
 * connected DNS provider, which Meshploy writes them through.
 *
 * A provider is offered for a name only when its token may write in a zone
 * holding that name, the same rule the API applies, so the choice fails here
 * rather than on submit.
 */

/** The zone among zones that holds name, the longest that is name or a parent
 *  of it; undefined when none does. */
export function zoneFor(zones: string[] | null | undefined, name: string): string | undefined {
  let best: string | undefined
  for (const z of zones ?? []) {
    if ((name === z || name.endsWith("." + z)) && z.length > (best?.length ?? 0)) best = z
  }
  return best
}

export function RecordsPicker({
  value,
  onChange,
  providers,
  domain,
  disabled,
}: {
  /** A provider's ID, or null for by hand. */
  value: string | null
  onChange: (providerId: string | null) => void
  providers: ApiDNSProvider[]
  /** The domain being added or changed; empty while it is being typed. */
  domain: string
  disabled?: boolean
}) {
  const options = [
    {
      id: null as string | null,
      label: "Manual",
      detail: "You add the records at your DNS provider, and a TXT record proves the domain is yours.",
      usable: true,
    },
    ...providers.map((p) => {
      const zone = domain ? zoneFor(p.zones, domain) : undefined
      return {
        id: p.id as string | null,
        label: p.name,
        detail: !domain
          ? `Meshploy writes the records. Zones: ${(p.zones ?? []).join(", ") || "none"}.`
          : zone
            ? `Meshploy writes the records in ${zone}, and the domain is verified at once. Certificates, internal ones included, are issued up front.`
            : `Its token may not write in a zone holding ${domain}. Zones: ${(p.zones ?? []).join(", ") || "none"}.`,
        usable: !domain || !!zone,
      }
    }),
  ]
  return (
    <div className="space-y-2" role="radiogroup" aria-label="Records">
      {options.map((o) => {
        const selected = value === o.id
        const off = disabled || !o.usable
        return (
          <button
            key={o.id ?? "manual"}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={off}
            onClick={() => onChange(o.id)}
            className={cn(
              "flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
              selected ? "border-primary bg-primary/5" : "border-border/60 bg-card",
              !selected && !off && "hover:border-border hover:bg-muted/20",
              off && "opacity-60 cursor-not-allowed"
            )}
          >
            <span
              className={cn(
                "mt-0.5 size-3.5 shrink-0 rounded-full border-2",
                selected ? "border-primary bg-primary/30" : "border-border"
              )}
            />
            <span className="min-w-0 space-y-0.5">
              <span className="block text-xs font-medium text-foreground">{o.label}</span>
              <span className="block text-xs text-muted-foreground">{o.detail}</span>
            </span>
          </button>
        )
      })}
      {providers.length === 0 && (
        <p className="text-xs text-muted-foreground">
          Connect Cloudflare in{" "}
          <Link to="/integrations/dns" className="text-foreground underline-offset-2 hover:underline">
            Integrations
          </Link>{" "}
          and Meshploy writes the records for you.
        </p>
      )}
    </div>
  )
}

/** The records a provider wrote for a domain, shown in place of the ones an
 *  operator would add by hand. */
export function ProviderRecords({
  domain,
  provider,
  publicIp,
}: {
  domain: string
  provider?: ApiDNSProvider
  publicIp?: string
}) {
  const zone = zoneFor(provider?.zones, domain)
  const records: DnsRecord[] = [
    { name: `*.${domain}`, zone, type: "A", value: publicIp || "<gateway public IP>" },
  ]
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground leading-relaxed">
        {provider ? provider.name : "The DNS provider"} keeps these, written by Meshploy and marked as
        its own. Removing the domain, or switching its records to manual, removes them again;
        records of yours under the same names are left alone.
      </p>
      <RecordTable records={records} />
    </div>
  )
}
