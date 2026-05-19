import type { ReactNode } from "react"
import { Loader2, Pencil, Play, Trash2, X, Check } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface BackupCardConfig {
  schedule: string
  retention_days: number
  path_prefix: string
  enabled: boolean
  last_backup_at: string | null
  last_backup_status: "pending" | "running" | "success" | "failed" | null
}

interface BackupCardProps {
  config: BackupCardConfig
  storageName: string
  onTrigger?: () => void
  isTriggerPending?: boolean
  onEdit?: () => void
  onToggle?: (enabled: boolean) => void
  isTogglePending?: boolean
  onDelete?: () => void
  isDeletePending?: boolean
  footer?: ReactNode
}

const STATUS_DOT: Record<string, string> = {
  pending: "bg-yellow-400 animate-pulse",
  running: "bg-yellow-400 animate-pulse",
  success: "bg-emerald-400",
  failed:  "bg-destructive",
}

export function BackupCard({
  config,
  storageName,
  onTrigger,
  isTriggerPending,
  onEdit,
  onToggle,
  isTogglePending,
  onDelete,
  isDeletePending,
  footer,
}: BackupCardProps) {
  return (
    <div className="rounded-lg border border-border/60 px-4 py-3.5 space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className={cn("h-2 w-2 rounded-full shrink-0", config.enabled ? "bg-emerald-400" : "bg-muted-foreground/30")} />
          <span className="text-sm font-medium">{config.enabled ? "Active" : "Paused"}</span>
        </div>
        <div className="flex items-center gap-0.5">
          {onTrigger && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onTrigger}
              disabled={isTriggerPending}
              title="Run now"
              className="text-muted-foreground/40 hover:text-foreground"
            >
              {isTriggerPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
            </Button>
          )}
          {onEdit && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onEdit}
              title="Edit"
              className="text-muted-foreground/40 hover:text-foreground"
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>
          )}
          {onToggle && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onToggle(!config.enabled)}
              disabled={isTogglePending}
              title={config.enabled ? "Pause" : "Resume"}
              className="text-muted-foreground/40 hover:text-foreground"
            >
              {isTogglePending
                ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                : config.enabled ? <X className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />
              }
            </Button>
          )}
          {onDelete && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onDelete}
              disabled={isDeletePending}
              title="Delete"
              className="text-muted-foreground/40 hover:text-destructive"
            >
              {isDeletePending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
            </Button>
          )}
        </div>
      </div>

      <div className="text-xs text-muted-foreground/70 space-y-0.5">
        <div className="flex items-center gap-4">
          <span><span className="text-muted-foreground/40">schedule</span> <code className="font-mono text-foreground/80">{config.schedule}</code></span>
          <span><span className="text-muted-foreground/40">retention</span> {config.retention_days}d</span>
        </div>
        <div className="flex items-center gap-4">
          <span><span className="text-muted-foreground/40">storage</span> {storageName}</span>
          {config.path_prefix && (
            <span><span className="text-muted-foreground/40">prefix</span> <code className="font-mono">{config.path_prefix}</code></span>
          )}
        </div>
        {config.last_backup_status && (
          <div className="flex items-center gap-1.5 pt-0.5">
            <span className={cn("h-1.5 w-1.5 rounded-full shrink-0", STATUS_DOT[config.last_backup_status] ?? "bg-muted-foreground/30")} />
            <span className="capitalize">{config.last_backup_status}</span>
            {config.last_backup_at && (
              <span className="text-muted-foreground/40">· {new Date(config.last_backup_at).toLocaleString()}</span>
            )}
          </div>
        )}
      </div>
      {footer}
    </div>
  )
}
