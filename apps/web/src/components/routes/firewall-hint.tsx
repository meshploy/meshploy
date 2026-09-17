import { useState } from "react"
import { AlertTriangle, Check, Copy, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { ApiPortFirewall } from "@/lib/api"
import { cn, formatRelativeTime } from "@/lib/utils"

const TOOL_NAMES: Record<string, string> = { ufw: "UFW", firewalld: "firewalld", iptables: "iptables" }

// Said everywhere a port's reachability comes up: nothing on the host can see
// a firewall at the hosting provider.
const PROVIDER_NOTE = "A firewall or security group at the hosting provider has to allow the port too, and the gateway cannot see it."

/**
 * The short form for a table row, beside the state: only when the host firewall
 * blocks the port, restricts it, or could not be checked. An open port shows
 * nothing here; the route's page still reminds about the provider's firewall.
 */
export function FirewallMarker({ port, verdict }: { port: number; verdict?: ApiPortFirewall }) {
  const state = verdict?.state ?? "unknown"
  if (state === "open") return null
  const tool = TOOL_NAMES[verdict?.tool ?? ""] ?? "host"
  const { label, cls, tip } = {
    blocked: {
      label: "blocked",
      cls: "text-destructive",
      tip: `The gateway's ${tool} firewall blocks port ${port}. Open the route for the command that allows it.`,
    },
    restricted: {
      label: "restricted",
      cls: "text-amber-400",
      tip: `The gateway's ${tool} firewall allows port ${port} only from ${(verdict?.sources ?? []).join(", ")}.`,
    },
    unknown: {
      label: "unchecked",
      cls: "text-amber-400",
      tip: `The gateway's firewall could not be checked${verdict?.reason ? `: ${verdict.reason}` : ""}.`,
    },
  }[state]
  return (
    <Tooltip>
      <TooltipTrigger render={<span className={cn("inline-flex items-center gap-1 text-[11px] cursor-default", cls)} />}>
        <AlertTriangle className="h-3 w-3" />
        {label}
      </TooltipTrigger>
      <TooltipContent>{tip}</TooltipContent>
    </Tooltip>
  )
}

/** What the gateway's own firewall does with a port, from the host agent. */
export function FirewallHint({ port, verdict }: { port: number; verdict?: ApiPortFirewall }) {
  const state = verdict?.state ?? "unknown"
  const tool = TOOL_NAMES[verdict?.tool ?? ""]
  const checked = verdict?.checked_at ? ` Checked ${formatRelativeTime(new Date(verdict.checked_at))}.` : ""

  if (state === "blocked") {
    const command = {
      ufw: `sudo ufw allow ${port}/tcp`,
      firewalld: `sudo firewall-cmd --permanent --add-port=${port}/tcp && sudo firewall-cmd --reload`,
    }[verdict?.tool ?? ""]
    return (
      <div className="flex gap-2.5 rounded-md border border-destructive/30 bg-destructive/5 p-3">
        <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-destructive mt-0.5" />
        <div className="min-w-0 flex-1 space-y-2 text-xs">
          <p className="text-destructive">
            The gateway's {tool ?? "host"} firewall blocks port {port}, so nothing from the internet reaches it.
            {command ? " Allow it by running this on the gateway:" : " Allow the port in the host firewall."}
          </p>
          {command && <CommandLine value={command} />}
          <p className="text-muted-foreground">{PROVIDER_NOTE}{checked}</p>
        </div>
      </div>
    )
  }

  if (state === "restricted") {
    return (
      <div className="flex gap-2.5 rounded-md border border-amber-500/30 bg-amber-500/5 p-3">
        <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-400 mt-0.5" />
        <div className="min-w-0 flex-1 space-y-1.5 text-xs">
          <p className="text-amber-400">
            The gateway's {tool ?? "host"} firewall allows port {port} only from{" "}
            {verdict!.sources!.map((s, i) => (
              <span key={s}>{i > 0 && ", "}<code className="font-mono">{s}</code></span>
            ))}.
          </p>
          <p className="text-muted-foreground">{PROVIDER_NOTE}{checked}</p>
        </div>
      </div>
    )
  }

  if (state === "open") {
    return (
      <p className="flex gap-2 text-xs text-muted-foreground">
        <ShieldCheck className="h-3.5 w-3.5 shrink-0 mt-px" />
        <span>Nothing on the gateway blocks port {port}. {PROVIDER_NOTE}{checked}</span>
      </p>
    )
  }

  return (
    <p className="flex gap-2 text-xs text-muted-foreground">
      <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-px text-amber-400" />
      <span>
        The gateway's firewall could not be checked{verdict?.reason ? `: ${verdict.reason}` : ""}. Make sure it allows
        port {port}/tcp. {PROVIDER_NOTE}
      </span>
    </p>
  )
}

function CommandLine({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-2">
      <code className="flex-1 min-w-0 truncate rounded border border-destructive/20 bg-background/60 px-2 py-1 font-mono text-foreground">{value}</code>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Copy firewall command"
        onClick={() => navigator.clipboard?.writeText(value).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        })}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </Button>
    </div>
  )
}
