import { createFileRoute, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Check, HardDrive, Loader2, Plus } from "lucide-react"
import {
  backups as backupsApi,
  storage as storageApi,
  type ApiBackupConfig,
  type ApiStorageIntegration,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import { inputCls } from "@/components/services/form-primitives"
import { BackupCard } from "@/components/backups/backup-card"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/services/$serviceId/backups")({
  component: ServiceBackupsPage,
})

function ServiceBackupsPage() {
  const { id: projectId, serviceId } = useParams({ from: "/_app/projects/$id/services/$serviceId/backups" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [showForm, setShowForm] = useState(false)

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["backups", orgId, projectId, serviceId],
    queryFn: () => backupsApi.list(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const { data: storageList = [] } = useQuery({
    queryKey: ["storage-integrations", orgId],
    queryFn: () => storageApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId,
  })

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-medium">Backups</h2>
          <p className="text-xs text-muted-foreground mt-0.5">Scheduled database backups to object storage</p>
        </div>
        {!showForm && storageList.length > 0 && (
          <Button size="sm" className="gap-1.5" onClick={() => setShowForm(true)}>
            <Plus className="h-3.5 w-3.5" /> Add backup
          </Button>
        )}
      </div>

      {showForm && (
        <BackupForm
          orgId={orgId}
          projectId={projectId}
          serviceId={serviceId}
          token={token}
          storageList={storageList}
          onSuccess={() => {
            setShowForm(false)
            qc.invalidateQueries({ queryKey: ["backups", orgId, projectId, serviceId] })
          }}
          onCancel={() => setShowForm(false)}
        />
      )}

      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" /><span>Loading…</span>
        </div>
      ) : storageList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-10 flex flex-col items-center gap-2">
          <HardDrive className="h-7 w-7 text-muted-foreground/30" />
          <p className="text-sm text-muted-foreground">No storage configured</p>
          <p className="text-xs text-muted-foreground/60">
            Add an object storage integration first to enable backups.
          </p>
        </div>
      ) : list.length === 0 && !showForm ? (
        <div className="rounded-lg border border-dashed border-border/60 py-10 flex flex-col items-center gap-2">
          <HardDrive className="h-7 w-7 text-muted-foreground/30" />
          <p className="text-sm text-muted-foreground">No backups configured</p>
          <p className="text-xs text-muted-foreground/60">Set up a schedule to automatically back up this database.</p>
          <Button size="sm" variant="outline" className="gap-1.5 mt-1" onClick={() => setShowForm(true)}>
            <Plus className="h-3.5 w-3.5" /> Add backup
          </Button>
        </div>
      ) : (
        <div className="space-y-3">
          {list.map((cfg) => (
            <BackupItem
              key={cfg.id}
              config={cfg}
              storageList={storageList}
              orgId={orgId}
              projectId={projectId}
              serviceId={serviceId}
              token={token}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function BackupItem({ config, storageList, orgId, projectId, serviceId, token }: {
  config: ApiBackupConfig
  storageList: ApiStorageIntegration[]
  orgId: string
  projectId: string
  serviceId: string
  token: string
}) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [schedule, setSchedule] = useState(config.schedule)
  const [retention, setRetention] = useState(String(config.retention_days))

  const invalidate = () => qc.invalidateQueries({ queryKey: ["backups", orgId, projectId, serviceId] })

  const updateMut = useMutation({
    mutationFn: (patch: { schedule?: string; retention_days?: number; enabled?: boolean }) =>
      backupsApi.update(orgId, projectId, serviceId, config.id, patch, token),
    onSuccess: () => { invalidate(); setEditing(false) },
  })

  const deleteMut = useMutation({
    mutationFn: () => backupsApi.delete(orgId, projectId, serviceId, config.id, token),
    onSuccess: invalidate,
  })

  const triggerMut = useMutation({
    mutationFn: () => backupsApi.trigger(orgId, projectId, serviceId, config.id, token),
    onSuccess: invalidate,
  })

  const storageName = storageList.find((s) => s.id === config.storage_integration_id)?.name ?? "Unknown storage"

  if (editing) {
    return (
      <div className="rounded-lg border border-border/60 px-4 py-3.5 space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground/60">Schedule (cron)</label>
            <input value={schedule} onChange={(e) => setSchedule(e.target.value)}
              className={cn(inputCls, "h-8 text-xs font-mono")} />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground/60">Retention (days)</label>
            <input type="number" min={1} value={retention} onChange={(e) => setRetention(e.target.value)}
              className={cn(inputCls, "h-8 text-xs")} />
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" className="h-7 text-xs gap-1" onClick={() => updateMut.mutate({ schedule: schedule.trim(), retention_days: parseInt(retention) || 30 })} disabled={updateMut.isPending || !schedule.trim()}>
            {updateMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />} Save
          </Button>
          <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => { setEditing(false); setSchedule(config.schedule); setRetention(String(config.retention_days)) }}>
            Cancel
          </Button>
        </div>
      </div>
    )
  }

  return (
    <BackupCard
      config={config}
      storageName={storageName}
      onTrigger={() => triggerMut.mutate()}
      isTriggerPending={triggerMut.isPending}
      onEdit={() => setEditing(true)}
      onToggle={(enabled) => updateMut.mutate({ enabled })}
      isTogglePending={updateMut.isPending}
      onDelete={() => deleteMut.mutate()}
      isDeletePending={deleteMut.isPending}
    />
  )
}

function BackupForm({ orgId, projectId, serviceId, token, storageList, onSuccess, onCancel }: {
  orgId: string
  projectId: string
  serviceId: string
  token: string
  storageList: ApiStorageIntegration[]
  onSuccess: () => void
  onCancel: () => void
}) {
  const [storageId, setStorageId] = useState(storageList[0]?.id ?? "")
  const [schedule,  setSchedule]  = useState("0 2 * * *")
  const [retention, setRetention] = useState("30")
  const [prefix,    setPrefix]    = useState("")
  const [error,     setError]     = useState<string | null>(null)

  const selectedStorage = storageList.find((s) => s.id === storageId)

  const mutation = useMutation({
    mutationFn: () => backupsApi.create(orgId, projectId, serviceId, {
      storage_integration_id: storageId,
      schedule: schedule.trim(),
      retention_days: parseInt(retention) || 30,
      path_prefix: prefix.trim() || undefined,
    }, token),
    onSuccess,
    onError: (err: Error) => setError(err.message),
  })

  return (
    <div className="rounded-lg border border-border/60 bg-muted/10 p-4 space-y-4">
      <p className="text-xs font-medium text-muted-foreground">New backup schedule</p>

      <div className="space-y-3">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-muted-foreground/60">Storage</label>
          <Select value={storageId} onValueChange={(v) => v && setStorageId(v)}>
            <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
              <SelectValue>{selectedStorage?.name ?? "Select storage"}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {storageList.map((s) => (
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
              className={cn(inputCls, "h-8 text-xs font-mono")} />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground/60">Retention (days)</label>
            <input type="number" min={1} value={retention} onChange={(e) => setRetention(e.target.value)}
              className={cn(inputCls, "h-8 text-xs")} />
          </div>
        </div>

        <div className="flex flex-col gap-1">
          <label className="text-xs text-muted-foreground/60">Path prefix (optional)</label>
          <input value={prefix} onChange={(e) => setPrefix(e.target.value)}
            placeholder="e.g. prod/postgres"
            className={cn(inputCls, "h-8 text-xs font-mono")} />
        </div>
      </div>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />{error}
        </div>
      )}

      <div className="flex items-center gap-2">
        <Button size="sm" className="h-7 text-xs gap-1" onClick={() => mutation.mutate()} disabled={mutation.isPending || !storageId || !schedule.trim()}>
          {mutation.isPending && <Loader2 className="h-3 w-3 animate-spin" />}
          Create backup
        </Button>
        <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={onCancel}>Cancel</Button>
      </div>
    </div>
  )
}
