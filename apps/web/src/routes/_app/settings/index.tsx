import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Crown, Shield, User, Globe, Plus, Loader2, Trash2, AlertCircle, X } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { domains as domainsApi, type ApiDomain } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { mockOrgs, mockMembers } from "@/lib/mock-data"
import { cn } from "@/lib/utils"
import type { OrgRole } from "@/types"

const currentOrg = mockOrgs[0]

export const Route = createFileRoute("/_app/settings/")({
  component: SettingsPage,
})

function SettingsPage() {
  return (
    <div className="p-6 space-y-8 max-w-3xl">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Manage your organization settings and members</p>
      </div>

      <section className="space-y-4">
        <h2 className="text-sm font-medium text-foreground border-b border-border/40 pb-2">General</h2>
        <div className="grid gap-4">
          <Field label="Organization name" value={currentOrg.name} />
          <Field label="Slug" value={currentOrg.slug} mono />
          <Field label="Organization ID" value={currentOrg.id} mono muted />
        </div>
      </section>

      <PrimaryDomainSection />

      <section className="space-y-4">
        <div className="flex items-center justify-between border-b border-border/40 pb-2">
          <h2 className="text-sm font-medium text-foreground">Members</h2>
          <span className="text-xs text-muted-foreground">{mockMembers.length} members</span>
        </div>
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {mockMembers.map((member) => (
            <div key={member.id} className="flex items-center gap-3 px-4 py-3.5">
              <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
                <span className="text-xs font-semibold text-primary">
                  {member.name.split(" ").map((p) => p[0]).join("").slice(0, 2)}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-foreground">{member.name}</p>
                <p className="text-xs text-muted-foreground">{member.email}</p>
              </div>
              <RoleBadge role={member.role} />
            </div>
          ))}
        </div>
      </section>

      <section className="space-y-4">
        <h2 className="text-sm font-medium text-destructive border-b border-border/40 pb-2">Danger zone</h2>
        <div className="rounded-lg border border-destructive/30 p-4 flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">Delete organization</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Permanently delete this organization and all its projects, services, and nodes.
            </p>
          </div>
          <button className="shrink-0 text-xs text-destructive border border-destructive/40 px-3 py-1.5 rounded-md hover:bg-destructive/10 transition-colors">
            Delete org
          </button>
        </div>
      </section>
    </div>
  )
}

// ─── Primary Domain ───────────────────────────────────────────────────────────

function PrimaryDomainSection() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: domainList = [], isLoading } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: (domainId: string) => domainsApi.delete(orgId, domainId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
      setDeleteError(null)
    },
    onError: (err: Error) => setDeleteError(err.message),
  })

  const domain = domainList[0] ?? null

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between border-b border-border/40 pb-2">
        <div>
          <h2 className="text-sm font-medium text-foreground">Primary Domain</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            DNS and TLS are managed automatically by Meshploy
          </p>
        </div>
        {!domain && !isLoading && (
          <Button size="sm" className="gap-1.5 h-7 text-xs" render={<Link to="/domains/new" />}>
            <Plus className="h-3.5 w-3.5" />
            Add domain
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
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : !domain ? (
        <p className="text-sm text-muted-foreground">
          No domain configured yet. Add one to enable routing and automatic TLS.
        </p>
      ) : (
        <DomainCard
          domain={domain}
          onDelete={(id) => deleteMutation.mutate(id)}
          isDeleting={deleteMutation.isPending}
        />
      )}
    </section>
  )
}

function DomainCard({ domain, onDelete, isDeleting }: {
  domain: ApiDomain
  onDelete: (id: string) => void
  isDeleting: boolean
}) {
  return (
    <div className="rounded-lg border border-border/60 px-4 py-4">
      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-2">
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
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground font-mono">
            <span>
              <span className="text-muted-foreground/50">internal: </span>
              {domain.internal_subdomain}.{domain.base_domain}
            </span>
            <span>
              <span className="text-muted-foreground/50">preview: </span>
              {domain.preview_subdomain}.{domain.base_domain}
            </span>
            <span>
              <span className="text-muted-foreground/50">mesh: </span>
              mesh.{domain.base_domain}
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

// ─── Shared ───────────────────────────────────────────────────────────────────

function Field({ label, value, mono, muted }: { label: string; value: string; mono?: boolean; muted?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-muted-foreground">{label}</label>
      <div className="flex items-center h-9 px-3 rounded-md border border-border/60 bg-muted/20">
        <span className={`text-sm ${mono ? "font-mono" : ""} ${muted ? "text-muted-foreground" : "text-foreground"}`}>{value}</span>
      </div>
    </div>
  )
}

function RoleBadge({ role }: { role: OrgRole }) {
  if (role === "owner") {
    return (
      <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-500/10">
        <Crown className="h-2.5 w-2.5" />owner
      </Badge>
    )
  }
  if (role === "admin") {
    return (
      <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
        <Shield className="h-2.5 w-2.5" />admin
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}
