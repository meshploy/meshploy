import { useState } from "react"
import { useNavigate } from "@tanstack/react-router"
import { useMutation } from "@tanstack/react-query"
import { Check, ChevronRight, Copy, Globe, Loader2, RefreshCw, AlertCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { domains as domainsApi, type ApiDomain } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { cn } from "@/lib/utils"

type Step = "base-domain" | "subdomains" | "verify" | "dns-setup"

const STEPS: { key: Step; label: string }[] = [
  { key: "base-domain", label: "Base domain" },
  { key: "subdomains", label: "Reserved subdomains" },
  { key: "verify", label: "Verify ownership" },
  { key: "dns-setup", label: "DNS routing" },
]

interface DomainSetupWizardProps {
  /** If provided, the wizard shows a "Back" breadcrumb to this path (Settings flow). */
  backTo?: string
}

export function DomainSetupWizard({ backTo }: DomainSetupWizardProps) {
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const [step, setStep] = useState<Step>("base-domain")
  const [baseDomain, setBaseDomain] = useState("")
  const [internalSub, setInternalSub] = useState("internal")
  const [previewSub, setPreviewSub] = useState("preview")
  const [createdDomain, setCreatedDomain] = useState<ApiDomain | null>(null)
  const [verifyError, setVerifyError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const currentStepIndex = STEPS.findIndex((s) => s.key === step)

  // ── Step 1+2 create ───────────────────────────────────────────────────────

  const createMutation = useMutation({
    mutationFn: () =>
      domainsApi.create(
        orgId,
        {
          base_domain: baseDomain.trim().toLowerCase(),
          internal_subdomain: internalSub.trim() || "internal",
          preview_subdomain: previewSub.trim() || "preview",
        },
        token
      ),
    onSuccess: (domain) => {
      setCreatedDomain(domain)
      setStep("verify")
    },
  })

  // ── Step 3 verify ─────────────────────────────────────────────────────────

  const verifyMutation = useMutation({
    mutationFn: () => domainsApi.verify(orgId, createdDomain!.id, token),
    onSuccess: (domain) => {
      setCreatedDomain(domain)
      setVerifyError(null)
      setStep("dns-setup")
    },
    onError: (err: Error) => {
      setVerifyError(err.message)
    },
  })

  function copyToken() {
    if (!createdDomain) return
    navigator.clipboard.writeText(createdDomain.id).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  function handleDone() {
    navigate({ to: backTo ?? "/nodes" })
  }

  return (
    <div className="min-h-[calc(100vh-4rem)] flex items-start justify-center pt-16 px-4">
      <div className="w-full max-w-xl">
        {/* Step indicators */}
        <div className="flex items-center gap-0 mb-10">
          {STEPS.map((s, i) => {
            const done = i < currentStepIndex
            const active = i === currentStepIndex
            return (
              <div key={s.key} className="flex items-center gap-0 flex-1 last:flex-none">
                <div className="flex flex-col items-center gap-1.5">
                  <div
                    className={cn(
                      "h-7 w-7 rounded-full flex items-center justify-center text-xs font-semibold border transition-colors",
                      done
                        ? "bg-primary border-primary text-primary-foreground"
                        : active
                          ? "border-primary text-primary bg-primary/10"
                          : "border-border/50 text-muted-foreground bg-card"
                    )}
                  >
                    {done ? <Check className="h-3.5 w-3.5" /> : i + 1}
                  </div>
                  <span
                    className={cn(
                      "text-[11px] font-medium whitespace-nowrap",
                      active ? "text-foreground" : "text-muted-foreground"
                    )}
                  >
                    {s.label}
                  </span>
                </div>
                {i < STEPS.length - 1 && (
                  <div
                    className={cn(
                      "flex-1 h-px mx-3 mt-[-14px]",
                      done ? "bg-primary" : "bg-border/40"
                    )}
                  />
                )}
              </div>
            )
          })}
        </div>

        {/* ── Step 1: Base domain ──────────────────────────────────────────── */}
        {step === "base-domain" && (
          <div className="space-y-6">
            <div>
              <h2 className="text-lg font-semibold tracking-tight">Base domain</h2>
              <p className="text-sm text-muted-foreground mt-1">
                The root domain managed by your CoreDNS server. This cannot be changed after setup.
              </p>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Domain
              </label>
              <input
                type="text"
                placeholder="cs.example.com"
                value={baseDomain}
                onChange={(e) => setBaseDomain(e.target.value)}
                autoFocus
                className="w-full rounded-lg border border-border/60 bg-input/30 px-3 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring focus:ring-3 focus:ring-ring/20 transition-all"
              />
            </div>
            <div className="rounded-lg border border-border/40 bg-muted/10 px-4 py-3 text-[13px] text-muted-foreground leading-relaxed">
              <Globe className="inline h-3.5 w-3.5 mr-1.5 -mt-0.5" />
              Your DNS provider should have a wildcard <code className="font-mono text-foreground/80">*.{baseDomain || "your-domain.com"}</code> A record pointing to your gateway node's public IP.
            </div>
            <div className="flex justify-end">
              <Button
                onClick={() => setStep("subdomains")}
                disabled={!baseDomain.trim() || !baseDomain.includes(".")}
                className="gap-1.5"
              >
                Continue
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        )}

        {/* ── Step 2: Reserved subdomains ───────────────────────────────────── */}
        {step === "subdomains" && (
          <div className="space-y-6">
            <div>
              <h2 className="text-lg font-semibold tracking-tight">Reserved subdomains</h2>
              <p className="text-sm text-muted-foreground mt-1">
                These prefixes are reserved for internal and preview routing. They can be changed later from Settings.
              </p>
            </div>

            <div className="space-y-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  Internal zone
                </label>
                <div className="flex items-stretch rounded-lg border border-border/60 bg-input/30 overflow-hidden focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/20 transition-all">
                  <input
                    type="text"
                    value={internalSub}
                    onChange={(e) => setInternalSub(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                    className="flex-1 min-w-0 bg-transparent px-3 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 outline-none"
                  />
                  <span className="flex items-center bg-muted/30 px-3 text-xs font-mono text-muted-foreground border-l border-border/60 shrink-0">
                    .{baseDomain}
                  </span>
                </div>
                <p className="text-[11px] text-muted-foreground/60 pl-0.5">
                  Caddy wildcard TLS — service-to-service routing over WireGuard
                </p>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  Preview zone
                </label>
                <div className="flex items-stretch rounded-lg border border-border/60 bg-input/30 overflow-hidden focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/20 transition-all">
                  <input
                    type="text"
                    value={previewSub}
                    onChange={(e) => setPreviewSub(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                    className="flex-1 min-w-0 bg-transparent px-3 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 outline-none"
                  />
                  <span className="flex items-center bg-muted/30 px-3 text-xs font-mono text-muted-foreground border-l border-border/60 shrink-0">
                    .{baseDomain}
                  </span>
                </div>
                <p className="text-[11px] text-muted-foreground/60 pl-0.5">
                  Deployment previews — isolated per-branch environments
                </p>
              </div>
            </div>

            {createMutation.isError && (
              <div className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2.5 text-sm text-destructive">
                <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                {(createMutation.error as Error).message}
              </div>
            )}

            <div className="flex justify-between">
              <Button variant="ghost" onClick={() => setStep("base-domain")}>
                Back
              </Button>
              <Button
                onClick={() => createMutation.mutate()}
                disabled={createMutation.isPending || !internalSub || !previewSub}
                className="gap-1.5"
              >
                {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                Create domain
                {!createMutation.isPending && <ChevronRight className="h-4 w-4" />}
              </Button>
            </div>
          </div>
        )}

        {/* ── Step 3: Verify ownership ──────────────────────────────────────── */}
        {step === "verify" && createdDomain && (
          <div className="space-y-6">
            <div>
              <h2 className="text-lg font-semibold tracking-tight">Verify domain ownership</h2>
              <p className="text-sm text-muted-foreground mt-1">
                Add this TXT record at your DNS provider to prove you own <strong className="text-foreground font-mono">{createdDomain.base_domain}</strong>.
              </p>
            </div>

            <div className="rounded-lg border border-border/60 overflow-hidden">
              <div className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 px-4 py-3.5 text-sm">
                <span className="text-muted-foreground font-medium">Name</span>
                <code className="font-mono text-foreground/90 break-all">
                  _meshploy-verify.{createdDomain.base_domain}
                </code>
                <span className="text-muted-foreground font-medium">Type</span>
                <code className="font-mono text-foreground/90">TXT</code>
                <span className="text-muted-foreground font-medium">Value</span>
                <div className="flex items-center gap-2 min-w-0">
                  <code className="font-mono text-foreground/90 text-xs break-all">{createdDomain.id}</code>
                  <button
                    type="button"
                    onClick={copyToken}
                    className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
                    title="Copy"
                  >
                    {copied ? (
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
              </div>
            </div>

            <div className="rounded-lg border border-border/40 bg-muted/10 px-4 py-3 text-[13px] text-muted-foreground">
              DNS changes can take a few minutes to propagate. Click{" "}
              <strong className="text-foreground">Check DNS</strong> once you've added the record.
            </div>

            {verifyError && (
              <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-300">
                <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                {verifyError}
              </div>
            )}

            <div className="flex items-center justify-between">
              <button
                type="button"
                onClick={handleDone}
                className="text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                Skip for now
              </button>
              <Button
                onClick={() => verifyMutation.mutate()}
                disabled={verifyMutation.isPending}
                className="gap-1.5"
              >
                {verifyMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <RefreshCw className="h-4 w-4" />
                )}
                Check DNS
              </Button>
            </div>
          </div>
        )}

        {/* ── Step 4: DNS routing info ──────────────────────────────────────── */}
        {step === "dns-setup" && createdDomain && (
          <div className="space-y-6">
            <div className="flex items-center gap-2">
              <div className="h-8 w-8 rounded-full bg-emerald-500/15 flex items-center justify-center">
                <Check className="h-4 w-4 text-emerald-400" />
              </div>
              <div>
                <h2 className="text-lg font-semibold tracking-tight">Domain verified</h2>
                <p className="text-sm text-muted-foreground">
                  <strong className="text-foreground font-mono">{createdDomain.base_domain}</strong> is now active.
                </p>
              </div>
            </div>

            <div className="space-y-3">
              <h3 className="text-sm font-medium">DNS records to configure</h3>
              <p className="text-[13px] text-muted-foreground">
                Add these wildcard A records in your CoreDNS zone file, pointing to your gateway node's public IP:
              </p>
              <div className="rounded-lg border border-border/60 overflow-hidden">
                <div className="divide-y divide-border/40">
                  {[
                    { name: `*.${createdDomain.internal_subdomain}.${createdDomain.base_domain}`, note: "Internal zone — Caddy wildcard TLS" },
                    { name: `*.${createdDomain.preview_subdomain}.${createdDomain.base_domain}`, note: "Preview zone — deployment previews" },
                    { name: `*.mesh.${createdDomain.base_domain}`, note: "Mesh zone — Headscale MagicDNS" },
                    { name: `*.${createdDomain.base_domain}`, note: "Public routes — user-created" },
                  ].map((row) => (
                    <div key={row.name} className="grid grid-cols-[1fr_auto] gap-4 px-4 py-3 items-start">
                      <div>
                        <code className="text-xs font-mono text-foreground/90">{row.name}</code>
                        <p className="text-[11px] text-muted-foreground mt-0.5">{row.note}</p>
                      </div>
                      <div className="text-right">
                        <code className="text-xs font-mono text-muted-foreground">A → &lt;gateway IP&gt;</code>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
              <p className="text-[12px] text-muted-foreground/60">
                The wildcard <code className="font-mono">*.{createdDomain.base_domain}</code> already covers public routes — you only need the two additional wildcard records above.
              </p>
            </div>

            <div className="flex justify-end">
              <Button onClick={handleDone} className="gap-1.5">
                Done
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
