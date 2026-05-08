import { createFileRoute, useNavigate, useParams, Link } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ArrowLeft, HardDrive, Loader2, Unplug, Link2, Trash2,
  AlertTriangle, AlertCircle, Check, Pencil, X,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  volumes as volumesApi,
  services as servicesApi,
  storage as storageApi,
  type ApiVolume,
  type ApiVolumeMount,
  type ApiVolumeBackupConfig,
  type ApiStorageIntegration,
  ApiError,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { cn, formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/volumes/$volumeId")({
  component: VolumeDetailPage,
})

// ─── Status ───────────────────────────────────────────────────────────────────

const STATUS_STYLES: Record<ApiVolume["status"], string> = {
  pending: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  ready:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
}

const BACKUP_STATUS_DOT: Record<string, string> = {
  pending: "bg-yellow-400 animate-pulse",
  running: "bg-yellow-400 animate-pulse",
  success: "bg-emerald-400",
  failed:  "bg-destructive",
}

// ─── Attachment section ───────────────────────────────────────────────────────

function AttachmentSection({
  volume,
  mount,
  projectId,
  orgId,
  token,
}: {
  volume: ApiVolume
  mount: ApiVolumeMount | undefined
  projectId: string
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [serviceId, setServiceId] = useState("")
  const [mountPath, setMountPath] = useState("")
  const [attachError, setAttachError] = useState<string | null>(null)

  const { data: allServices = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })
  const appServices = allServices.filter((s) => s.type === "application")

  const mountedService = mount
    ? allServices.find((s) => s.id === mount.service_id)
    : undefined

  const detachMut = useMutation({
    mutationFn: () => volumesApi.detach(orgId, projectId, volume.id, mount!.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["volume", orgId, projectId, volume.id] })
      qc.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
    },
  })

  const attachMut = useMutation({
    mutationFn: () =>
      volumesApi.attach(orgId, projectId, volume.id, { service_id: serviceId, mount_path: mountPath }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["volume", orgId, projectId, volume.id] })
      qc.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
      setServiceId("")
      setMountPath("")
      setAttachError(null)
    },
    onError: (err: Error) => setAttachError(err.message),
  })

  return (
    <Section title="Attachment" subtitle="Mount this volume into an application service">
      {/* RWO warning */}
      <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2.5">
        <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
        <p className="text-xs text-amber-300/80">
          PVC volumes use ReadWriteOnce access mode — they can only be mounted by a single service replica.
        </p>
      </div>

      {mount ? (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="flex items-center justify-center w-7 h-7 rounded-md bg-muted border border-border/60 shrink-0">
              <Link2 className="h-3 w-3 text-muted-foreground" />
            </div>
            <div className="min-w-0">
              <p className="text-xs font-medium text-foreground truncate">
                {mountedService?.name ?? mount.service_id}
              </p>
              <p className="text-[11px] font-mono text-muted-foreground truncate">{mount.mount_path}</p>
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs gap-1.5 shrink-0 text-amber-400 border-amber-500/30 hover:bg-amber-500/10 hover:text-amber-400"
            disabled={detachMut.isPending}
            onClick={() => detachMut.mutate()}
          >
            {detachMut.isPending
              ? <Loader2 className="h-3 w-3 animate-spin" />
              : <Unplug className="h-3 w-3" />
            }
            Detach
          </Button>
        </div>
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Service" required>
              <Select value={serviceId} onValueChange={(v) => setServiceId(v ?? "")}>
                <SelectTrigger className={inputCls}>
                  <SelectValue placeholder="Select a service…" />
                </SelectTrigger>
                <SelectContent>
                  {appServices.length === 0
                    ? <SelectItem value="__none" disabled>No application services</SelectItem>
                    : appServices.map((s) => (
                        <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
                      ))
                  }
                </SelectContent>
              </Select>
            </Field>
            <Field label="Mount path" required>
              <input
                className={inputCls}
                placeholder="/data"
                value={mountPath}
                onChange={(e) => setMountPath(e.target.value)}
              />
            </Field>
          </div>

          {attachError && (
            <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />{attachError}
            </div>
          )}

          <Button
            size="sm"
            disabled={!serviceId || !mountPath || attachMut.isPending}
            onClick={() => attachMut.mutate()}
          >
            {attachMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
            Attach volume
          </Button>
        </div>
      )}
    </Section>
  )
}

// ─── Backup section ───────────────────────────────────────────────────────────

function BackupSection({
  volume,
  projectId,
  orgId,
  token,
}: {
  volume: ApiVolume
  projectId: string
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)

  const { data: storageList = [] } = useQuery({
    queryKey: ["storage-integrations", orgId],
    queryFn: () => storageApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId,
  })

  const { data: backupCfg, isLoading: backupLoading } = useQuery<ApiVolumeBackupConfig | null>({
    queryKey: ["volume-backup", orgId, projectId, volume.id],
    queryFn: async () => {
      try {
        return await volumesApi.getBackup(orgId, projectId, volume.id, token)
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null
        throw err
      }
    },
    enabled: !!orgId,
  })

  const deleteBackupMut = useMutation({
    mutationFn: () => volumesApi.deleteBackup(orgId, projectId, volume.id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["volume-backup", orgId, projectId, volume.id] }),
  })

  const showForm = editing || backupCfg === null

  if (backupLoading) {
    return (
      <Section title="Backup" subtitle="Scheduled snapshots to object storage">
        <div className="flex items-center gap-2 text-muted-foreground text-sm py-4">
          <Loader2 className="h-3.5 w-3.5 animate-spin" /><span>Loading…</span>
        </div>
      </Section>
    )
  }

  if (storageList.length === 0) {
    return (
      <Section title="Backup" subtitle="Scheduled snapshots to object storage">
        <div className="rounded-lg border border-dashed border-border/60 py-8 flex flex-col items-center gap-2">
          <HardDrive className="h-6 w-6 text-muted-foreground/30" />
          <p className="text-sm text-muted-foreground">No storage integration configured</p>
          <p className="text-xs text-muted-foreground/60">
            Add an S3-compatible storage integration to enable volume backups.
          </p>
        </div>
      </Section>
    )
  }

  return (
    <Section
      title="Backup"
      subtitle="Scheduled snapshots to object storage"
      action={
        backupCfg && !editing ? (
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" className="h-7 text-xs gap-1" onClick={() => setEditing(true)}>
              <Pencil className="h-3 w-3" /> Edit
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs gap-1 text-destructive hover:text-destructive hover:bg-destructive/10"
              disabled={deleteBackupMut.isPending}
              onClick={() => deleteBackupMut.mutate()}
            >
              {deleteBackupMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
              Remove
            </Button>
          </div>
        ) : undefined
      }
    >
      {backupCfg && !showForm ? (
        <BackupConfigDisplay cfg={backupCfg} storageList={storageList} />
      ) : (
        <BackupConfigForm
          volume={volume}
          projectId={projectId}
          orgId={orgId}
          token={token}
          storageList={storageList}
          existing={backupCfg ?? undefined}
          onSuccess={() => {
            setEditing(false)
            qc.invalidateQueries({ queryKey: ["volume-backup", orgId, projectId, volume.id] })
          }}
          onCancel={backupCfg ? () => setEditing(false) : undefined}
        />
      )}
    </Section>
  )
}

function BackupConfigDisplay({
  cfg,
  storageList,
}: {
  cfg: ApiVolumeBackupConfig
  storageList: ApiStorageIntegration[]
}) {
  const storage = storageList.find((s) => s.id === cfg.storage_integration_id)

  return (
    <div className="rounded-lg border border-border/60 bg-card px-4 py-3.5 space-y-2.5">
      <div className="flex items-center gap-2">
        <code className="text-xs font-mono text-foreground">{cfg.schedule}</code>
        <span
          className={cn(
            "h-1.5 w-1.5 rounded-full shrink-0",
            cfg.enabled ? "bg-emerald-400" : "bg-muted-foreground/30"
          )}
        />
        <span className="text-[11px] text-muted-foreground/60">{cfg.enabled ? "enabled" : "paused"}</span>
      </div>
      <div className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground/60">
        <span>{storage?.name ?? "Unknown storage"}</span>
        <span>·</span>
        <span>{cfg.retention_days}d retention</span>
        {cfg.path_prefix && (
          <>
            <span>·</span>
            <code className="font-mono">{cfg.path_prefix}</code>
          </>
        )}
      </div>
      {cfg.last_backup_status && (
        <div className="flex items-center gap-1.5">
          <div
            className={cn(
              "h-1.5 w-1.5 rounded-full shrink-0",
              BACKUP_STATUS_DOT[cfg.last_backup_status] ?? "bg-muted-foreground/30"
            )}
          />
          <span className="text-[11px] text-muted-foreground/60 capitalize">
            Last: {cfg.last_backup_status}
            {cfg.last_backup_at ? ` · ${new Date(cfg.last_backup_at).toLocaleString()}` : ""}
          </span>
        </div>
      )}
    </div>
  )
}

function BackupConfigForm({
  volume,
  projectId,
  orgId,
  token,
  storageList,
  existing,
  onSuccess,
  onCancel,
}: {
  volume: ApiVolume
  projectId: string
  orgId: string
  token: string
  storageList: ApiStorageIntegration[]
  existing?: ApiVolumeBackupConfig
  onSuccess: () => void
  onCancel?: () => void
}) {
  const [storageId, setStorageId]   = useState(existing?.storage_integration_id ?? storageList[0]?.id ?? "")
  const [schedule,  setSchedule]    = useState(existing?.schedule ?? "0 2 * * *")
  const [retention, setRetention]   = useState(String(existing?.retention_days ?? 30))
  const [prefix,    setPrefix]      = useState(existing?.path_prefix ?? "")
  const [enabled,   setEnabled]     = useState(existing?.enabled ?? true)
  const [error,     setError]       = useState<string | null>(null)

  const selectedStorage = storageList.find((s) => s.id === storageId)

  const mutation = useMutation({
    mutationFn: () =>
      volumesApi.upsertBackup(orgId, projectId, volume.id, {
        storage_integration_id: storageId,
        schedule: schedule.trim(),
        retention_days: parseInt(retention) || 30,
        path_prefix: prefix.trim() || undefined,
        enabled,
      }, token),
    onSuccess,
    onError: (err: Error) => setError(err.message),
  })

  return (
    <div className="rounded-lg border border-border/60 bg-muted/5 p-4 space-y-4">
      <p className="text-xs font-medium text-muted-foreground">
        {existing ? "Edit backup schedule" : "Set up backup schedule"}
      </p>

      <div className="space-y-3">
        <Field label="Storage integration" required>
          <Select value={storageId} onValueChange={(v) => v && setStorageId(v)}>
            <SelectTrigger className={inputCls}>
              <SelectValue>{selectedStorage?.name ?? "Select storage"}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {storageList.map((s) => (
                <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Schedule (cron)" required>
            <input
              value={schedule}
              onChange={(e) => setSchedule(e.target.value)}
              placeholder="0 2 * * *"
              className={cn(inputCls, "font-mono")}
            />
          </Field>
          <Field label="Retention (days)" required>
            <input
              type="number"
              min={1}
              value={retention}
              onChange={(e) => setRetention(e.target.value)}
              className={inputCls}
            />
          </Field>
        </div>

        <Field label="Path prefix">
          <input
            value={prefix}
            onChange={(e) => setPrefix(e.target.value)}
            placeholder={`volumes/${volume.id}`}
            className={cn(inputCls, "font-mono")}
          />
        </Field>

        <label className="flex items-center gap-2.5 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="h-3.5 w-3.5 accent-primary"
          />
          <span className="text-xs text-muted-foreground">Enable backup schedule</span>
        </label>
      </div>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />{error}
        </div>
      )}

      <div className="flex items-center gap-2">
        <Button
          size="sm"
          className="gap-1.5"
          disabled={mutation.isPending || !storageId || !schedule.trim()}
          onClick={() => mutation.mutate()}
        >
          {mutation.isPending
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : <Check className="h-3.5 w-3.5" />
          }
          {existing ? "Save changes" : "Enable backup"}
        </Button>
        {onCancel && (
          <Button size="sm" variant="ghost" onClick={onCancel}>
            <X className="h-3.5 w-3.5 mr-1" />
            Cancel
          </Button>
        )}
      </div>
    </div>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function VolumeDetailPage() {
  const { id: projectId, volumeId } = useParams({ from: "/_app/projects/$id/volumes/$volumeId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const { data: volume, isLoading } = useQuery({
    queryKey: ["volume", orgId, projectId, volumeId],
    queryFn: () => volumesApi.get(orgId, projectId, volumeId, token),
    enabled: !!orgId,
  })

  const deleteMut = useMutation({
    mutationFn: () => volumesApi.delete(orgId, projectId, volumeId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
      navigate({ to: "/projects/$id/volumes", params: { id: projectId } })
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-24 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading…</span>
      </div>
    )
  }

  if (!volume) {
    return (
      <div className="p-6 text-sm text-muted-foreground">Volume not found.</div>
    )
  }

  const mount = volume.mounts?.[0]
  const hasMounts = (volume.mounts?.length ?? 0) > 0

  return (
    <div className="p-6 max-w-2xl space-y-6">
      {/* Back link */}
      <Link
        to="/projects/$id/volumes"
        params={{ id: projectId }}
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-3.5 w-3.5" />
        Back to volumes
      </Link>

      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-muted border border-border/60 shrink-0">
            <HardDrive className="h-5 w-5 text-muted-foreground" />
          </div>
          <div>
            <h1 className="text-base font-semibold text-foreground">{volume.name}</h1>
            <p className="text-xs text-muted-foreground font-mono">{volume.slug}</p>
          </div>
        </div>
        <Badge className={cn("text-[10px] px-1.5 py-0 h-5 border shrink-0 mt-0.5", STATUS_STYLES[volume.status])}>
          {volume.status}
        </Badge>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-3 gap-4 rounded-lg border border-border/60 bg-card px-4 py-3.5">
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Size</p>
          <p className="text-sm text-foreground font-mono">{volume.storage_gb} GB</p>
        </div>
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Attached</p>
          <p className="text-sm text-foreground">{hasMounts ? "Yes" : "No"}</p>
        </div>
        <div>
          <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Created</p>
          <p className="text-sm text-muted-foreground">{formatRelativeTime(new Date(volume.created_at))}</p>
        </div>
      </div>

      {/* Attachment */}
      <AttachmentSection
        volume={volume}
        mount={mount}
        projectId={projectId}
        orgId={orgId}
        token={token}
      />

      {/* Backup */}
      <BackupSection
        volume={volume}
        projectId={projectId}
        orgId={orgId}
        token={token}
      />

      {/* Danger zone */}
      <Section
        title="Danger zone"
        danger
        subtitle="Destructive actions that cannot be undone"
      >
        <div className="flex items-start justify-between gap-4 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3.5">
          <div>
            <p className="text-xs font-medium text-foreground">Delete volume</p>
            <p className="text-[11px] text-muted-foreground mt-0.5">
              {hasMounts
                ? "Detach this volume from all services before deleting."
                : "Permanently removes the PVC and all stored data."}
            </p>
          </div>
          <Button
            variant="destructive"
            size="sm"
            disabled={hasMounts || deleteMut.isPending}
            className="shrink-0"
            onClick={() => deleteMut.mutate()}
          >
            {deleteMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" /> : <Trash2 className="h-3.5 w-3.5 mr-1.5" />}
            Delete
          </Button>
        </div>
        {deleteMut.error && (
          <p className="text-xs text-destructive">{(deleteMut.error as Error).message}</p>
        )}
      </Section>
    </div>
  )
}
