import { createFileRoute, Link } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Crown, Globe, HardDrive, Loader2, Plus, Shield, Trash2, User, Check, Pencil, X } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  orgs as orgsApi,
  domains as domainsApi,
  storage as storageApi,
  backups as backupsApi,
  type ApiDomain,
  type ApiOrgMember,
  type ApiStorageIntegration,
  type ApiSystemBackupConfig,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"
import type { OrgRole } from "@/types"
import { ACCENT_GROUPS, getAccent } from "@/lib/accents"
import { useAccentStore } from "@/store/accent-store"

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

      <AppearanceSection />

      <PrimaryDomainSection />

      <SystemBackupSection />

      <MembersSection />
    </div>
  )
}

function AppearanceSection() {
  const { accentId, setAccent } = useAccentStore()

  return (
    <Section title="Appearance" subtitle="Customize the accent color used across the interface">
      <div className="space-y-4">
        {ACCENT_GROUPS.map((group) => (
          <div key={group.label}>
            <p className="text-[10px] font-medium text-muted-foreground/50 uppercase tracking-wider mb-2">
              {group.label}
            </p>
            <div className="flex flex-wrap gap-2">
              {group.colors.map((color) => {
                const isSelected = color.id === accentId
                return (
                  <button
                    key={color.id}
                    type="button"
                    onClick={() => setAccent(color.id)}
                    title={color.label}
                    className={cn(
                      "flex items-center gap-1.5 h-7 px-2.5 rounded-md text-xs transition-colors border",
                      isSelected
                        ? "bg-muted/60 text-foreground border-border"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/30 border-transparent"
                    )}
                  >
                    <span
                      className="h-3 w-3 rounded-full shrink-0 ring-1 ring-black/20"
                      style={{ background: color.value }}
                    />
                    {color.label}
                    {isSelected && <Check className="h-3 w-3 ml-0.5 shrink-0" />}
                  </button>
                )
              })}
            </div>
          </div>
        ))}
      </div>
    </Section>
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

function SystemBackupSection() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [storageId, setStorageId]   = useState("")
  const [schedule,  setSchedule]    = useState("0 2 * * *")
  const [retention, setRetention]   = useState("30")
  const [prefix,    setPrefix]      = useState("")
  const [error,     setError]       = useState<string | null>(null)

  const { data: storageList = [] } = useQuery({
    queryKey: ["storage-integrations", orgId],
    queryFn: () => storageApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId,
  })

  const { data: cfg, isLoading } = useQuery({
    queryKey: ["system-backup", orgId],
    queryFn: () => backupsApi.getSystem(orgId, token),
    enabled: !!orgId,
  })

  const upsertMut = useMutation({
    mutationFn: () => backupsApi.upsertSystem(orgId, {
      storage_integration_id: storageId,
      schedule: schedule.trim(),
      retention_days: parseInt(retention) || 30,
      path_prefix: prefix.trim() || undefined,
      enabled: true,
    }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["system-backup", orgId] })
      setEditing(false)
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })

  const toggleMut = useMutation({
    mutationFn: (enabled: boolean) => backupsApi.upsertSystem(orgId, {
      storage_integration_id: cfg!.storage_integration_id,
      schedule: cfg!.schedule,
      retention_days: cfg!.retention_days,
      path_prefix: cfg!.path_prefix,
      enabled,
    }, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["system-backup", orgId] }),
  })

  const deleteMut = useMutation({
    mutationFn: () => backupsApi.deleteSystem(orgId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["system-backup", orgId] }),
  })

  function startEdit(existing?: ApiSystemBackupConfig) {
    setStorageId(existing?.storage_integration_id ?? storageList[0]?.id ?? "")
    setSchedule(existing?.schedule ?? "0 2 * * *")
    setRetention(String(existing?.retention_days ?? 30))
    setPrefix(existing?.path_prefix ?? "")
    setError(null)
    setEditing(true)
  }

  const selectedStorage = storageList.find((s: ApiStorageIntegration) => s.id === storageId)

  return (
    <Section
      title="System Backup"
      subtitle="Automated backup of Meshploy's own database to object storage"
      action={
        !isLoading && !editing && cfg && (
          <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs" onClick={() => startEdit(cfg)}>
            <Pencil className="h-3 w-3" /> Edit
          </Button>
        )
      }
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" /><span>Loading…</span>
        </div>
      ) : storageList.length === 0 ? (
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          <HardDrive className="h-4 w-4 text-muted-foreground/40 shrink-0" />
          <span>No storage integrations configured. <Link to="/integrations/new" search={{ category: "storage" }} className="text-primary hover:underline">Add one</Link> to enable system backups.</span>
        </div>
      ) : editing ? (
        <div className="space-y-3">
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground/60">Storage</label>
            <Select value={storageId} onValueChange={(v) => v && setStorageId(v)}>
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue>{selectedStorage?.name ?? "Select storage"}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {storageList.map((s: ApiStorageIntegration) => (
                  <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground/60">Schedule (cron)</label>
              <input value={schedule} onChange={(e) => setSchedule(e.target.value)}
                placeholder="0 2 * * *"
                className={cn(inputCls, "font-mono")} />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-muted-foreground/60">Retention (days)</label>
              <input type="number" min={1} value={retention} onChange={(e) => setRetention(e.target.value)}
                className={inputCls} />
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground/60">Path prefix (optional)</label>
            <input value={prefix} onChange={(e) => setPrefix(e.target.value)}
              placeholder="e.g. meshploy/system"
              className={cn(inputCls, "font-mono")} />
          </div>
          {error && (
            <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />{error}
            </div>
          )}
          <div className="flex items-center gap-2">
            <Button size="sm" onClick={() => upsertMut.mutate()} disabled={upsertMut.isPending || !storageId || !schedule.trim()}>
              {upsertMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" /> : null}
              {cfg ? "Save changes" : "Enable backup"}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => { setEditing(false); setError(null) }}>Cancel</Button>
          </div>
        </div>
      ) : cfg ? (
        <div className="rounded-lg border border-border/60 px-4 py-3.5 space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className={cn("h-2 w-2 rounded-full shrink-0", cfg.enabled ? "bg-emerald-400" : "bg-muted-foreground/30")} />
              <span className="text-sm font-medium">{cfg.enabled ? "Active" : "Paused"}</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => toggleMut.mutate(!cfg.enabled)}
                disabled={toggleMut.isPending}
                className="p-1.5 text-xs text-muted-foreground/60 hover:text-foreground transition-colors disabled:opacity-30"
                title={cfg.enabled ? "Pause" : "Resume"}
              >
                {toggleMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : cfg.enabled ? <X className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />}
              </button>
              <button
                onClick={() => deleteMut.mutate()}
                disabled={deleteMut.isPending}
                className="p-1.5 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-30"
                title="Remove backup config"
              >
                {deleteMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
              </button>
            </div>
          </div>
          <div className="text-xs text-muted-foreground/70 space-y-0.5">
            <div className="flex items-center gap-3">
              <span><span className="text-muted-foreground/40">schedule</span> <code className="font-mono text-foreground/80">{cfg.schedule}</code></span>
              <span><span className="text-muted-foreground/40">retention</span> {cfg.retention_days}d</span>
            </div>
            <div className="flex items-center gap-3">
              <span><span className="text-muted-foreground/40">storage</span> {storageList.find((s: ApiStorageIntegration) => s.id === cfg.storage_integration_id)?.name ?? cfg.storage_integration_id}</span>
              {cfg.path_prefix && <span><span className="text-muted-foreground/40">prefix</span> <code className="font-mono">{cfg.path_prefix}</code></span>}
            </div>
            {cfg.last_backup_status && (
              <div className="flex items-center gap-1.5 pt-1">
                <span className={cn("h-1.5 w-1.5 rounded-full shrink-0", {
                  "bg-yellow-400 animate-pulse": cfg.last_backup_status === "running" || cfg.last_backup_status === "pending",
                  "bg-emerald-400": cfg.last_backup_status === "success",
                  "bg-destructive": cfg.last_backup_status === "failed",
                })} />
                <span className="capitalize">{cfg.last_backup_status}</span>
                {cfg.last_backup_at && <span className="text-muted-foreground/40">· {new Date(cfg.last_backup_at).toLocaleString()}</span>}
              </div>
            )}
          </div>
        </div>
      ) : (
        <div className="flex items-center gap-3">
          <p className="text-sm text-muted-foreground flex-1">No backup configured for the system database.</p>
          <Button size="sm" variant="outline" className="gap-1.5 shrink-0" onClick={() => startEdit()}>
            <Plus className="h-3.5 w-3.5" /> Configure
          </Button>
        </div>
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
