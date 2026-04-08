import { createFileRoute, Link } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  Globe,
  Loader2,
  Plus,
  Sparkles,
  Trash2,
  AlertCircle,
  X,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { domains as domainsApi, type ApiDomain } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/domains/")({
  component: DomainsSettingsPage,
})

function DomainsSettingsPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const { data: domainList = [], isLoading } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const [deleteError, setDeleteError] = useState<string | null>(null)

  const deleteMutation = useMutation({
    mutationFn: (domainId: string) => domainsApi.delete(orgId, domainId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
      setDeleteError(null)
    },
    onError: (err: Error) => {
      setDeleteError(err.message)
    },
  })

  // CE: first domain exists → "Add Domain" is soft-locked
  const atCELimit = domainList.length >= 1

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Domains</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Manage base domains and their reserved subdomains.
          </p>
        </div>

        {atCELimit ? (
          <Tooltip>
            <TooltipTrigger>
              <Button disabled className="gap-1.5 opacity-60 cursor-not-allowed">
                <Plus className="h-3.5 w-3.5" />
                Add Domain
                <span className="ml-1 flex items-center gap-0.5 rounded-sm bg-amber-500/20 px-1 py-0.5 text-[10px] font-semibold text-amber-400">
                  <Sparkles className="h-2.5 w-2.5" />
                  EE
                </span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="left">
              Multiple domains require Meshploy EE
            </TooltipContent>
          </Tooltip>
        ) : (
          <Button
            className="gap-1.5"
            render={<Link to="/domains/new" />}
          >
            <Plus className="h-3.5 w-3.5" />
            Add Domain
          </Button>
        )}
      </div>

      {deleteError && (
        <div className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2.5 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span className="flex-1">{deleteError}</span>
          <button onClick={() => setDeleteError(null)} className="shrink-0 hover:text-destructive/70">
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {isLoading ? (
        <div className="flex items-center gap-2 py-10 justify-center text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span className="text-sm">Loading domains…</span>
        </div>
      ) : domainList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-12 flex flex-col items-center gap-4">
          <Globe className="h-8 w-8 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No domains configured</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              Add a domain to enable routing for your organization
            </p>
          </div>
          <Button
            size="sm"
            className="gap-1.5 mt-1"
            render={<Link to="/domains/new" />}
          >
            <Plus className="h-3.5 w-3.5" />
            Add Domain
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {domainList.map((domain) => (
            <DomainRow
              key={domain.id}
              domain={domain}
              onDelete={(id) => deleteMutation.mutate(id)}
              isDeleting={deleteMutation.isPending}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ─── DomainRow ────────────────────────────────────────────────────────────────

interface DomainRowProps {
  domain: ApiDomain
  onDelete: (id: string) => void
  isDeleting: boolean
}

function DomainRow({ domain, onDelete, isDeleting }: DomainRowProps) {
  return (
    <div className="px-4 py-4">
      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <Globe className="h-4 w-4 text-muted-foreground shrink-0" />
            <span className="text-sm font-medium font-mono">{domain.base_domain}</span>
            <span
              className={cn(
                "text-[10px] font-medium px-1.5 py-0.5 rounded-full border",
                domain.verified
                  ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/20"
                  : "bg-amber-500/10 text-amber-400 border-amber-500/20"
              )}
            >
              {domain.verified ? "verified" : "pending"}
            </span>
          </div>
          <div className="mt-1.5 flex items-center gap-4 text-xs text-muted-foreground font-mono">
            <span>
              <span className="text-muted-foreground/50">internal: </span>
              {domain.internal_subdomain}.{domain.base_domain}
            </span>
            <span>
              <span className="text-muted-foreground/50">preview: </span>
              {domain.preview_subdomain}.{domain.base_domain}
            </span>
          </div>
        </div>

        <button
          type="button"
          onClick={() => onDelete(domain.id)}
          disabled={isDeleting}
          className="p-1.5 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-40 shrink-0"
          title="Delete domain"
        >
          {isDeleting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
    </div>
  )
}
