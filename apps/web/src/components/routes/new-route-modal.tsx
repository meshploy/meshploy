import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Globe, Lock, Loader2 } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { domains as domainsApi, routes as routesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { cn } from "@/lib/utils"
import type { RouteZone } from "@/types"

interface NewRouteModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  orgId: string
  projectId: string
}

// Preview zone is excluded — preview hostnames are auto-generated per deployment
const ZONES: { key: RouteZone; label: string; description: string; icon: typeof Globe; color: string }[] = [
  {
    key: "public",
    label: "Public",
    description: "Internet-accessible",
    icon: Globe,
    color: "border-sky-500/40 bg-sky-500/8 text-sky-300",
  },
  {
    key: "internal",
    label: "Internal",
    description: "WireGuard mesh only",
    icon: Lock,
    color: "border-violet-500/40 bg-violet-500/8 text-violet-300",
  },
]

export function NewRouteModal({ open, onOpenChange, orgId, projectId }: NewRouteModalProps) {
  const token = useAuthStore((s) => s.token)!
  const currentOrgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const [zone, setZone] = useState<RouteZone>("public")
  const [domainId, setDomainId] = useState("")
  const [subdomain, setSubdomain] = useState("")
  const [targetIp, setTargetIp] = useState("")
  const [targetPort, setTargetPort] = useState("")
  const [error, setError] = useState<string | null>(null)

  const { data: domainList = [] } = useQuery({
    queryKey: ["domains", currentOrgId],
    queryFn: () => domainsApi.list(currentOrgId, token),
    enabled: open && !!currentOrgId,
  })

  const verifiedDomains = domainList.filter((d) => d.verified)
  const pendingDomains = domainList.filter((d) => !d.verified)
  const selectedDomain = verifiedDomains.find((d) => d.id === domainId)

  // Compute live hostname preview
  const hostnamePreview = (() => {
    if (!selectedDomain || !subdomain.trim()) return null
    const sub = subdomain.trim()
    switch (zone) {
      case "internal":
        return `${sub}.${selectedDomain.internal_subdomain}.${selectedDomain.base_domain}`
      default:
        return `${sub}.${selectedDomain.base_domain}`
    }
  })()

  // Client-side reserved subdomain guard (public zone only)
  const reservedError = (() => {
    if (!selectedDomain || !subdomain || zone !== "public") return null
    if (subdomain === selectedDomain.internal_subdomain) {
      return `"${subdomain}" is reserved for internal routing`
    }
    if (subdomain === selectedDomain.preview_subdomain) {
      return `"${subdomain}" is reserved for preview routing`
    }
    return null
  })()

  const createMutation = useMutation({
    mutationFn: () =>
      routesApi.create(
        orgId,
        projectId,
        {
          domain_id: domainId || undefined,
          zone,
          subdomain: subdomain.trim(),
          targets: [{
            path: "/",
            strip_path: false,
            target_ip: targetIp.trim(),
            target_port: parseInt(targetPort, 10),
          }],
        },
        token
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
      handleClose()
    },
    onError: (err: Error) => {
      setError(err.message)
    },
  })

  function handleClose() {
    onOpenChange(false)
    setZone("public")
    setDomainId("")
    setSubdomain("")
    setTargetIp("")
    setTargetPort("")
    setError(null)
  }

  const canSubmit =
    !!domainId &&
    !!subdomain.trim() &&
    !!targetIp.trim() &&
    !!targetPort &&
    !reservedError &&
    !createMutation.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton>
        <DialogHeader>
          <DialogTitle>New Route</DialogTitle>
        </DialogHeader>

        <div className="space-y-5 py-1">
          {/* Zone toggle */}
          <div className="grid grid-cols-2 gap-2">
            {ZONES.map(({ key, label, description, icon: Icon, color }) => (
              <button
                key={key}
                type="button"
                onClick={() => setZone(key)}
                className={cn(
                  "flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-sm transition-colors text-left",
                  zone === key
                    ? color
                    : "border-border/60 bg-card text-muted-foreground hover:border-border hover:text-foreground"
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <div>
                  <p className="font-medium leading-none">{label}</p>
                  <p className="text-[11px] mt-0.5 opacity-70 leading-none">{description}</p>
                </div>
              </button>
            ))}
          </div>

          {/* Domain picker */}
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Domain
            </label>
            <Select value={domainId} onValueChange={(v) => setDomainId(v ?? "")}>
              <SelectTrigger className="w-full! h-10 px-3 text-sm bg-input/30 border-border/60">
                <SelectValue placeholder="Select a domain…" />
              </SelectTrigger>
              <SelectContent>
                {verifiedDomains.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.base_domain}
                  </SelectItem>
                ))}
                {pendingDomains.length > 0 && verifiedDomains.length > 0 && (
                  <SelectSeparator />
                )}
                {pendingDomains.map((d) => (
                  <SelectItem key={d.id} value={d.id} disabled>
                    <span className="text-muted-foreground">{d.base_domain}</span>
                    <span className="ml-2 text-[10px] text-amber-400/70">pending</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Subdomain input */}
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Subdomain
            </label>
            <div
              className={cn(
                "flex items-stretch rounded-lg border bg-input/30 overflow-hidden focus-within:ring-3 focus-within:ring-ring/20 transition-all",
                reservedError
                  ? "border-destructive/50 focus-within:border-destructive"
                  : "border-border/60 focus-within:border-ring"
              )}
            >
              <input
                type="text"
                placeholder="my-service"
                value={subdomain}
                onChange={(e) => setSubdomain(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                className="flex-1 min-w-0 bg-transparent px-3 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 outline-none"
              />
              {selectedDomain && (
                <span className="flex items-center bg-muted/30 px-3 text-xs font-mono text-muted-foreground border-l border-border/60 shrink-0">
                  {zone === "internal"
                    ? `.${selectedDomain.internal_subdomain}.${selectedDomain.base_domain}`
                    : `.${selectedDomain.base_domain}`}
                </span>
              )}
            </div>
            {reservedError ? (
              <p className="text-[11px] text-destructive pl-0.5 flex items-center gap-1">
                <AlertCircle className="h-3 w-3" />
                {reservedError}
              </p>
            ) : hostnamePreview ? (
              <p className="text-[11px] font-mono text-muted-foreground/60 pl-0.5">
                → {hostnamePreview}
              </p>
            ) : null}
          </div>

          {/* Target */}
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Target
            </label>
            <div className="grid grid-cols-[1fr_auto] gap-2">
              <input
                type="text"
                placeholder="100.64.0.1"
                value={targetIp}
                onChange={(e) => setTargetIp(e.target.value)}
                className="rounded-lg border border-border/60 bg-input/30 px-3 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-ring focus:ring-3 focus:ring-ring/20 transition-all"
              />
              <input
                type="number"
                min={1}
                max={65535}
                placeholder="3000"
                value={targetPort}
                onChange={(e) => setTargetPort(e.target.value)}
                className="w-24 rounded-lg border border-border/60 bg-input/30 px-3 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-ring focus:ring-3 focus:ring-ring/20 transition-all"
              />
            </div>
            <p className="text-[11px] text-muted-foreground/60 pl-0.5">
              Headscale mesh IP (100.x.x.x) + service port
            </p>
          </div>

          {error && (
            <div className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2.5 text-sm text-destructive">
              <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
              {error}
            </div>
          )}
        </div>

        <DialogFooter showCloseButton>
          <Button
            onClick={() => createMutation.mutate()}
            disabled={!canSubmit}
            className="gap-1.5"
          >
            {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
            Create Route
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
