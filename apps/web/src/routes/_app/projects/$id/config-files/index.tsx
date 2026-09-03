import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { FileCog, Loader2, Plus, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { configFiles as configFilesApi, type ApiConfigFile } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { StackPill, useStackNames } from "@/components/stacks/stack-pill"
import { livePoll } from "@/lib/live-poll"

export const Route = createFileRoute("/_app/projects/$id/config-files/")({
  component: ConfigFilesPage,
})

function ConfigFilesPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/config-files/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const stackNames = useStackNames(orgId, projectId)

  const { data, isLoading } = useQuery({
    queryKey: ["config-files", orgId, projectId],
    queryFn: () => configFilesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
    refetchInterval: livePoll<{ files: ApiConfigFile[] }>(() => false),
  })
  const files = data?.files ?? []

  // Creating and editing both have their own page now; the list keeps only the
  // one action that needs no form -- deleting a file nothing mounts.
  const [deleteTarget, setDeleteTarget] = useState<ApiConfigFile | null>(null)

  const remove = useMutation({
    mutationFn: () => configFilesApi.delete(orgId!, projectId, deleteTarget!.id, token),
    onSuccess: () => {
      setDeleteTarget(null)
      qc.invalidateQueries({ queryKey: ["config-files", orgId, projectId] })
    },
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-sm font-medium">Config files</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Files mounted into a service at a path — for software configured by file rather than by
            environment variable. Stored encrypted; the contents are never shown again after saving.
          </p>
        </div>
        <Button
          size="sm"
          className="gap-1.5 shrink-0"
          onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "config-file" } })}
        >
          <Plus className="h-3.5 w-3.5" />
          New file
        </Button>
      </div>

      {isLoading ? (
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      ) : files.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-3 py-20 text-center">
          <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-muted/50">
            <FileCog className="h-5 w-5 text-muted-foreground/60" />
          </div>
          <div>
            <p className="text-sm font-medium text-foreground">No config files yet</p>
            <p className="text-xs text-muted-foreground mt-1 max-w-xs">
              Create a file and attach it to a service from that service's Config tab.
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={() => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "config-file" } })}
          >
            <Plus className="h-3.5 w-3.5 mr-1.5" />
            New Config File
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {files.map((f) => (
            <div
              key={f.id}
              className="flex items-center gap-3 px-4 py-3 hover:bg-muted/20 transition-colors cursor-pointer"
              onClick={() => navigate({ to: "/projects/$id/config-files/$fileId", params: { id: projectId, fileId: f.id } })}
            >
              <FileCog className="h-3.5 w-3.5 text-muted-foreground/60 shrink-0" />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-medium text-foreground">{f.name}</span>
                  <StackPill stackId={f.stack_id} stackNames={stackNames} orgId={orgId} projectId={projectId} />
                </div>
                <code className="text-[11px] font-mono text-muted-foreground/60 truncate block">{f.path}</code>
              </div>
              {f.services.length > 0 && (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0">
                  {f.services.length === 1 ? f.services[0] : `${f.services.length} services`}
                </Badge>
              )}
              <span className="text-[11px] text-muted-foreground/60 shrink-0">{f.size} B</span>
              <Button
                size="icon"
                variant="ghost"
                className="h-6 w-6 shrink-0 text-muted-foreground hover:text-destructive"
                aria-label={`Delete ${f.name}`}
                onClick={(e) => { e.stopPropagation(); setDeleteTarget(f) }}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <Dialog open={Boolean(deleteTarget)} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this config file?</DialogTitle>
            <DialogDescription>
              {deleteTarget?.services.length
                ? `${deleteTarget.name} is mounted by ${deleteTarget.services.join(", ")}. Detach it from each service first — deleting a file something still mounts is refused.`
                : `${deleteTarget?.name} is not mounted by any service. Deleting it cannot be undone.`}
            </DialogDescription>
          </DialogHeader>
          {remove.isError && <p className="text-xs text-destructive">{(remove.error as Error).message}</p>}
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button
              variant="destructive"
              size="sm"
              className="gap-1.5"
              onClick={() => remove.mutate()}
              disabled={remove.isPending || (deleteTarget?.services.length ?? 0) > 0}
            >
              {remove.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
