import { createFileRoute, Link } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Crown, Globe, Loader2, Plus, Shield, User, Check, Pencil, X } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { orgs as orgsApi, domains as domainsApi, type ApiDomain, type ApiOrgMember } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"
import type { OrgRole } from "@/types"

export const Route = createFileRoute("/_app/settings/")({
  component: SettingsPage,
})

function SettingsPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const setCurrentOrg = useOrgStore((s) => s.setCurrentOrg)

  const { data: org, isLoading } = useQuery({
    queryKey: ["org", orgId],
    queryFn: () => orgsApi.get(orgId, token),
    enabled: !!orgId,
  })

  if (isLoading || !org) {
    return (
      <div className="p-6 flex items-center gap-2 text-muted-foreground text-sm">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>Loading…</span>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Manage your organization settings and members</p>
      </div>

      <GeneralSection org={org} onNameUpdated={(updated) => setCurrentOrg({ id: updated.id, name: updated.name, slug: updated.slug })} />

      <PrimaryDomainSection />

      <MembersSection />
    </div>
  )
}

function GeneralSection({ org, onNameUpdated }: { org: { id: string; name: string; slug: string }; onNameUpdated: (o: { id: string; name: string; slug: string }) => void }) {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [nameVal, setNameVal] = useState(org.name)

  const { mutate: saveName, isPending } = useMutation({
    mutationFn: () => orgsApi.update(org.id, nameVal, token),
    onSuccess: (updated) => {
      qc.setQueryData(["org", org.id], updated)
      onNameUpdated(updated)
      setEditing(false)
    },
  })

  return (
    <Section title="Workspace">
      <div className="flex flex-col gap-1">
        <label className="text-xs text-muted-foreground">Name</label>
        {editing ? (
          <div className="flex items-center gap-2">
            <Input
              value={nameVal}
              onChange={(e) => setNameVal(e.target.value)}
              className="h-9 text-sm"
              onKeyDown={(e) => { if (e.key === "Enter") saveName(); if (e.key === "Escape") { setEditing(false); setNameVal(org.name) } }}
              autoFocus
            />
            <Button size="icon" variant="ghost" className="h-9 w-9 shrink-0" onClick={() => saveName()} disabled={isPending}>
              {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5 text-primary" />}
            </Button>
            <Button size="icon" variant="ghost" className="h-9 w-9 shrink-0" onClick={() => { setEditing(false); setNameVal(org.name) }}>
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        ) : (
          <div className="flex items-center gap-2">
            <div className="flex items-center h-9 px-3 rounded-md border border-border/60 bg-muted/20 flex-1 min-w-0">
              <span className="text-sm">{org.name}</span>
            </div>
            <Button size="icon" variant="ghost" className="h-9 w-9 shrink-0" onClick={() => setEditing(true)}>
              <Pencil className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}
      </div>
    </Section>
  )
}

function PrimaryDomainSection() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: domainList = [], isLoading } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const domain = domainList[0] ?? null

  return (
    <Section
      title="Primary Domain"
      subtitle="DNS and TLS are managed automatically by Meshploy"
      action={
        !domain && !isLoading ? (
          <Button size="sm" className="gap-1.5 h-7 text-xs" render={<Link to="/domains/new" />}>
            <Plus className="h-3.5 w-3.5" />
            Add domain
          </Button>
        ) : undefined
      }
    >
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
        <DomainCard domain={domain} />
      )}
    </Section>
  )
}

function MembersSection() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const [inviteEmail, setInviteEmail] = useState("")
  const [showInvite, setShowInvite] = useState(false)

  const { data: members = [], isLoading } = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId, token),
    enabled: !!orgId,
  })

  const { mutate: invite, isPending: inviting, error: inviteError } = useMutation({
    mutationFn: () => orgsApi.addMember(orgId, inviteEmail, "member", token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["org-members", orgId] })
      setInviteEmail("")
      setShowInvite(false)
    },
  })

  return (
    <Section
      title="Members"
      action={
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{members.length} {members.length === 1 ? "member" : "members"}</span>
          <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs" onClick={() => setShowInvite((v) => !v)}>
            <Plus className="h-3.5 w-3.5" />
            Invite
          </Button>
        </div>
      }
    >
      {showInvite && (
        <div className="flex items-center gap-2 mb-3">
          <Input
            placeholder="Email address"
            value={inviteEmail}
            onChange={(e) => setInviteEmail(e.target.value)}
            className="h-8 text-sm"
            onKeyDown={(e) => { if (e.key === "Enter") invite() }}
          />
          <Button size="sm" className="h-8 shrink-0" onClick={() => invite()} disabled={inviting || !inviteEmail}>
            {inviting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : "Invite"}
          </Button>
          <Button size="sm" variant="ghost" className="h-8 shrink-0" onClick={() => { setShowInvite(false); setInviteEmail("") }}>
            Cancel
          </Button>
        </div>
      )}
      {inviteError && (
        <p className="text-xs text-destructive mb-2">{String((inviteError as Error).message)}</p>
      )}
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {members.map((member) => (
            <MemberRow key={member.id} member={member} />
          ))}
        </div>
      )}
    </Section>
  )
}

function MemberRow({ member }: { member: ApiOrgMember }) {
  const initials = member.user_name.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()

  return (
    <div className="flex items-center gap-3 px-4 py-3.5">
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <span className="text-xs font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{member.user_name}</p>
        <p className="text-xs text-muted-foreground">{member.user_email}</p>
      </div>
      <RoleBadge role={member.role as OrgRole} />
    </div>
  )
}

function DomainCard({ domain }: { domain: ApiDomain }) {
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
            <span><span className="text-muted-foreground/50">internal: </span>{domain.internal_subdomain}.{domain.base_domain}</span>
            <span><span className="text-muted-foreground/50">preview: </span>{domain.preview_subdomain}.{domain.base_domain}</span>
            <span><span className="text-muted-foreground/50">mesh: </span>mesh.{domain.base_domain}</span>
          </div>
        </div>
      </div>
    </div>
  )
}


function RoleBadge({ role }: { role: OrgRole }) {
  if (role === "owner") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-500/10">
      <Crown className="h-2.5 w-2.5" />owner
    </Badge>
  )
  if (role === "admin") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
      <Shield className="h-2.5 w-2.5" />admin
    </Badge>
  )
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}
