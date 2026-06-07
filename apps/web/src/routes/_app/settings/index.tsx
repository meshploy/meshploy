import { createFileRoute, Link } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Check, Globe, HardDrive, Loader2, Pencil, Plus, X } from "lucide-react"
import { useState } from "react"
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
  type ApiStorageIntegration,
  type ApiSystemBackupConfig,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, inputCls } from "@/components/services/form-primitives"
import { BackupCard } from "@/components/backups/backup-card"
import { RestoreAccordion } from "@/components/backups/restore-accordion"
import { cn } from "@/lib/utils"
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
        <p className="text-sm text-muted-foreground mt-0.5">Manage your organization settings</p>
      </div>

      <GeneralSection org={org} onNameUpdated={(updated) => setCurrentOrg({ id: updated.id, name: updated.name, slug: updated.slug })} />

      <AppearanceSection />

      <PrimaryDomainSection />

      <SystemBackupSection />
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
                  <Button
                    key={color.id}
                    variant="ghost"
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
                  </Button>
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

  const triggerMut = useMutation({
    mutationFn: () => backupsApi.triggerSystem(orgId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["system-backup", orgId] }),
  })

  const [accordionOpen, setAccordionOpen] = useState(false)
  const [restoringKey, setRestoringKey] = useState<string | null>(null)

  const { data: objects, isLoading: objectsLoading } = useQuery({
    queryKey: ["system-backup-objects", orgId],
    queryFn: () => backupsApi.listSystemObjects(orgId, token),
    enabled: accordionOpen && !!cfg,
  })

  const restoreMut = useMutation({
    mutationFn: (key: string) => backupsApi.restoreSystem(orgId, key, token),
    onMutate: (key) => setRestoringKey(key),
    onSettled: () => setRestoringKey(null),
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
        <BackupCard
          config={cfg}
          storageName={storageList.find((s: ApiStorageIntegration) => s.id === cfg.storage_integration_id)?.name ?? cfg.storage_integration_id}
          onTrigger={() => triggerMut.mutate()}
          isTriggerPending={triggerMut.isPending}
          onEdit={() => startEdit(cfg)}
          onToggle={(enabled) => toggleMut.mutate(enabled)}
          isTogglePending={toggleMut.isPending}
          onDelete={() => deleteMut.mutate()}
          isDeletePending={deleteMut.isPending}
          footer={
            <RestoreAccordion
              objects={objects}
              isLoading={objectsLoading}
              onRestore={(key) => restoreMut.mutate(key)}
              isRestorePending={restoreMut.isPending}
              restoringKey={restoringKey}
              onOpenChange={setAccordionOpen}
            />
          }
        />
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
