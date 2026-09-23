import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { cn } from "@/lib/utils"
import type { DnsMode } from "@/lib/api/domains"

/**
 * The DNS records a base domain needs, in the order they have to be added.
 *
 * The order is the point, and it is not obvious. Ownership is proved by a TXT
 * record, looked up through public DNS. Under NS delegation, once the NS record
 * points here, that lookup is answered by this gateway - which serves nothing
 * for a domain until it is verified. Delegate first and the proof can never be
 * found. So the TXT goes in at the current provider, verification happens, and
 * only then does the NS record move.
 *
 * On-demand has no such trap, since DNS never moves, but it is shown in the
 * same order so there is one sequence to learn.
 */
export function DnsRecords({
  domain,
  mode,
  verified,
  verifyToken,
  publicIp,
}: {
  domain: string
  mode: DnsMode
  verified: boolean
  verifyToken?: string
  publicIp?: string
}) {
  const ip = publicIp || "<gateway public IP>"
  const pointing: DnsRecord[] =
    mode === "delegation"
      ? [
          { name: domain, type: "NS", value: `ns1.${domain}`, note: "At the parent zone or registrar" },
          { name: `ns1.${domain}`, type: "A", value: ip, note: "Glue record, beside the NS" },
        ]
      : // The wildcard only. Every route has a subdomain, so nothing here serves
        // the bare name, and asking for it would move a website that lives there.
        [{ name: `*.${domain}`, type: "A", value: ip }]

  return (
    <ol className="space-y-5">
      {verifyToken !== undefined && (
        <Step
          n={1}
          title="Prove you own it"
          done={verified}
          detail={
            mode === "delegation"
              ? "Add this at the provider that answers for the domain today, before changing its NS record. Once the NS points here, this gateway answers - and it serves nothing for a domain until it is verified."
              : "Add this at your DNS provider."
          }
        >
          <RecordTable records={[{ name: `_meshploy-verify.${domain}`, type: "TXT", value: verifyToken || "" }]} />
        </Step>
      )}
      <Step
        n={verifyToken !== undefined ? 2 : 1}
        title={mode === "delegation" ? "Delegate it to this gateway" : "Point it at this gateway"}
        done={false}
        pending={verifyToken !== undefined && !verified}
        detail={
          mode === "delegation"
            ? "This gateway then runs the domain's DNS, including the wildcard certificate for internal routes."
            : "Every subdomain resolves here; certificates are issued as each hostname is first visited."
        }
      >
        <RecordTable records={pointing} />
      </Step>
    </ol>
  )
}

export interface DnsRecord {
  name: string
  type: string
  value: string
  note?: string
}

export function Step({
  n,
  title,
  detail,
  done,
  pending,
  children,
}: {
  n: number
  title: string
  detail: string
  done: boolean
  /** Dims the step. A string says what it waits on. */
  pending?: boolean | string
  children: React.ReactNode
}) {
  return (
    <li className={cn("flex gap-3", pending && "opacity-50")}>
      <span
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-medium",
          done ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-400" : "border-border text-muted-foreground"
        )}
      >
        {done ? <Check className="size-3" /> : n}
      </span>
      <div className="min-w-0 flex-1 space-y-2">
        <div className="space-y-0.5">
          <p className="text-xs font-medium text-foreground">
            {title}
            {pending && <span className="ml-2 font-normal text-muted-foreground">{pending === true ? "after step 1" : pending}</span>}
          </p>
          <p className="text-xs text-muted-foreground leading-relaxed">{detail}</p>
        </div>
        {children}
      </div>
    </li>
  )
}

export function RecordTable({ records }: { records: DnsRecord[] }) {
  return (
    <>
      {/* Stacked on a phone. A table there has to scroll sideways, and the
          column it hides is the value - the one thing the reader came to copy. */}
      <div className="divide-y divide-border/50 rounded-md border border-border/60 sm:hidden">
        {records.map((r) => (
          <div key={r.name + r.type} className="space-y-1.5 px-3 py-2.5 text-xs">
            <div className="flex items-baseline justify-between gap-3">
              <span className="text-[11px] text-muted-foreground">Name</span>
              <span className="font-mono text-[11px] text-muted-foreground">{r.type}</span>
            </div>
            <CopyValue value={r.name} />
            {r.note && <p className="text-[11px] text-muted-foreground/70">{r.note}</p>}
            <p className="pt-1 text-[11px] text-muted-foreground">Value</p>
            <CopyValue value={r.value} />
          </div>
        ))}
      </div>
      <div className="hidden overflow-x-auto rounded-md border border-border/60 sm:block">
      <table className="w-full min-w-[420px] text-xs">
        <thead>
          <tr className="border-b border-border/60 text-left text-[11px] text-muted-foreground">
            <th className="px-3 py-1.5 font-medium">Name</th>
            <th className="px-3 py-1.5 font-medium">Type</th>
            <th className="px-3 py-1.5 font-medium">Value</th>
          </tr>
        </thead>
        <tbody>
          {records.map((r) => (
            <tr key={r.name + r.type} className="border-b border-border/40 last:border-0 align-top">
              <td className="px-3 py-2">
                <CopyValue value={r.name} />
                {r.note && <p className="mt-0.5 text-[11px] text-muted-foreground/70">{r.note}</p>}
              </td>
              <td className="px-3 py-2 font-mono text-muted-foreground">{r.type}</td>
              <td className="px-3 py-2">
                <CopyValue value={r.value} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
    </>
  )
}

export function CopyValue({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <span className="group inline-flex items-center gap-1.5 font-mono text-foreground/90 break-all">
      {value}
      <button
        type="button"
        aria-label={`Copy ${value}`}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value)
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          } catch {
            // Nothing to fall back to; the value is on screen to select.
          }
        }}
        // Visible without hover: on a touch screen there is no hover, and a
        // record is only useful if it can be copied.
        className="shrink-0 text-muted-foreground opacity-60 hover:opacity-100"
      >
        {copied ? <Check className="size-3 text-emerald-400" /> : <Copy className="size-3" />}
      </button>
    </span>
  )
}
