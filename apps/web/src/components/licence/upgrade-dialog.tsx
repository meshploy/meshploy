import { Check, ExternalLink, Minus } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/**
 * Where "Get a licence" goes.
 *
 * An external link rather than an in-app email form on purpose: this console
 * runs on the customer's own server, so capturing an address here would mean
 * the product posting to a Meshploy-operated endpoint. That turns a
 * self-contained install into one that phones home, breaks on air-gapped and
 * egress-restricted networks, and reads as telemetry in an MIT repository.
 *
 * The query parameter marks the highest-intent source there is — someone
 * already running Meshploy.
 */
const LICENCE_URL = "https://meshploy.com/enterprise?src=console"

type Tier = "both" | "enterprise"

type Feature = {
  label: string
  tier: Tier
  /** Shipping today. Anything false is described as planned, never implied. */
  available: boolean
  detail?: string
}

/**
 * One array drives both columns, so the comparison cannot drift between them.
 *
 * A feature list is marketing, not implementation — publishing it here costs
 * nothing, unlike the code behind an Enterprise feature, which stays in the
 * private repository. Keep this honest: `available: false` renders as planned,
 * because advertising something that does not ship yet is how trials end in
 * refund requests.
 */
const FEATURES: Feature[] = [
  { label: "Unlimited projects and services", tier: "both", available: true },
  { label: "WireGuard mesh and K3s orchestration", tier: "both", available: true },
  { label: "Git-driven builds and deployments", tier: "both", available: true },
  { label: "One-click templates", tier: "both", available: true },
  { label: "Agent principals and remote MCP", tier: "both", available: true },
  { label: "Backups and scheduled jobs", tier: "both", available: true },
  { label: "Community support", tier: "both", available: true },

  {
    label: "Single sign-on",
    tier: "enterprise",
    available: false,
    detail: "SAML and OIDC against your identity provider",
  },
  {
    label: "Audit logging",
    tier: "enterprise",
    available: false,
    detail: "Who changed what, exportable and retained",
  },
  {
    label: "Multi-tenancy",
    tier: "enterprise",
    available: false,
    detail: "Isolated tenants on shared infrastructure, for managed providers",
  },
  {
    label: "Commercial support",
    tier: "enterprise",
    available: false,
    detail: "Direct support with agreed response times",
  },
]

function Row({ label, detail, state }: { label: string; detail?: string; state: "yes" | "no" | "planned" }) {
  return (
    <div className="flex items-start gap-2 py-1.5">
      {state === "yes" && <Check className="h-3.5 w-3.5 text-primary shrink-0 mt-0.5" />}
      {state === "no" && <Minus className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0 mt-0.5" />}
      {state === "planned" && (
        <span className="h-3.5 w-3.5 shrink-0 mt-0.5 rounded-full border border-dashed border-muted-foreground/40" />
      )}
      <div className="min-w-0">
        <p className={cn("text-xs", state === "no" ? "text-muted-foreground/40" : "text-foreground")}>
          {label}
          {state === "planned" && (
            <span className="ml-1.5 text-[10px] uppercase tracking-wider text-muted-foreground/60">
              planned
            </span>
          )}
        </p>
        {detail && state !== "no" && (
          <p className="text-[11px] text-muted-foreground mt-0.5">{detail}</p>
        )}
      </div>
    </div>
  )
}

export function UpgradeDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Community and Enterprise</DialogTitle>
          <DialogDescription>
            Everything you run today stays in Community. Enterprise adds
            organisational features and a support agreement.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 sm:grid-cols-2">
          {/* Community */}
          <div className="rounded-md border border-border/60 p-3">
            <p className="text-sm font-medium">Community</p>
            <p className="text-xs text-muted-foreground mt-0.5 mb-2">
              Free and open source. What you are running now.
            </p>
            <div className="border-t border-border/40 pt-1">
              {FEATURES.map((f) => (
                <Row
                  key={f.label}
                  label={f.label}
                  state={f.tier === "both" ? "yes" : "no"}
                />
              ))}
            </div>
          </div>

          {/* Enterprise */}
          <div className="rounded-md border border-primary/40 bg-primary/[0.03] p-3 flex flex-col">
            <p className="text-sm font-medium">Enterprise</p>
            <p className="text-xs text-muted-foreground mt-0.5 mb-2">
              Everything in Community, plus the below.
            </p>
            <div className="border-t border-border/40 pt-1 flex-1">
              {FEATURES.map((f) => (
                <Row
                  key={f.label}
                  label={f.label}
                  detail={f.tier === "enterprise" ? f.detail : undefined}
                  state={f.tier === "both" ? "yes" : f.available ? "yes" : "planned"}
                />
              ))}
            </div>
            <Button
              size="sm"
              className="w-full mt-3 gap-1.5"
              render={
                <a href={LICENCE_URL} target="_blank" rel="noopener noreferrer">
                  Get a licence
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
              }
            />
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          Already have a licence? Close this and paste it below — activation
          needs the Enterprise image, and the licence section explains how to
          switch to it.
        </p>
      </DialogContent>
    </Dialog>
  )
}
