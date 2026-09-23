import { cn } from "@/lib/utils"
import type { DnsMode } from "@/lib/api/domains"

/**
 * How a base domain's DNS is arranged, and the one place it is described.
 *
 * Adding a domain and changing one later ask the same question, so they share
 * the words. "NS delegation" is what install.sh and the docs call it: the
 * operator met those words when they installed, and a picker that renamed the
 * choice would be one they had to re-derive.
 *
 * The difference that matters is the internal certificate, so each option says
 * what it does to that. Everything else - public routes, custom domains - works
 * the same either way, which is worth saying too, or the choice looks bigger
 * than it is.
 */
export const DNS_MODES: { value: DnsMode; label: string; detail: string }[] = [
  {
    value: "delegation",
    label: "NS delegation",
    detail:
      "Point the domain's NS record at this gateway. It runs the DNS, so internal routes get a trusted wildcard certificate.",
  },
  {
    value: "ondemand",
    label: "On-demand TLS",
    detail:
      "DNS stays with your provider; add a wildcard A record. Public routes get certificates as they are first visited. Internal routes are signed by this gateway's own authority.",
  },
]

export function dnsModeLabel(mode: DnsMode): string {
  return DNS_MODES.find((m) => m.value === mode)?.label ?? mode
}

export function DnsModePicker({
  value,
  onChange,
  disabled,
}: {
  value: DnsMode
  onChange: (mode: DnsMode) => void
  disabled?: boolean
}) {
  return (
    <div className="space-y-2" role="radiogroup" aria-label="DNS mode">
      {DNS_MODES.map((m) => {
        const selected = value === m.value
        return (
          <button
            key={m.value}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={disabled}
            onClick={() => onChange(m.value)}
            className={cn(
              "flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
              selected ? "border-primary bg-primary/5" : "border-border/60 bg-card",
              !selected && !disabled && "hover:border-border hover:bg-muted/20",
              disabled && "opacity-60 cursor-not-allowed"
            )}
          >
            <span
              className={cn(
                "mt-0.5 size-3.5 shrink-0 rounded-full border-2",
                selected ? "border-primary bg-primary/30" : "border-border"
              )}
            />
            <span className="min-w-0 space-y-0.5">
              <span className="block text-xs font-medium text-foreground">{m.label}</span>
              <span className="block text-xs text-muted-foreground">{m.detail}</span>
            </span>
          </button>
        )
      })}
    </div>
  )
}
