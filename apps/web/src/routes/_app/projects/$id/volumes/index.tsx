import { createFileRoute, useNavigate, useParams, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { HardDrive, Loader2, Plus, ChevronRight } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { volumes as volumesApi, type ApiVolume } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/volumes/")({
  component: VolumesTab,
})

const STATUS_STYLES: Record<ApiVolume["status"], string> = {
  pending: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  ready:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
}

function VolumeCard({ volume, projectId }: { volume: ApiVolume; projectId: string }) {
  const mount = volume.mounts?.[0]

  return (
    <Link
      to="/projects/$id/volumes/$volumeId"
      params={{ id: projectId, volumeId: volume.id }}
      className="group flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4 hover:border-border transition-all"
    >
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
          <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40 group-hover:text-muted-foreground transition-colors" />
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
    </Link>
  )
}

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
