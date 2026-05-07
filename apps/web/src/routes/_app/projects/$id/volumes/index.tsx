import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { HardDrive, Loader2, Plus, Trash2, Unplug, AlertTriangle, Link2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { volumes as volumesApi, services as servicesApi, type ApiVolume, type ApiVolumeMount } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import { inputCls, Field } from "@/components/services/form-primitives"

export const Route = createFileRoute("/_app/projects/$id/volumes/")({
  component: VolumesTab,
})

// ─── Status styles ────────────────────────────────────────────────────────────

const STATUS_STYLES: Record<ApiVolume["status"], string> = {
  pending: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  ready:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
}

// ─── Attach dialog ────────────────────────────────────────────────────────────

function AttachDialog({
  volume,
  projectId,
  onClose,
}: {
  volume: ApiVolume
  projectId: string
  onClose: () => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const [serviceId, setServiceId] = useState("")
  const [mountPath, setMountPath] = useState("")

  const { data: allServices = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })
  const appServices = allServices.filter((s) => s.type === "application")

  const attachMutation = useMutation({
    mutationFn: () =>
      volumesApi.attach(orgId, projectId, volume.id, { service_id: serviceId, mount_path: mountPath }, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
      onClose()
    },
  })

  return (
    <DialogContent className="max-w-md">
      <DialogHeader>
        <DialogTitle>Attach volume</DialogTitle>
        <DialogDescription>
          Mount <strong>{volume.name}</strong> into an application service.
        </DialogDescription>
      </DialogHeader>

      {/* Replica warning */}
      <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2.5">
        <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
        <p className="text-xs text-amber-300/80">
          RWO volumes can only be used with a single replica. The service will be scaled down to 1 replica automatically.
        </p>
      </div>

      <div className="space-y-4 pt-1">
        <Field label="Service" required>
          <Select value={serviceId} onValueChange={(v) => setServiceId(v ?? "")}>
            <SelectTrigger className={inputCls}>
              <SelectValue placeholder="Select a service…" />
            </SelectTrigger>
            <SelectContent>
              {appServices.map((s) => (
                <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
              ))}
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

        {attachMutation.error && (
          <p className="text-xs text-destructive">{(attachMutation.error as Error).message}</p>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
          <Button
            size="sm"
            disabled={!serviceId || !mountPath || attachMutation.isPending}
            onClick={() => attachMutation.mutate()}
          >
            {attachMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" /> : null}
            Attach
          </Button>
        </div>
      </div>
    </DialogContent>
  )
}

// ─── Volume card ──────────────────────────────────────────────────────────────

function VolumeCard({ volume, projectId }: { volume: ApiVolume; projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const queryKey = ["volumes", orgId, projectId]
  const [showAttach, setShowAttach] = useState(false)

  const mount: ApiVolumeMount | undefined = volume.mounts?.[0]

  const deleteMutation = useMutation({
    mutationFn: () => volumesApi.delete(orgId, projectId, volume.id, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })

  const detachMutation = useMutation({
    mutationFn: () =>
      volumesApi.detach(orgId, projectId, volume.id, mount!.id, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })

  return (
    <>
      <div className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4 hover:border-border transition-all">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2.5">
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted border border-border/60 shrink-0">
              <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
            </div>
            <div>
              <p className="text-sm font-semibold text-foreground leading-tight">{volume.name}</p>
              <p className="text-[11px] text-muted-foreground font-mono">{volume.slug}</p>
            </div>
          </div>

          <div className="flex items-center gap-1.5">
            <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border shrink-0 ${STATUS_STYLES[volume.status]}`}>
              {volume.status}
            </Badge>
            {!mount && (
              <Tooltip>
                <TooltipTrigger render={<button
                  onClick={() => setShowAttach(true)}
                  className="h-7 w-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors"
                />}>
                  <Link2 className="h-3.5 w-3.5" />
                </TooltipTrigger>
                <TooltipContent>Attach to service</TooltipContent>
              </Tooltip>
            )}
            {mount && (
              <Tooltip>
                <TooltipTrigger render={<button
                  onClick={() => detachMutation.mutate()}
                  disabled={detachMutation.isPending}
                  className="h-7 w-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-amber-400 hover:bg-amber-500/10 transition-colors"
                />}>
                  {detachMutation.isPending
                    ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    : <Unplug className="h-3.5 w-3.5" />
                  }
                </TooltipTrigger>
                <TooltipContent>Detach</TooltipContent>
              </Tooltip>
            )}
            <Tooltip>
              <TooltipTrigger render={<button
                onClick={() => deleteMutation.mutate()}
                disabled={deleteMutation.isPending || !!mount}
                className="h-7 w-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
              />}>
                {deleteMutation.isPending
                  ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  : <Trash2 className="h-3.5 w-3.5" />
                }
              </TooltipTrigger>
              <TooltipContent>{mount ? "Detach before deleting" : "Delete volume"}</TooltipContent>
            </Tooltip>
          </div>
        </div>

        <div className="border-t border-border/40 pt-3 grid grid-cols-3 gap-x-4 gap-y-1.5">
          <div>
            <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Size</p>
            <p className="text-[11px] text-foreground font-mono">{volume.storage_gb} GB</p>
          </div>
          <div>
            <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Mount</p>
            <p className="text-[11px] font-mono text-muted-foreground truncate">
              {mount ? mount.mount_path : "—"}
            </p>
          </div>
          <div>
            <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Created</p>
            <p className="text-[11px] text-muted-foreground">{formatRelativeTime(new Date(volume.created_at))}</p>
          </div>
        </div>
      </div>

      <Dialog open={showAttach} onOpenChange={(o) => !o && setShowAttach(false)}>
        <AttachDialog volume={volume} projectId={projectId} onClose={() => setShowAttach(false)} />
      </Dialog>
    </>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function VolumesTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/volumes/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()

  const { data: volumeList = [], isLoading } = useQuery({
    queryKey: ["volumes", orgId, projectId],
    queryFn: () => volumesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-24 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading volumes…</span>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">Volumes</h2>
        <Button
          size="sm"
          onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "volume" } })}
        >
          <Plus className="h-3.5 w-3.5 mr-1.5" />
          New Volume
        </Button>
      </div>

      {volumeList.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-3 py-20 text-center">
          <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-muted/50">
            <HardDrive className="h-5 w-5 text-muted-foreground/60" />
          </div>
          <div>
            <p className="text-sm font-medium text-foreground">No volumes yet</p>
            <p className="text-xs text-muted-foreground mt-1 max-w-xs">
              Create a persistent volume and attach it to an application service.
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "volume" } })}
          >
            <Plus className="h-3.5 w-3.5 mr-1.5" />
            New Volume
          </Button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {volumeList.map((v) => (
            <VolumeCard key={v.id} volume={v} projectId={projectId} />
          ))}
        </div>
      )}
    </div>
  )
}
