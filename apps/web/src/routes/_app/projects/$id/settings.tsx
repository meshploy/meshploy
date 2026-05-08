import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Loader2, Pencil, Trash2, X } from "lucide-react"
import { projects as projectsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { inputCls } from "@/components/services/form-primitives"

export const Route = createFileRoute("/_app/projects/$id/settings")({
  component: ProjectSettingsPage,
})

function ProjectSettingsPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/settings" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const navigate = useNavigate()

  const [editingName, setEditingName] = useState(false)
  const [nameInput, setNameInput] = useState("")
  const [deleteConfirm, setDeleteConfirm] = useState("")

  const { data: project } = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId, projectId, token),
    enabled: !!orgId,
  })

  const renameMut = useMutation({
    mutationFn: () => projectsApi.update(orgId, projectId, nameInput.trim(), token),
    onSuccess: (updated) => {
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["projects", orgId] })
      setEditingName(false)
    },
  })

  const deleteMut = useMutation({
    mutationFn: () => projectsApi.delete(orgId, projectId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["projects", orgId] })
      navigate({ to: "/projects" })
    },
  })

  if (!project) return null

  return (
    <div className="p-6 max-w-2xl space-y-8">
      {/* General */}
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-medium">General</h2>
          <p className="text-xs text-muted-foreground mt-0.5">Basic project information.</p>
        </div>

        <div className="rounded-lg border border-border/60 bg-card divide-y divide-border/40">
          {/* Name */}
          <div className="flex items-center justify-between px-4 py-3 gap-4">
            <div className="min-w-0">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">Name</p>
              {editingName ? (
                <div className="flex items-center gap-2">
                  <input
                    className={inputCls + " h-7 text-xs w-48"}
                    value={nameInput}
                    onChange={(e) => setNameInput(e.target.value)}
                    autoFocus
                    onKeyDown={(e) => {
                      if (e.key === "Enter") renameMut.mutate()
                      if (e.key === "Escape") setEditingName(false)
                    }}
                  />
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => renameMut.mutate()}
                    disabled={!nameInput.trim() || renameMut.isPending}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    {renameMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setEditingName(false)}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ) : (
                <p className="text-sm text-foreground">{project.name}</p>
              )}
            </div>
            {!editingName && (
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => { setNameInput(project.name); setEditingName(true) }}
                className="text-muted-foreground/40 hover:text-muted-foreground"
              >
                <Pencil className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>

          {/* Slug (read-only) */}
          <div className="flex items-center justify-between px-4 py-3">
            <div>
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">Slug</p>
              <code className="text-xs font-mono text-foreground">{project.slug}</code>
            </div>
            <span className="text-[10px] text-muted-foreground/40 border border-border/40 px-1.5 py-0.5 rounded">
              K8s namespace
            </span>
          </div>
        </div>
      </section>

      {/* Danger zone */}
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-medium text-destructive/80">Danger zone</h2>
          <p className="text-xs text-muted-foreground mt-0.5">Irreversible actions. Proceed with caution.</p>
        </div>

        <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 space-y-4">
          <div>
            <p className="text-sm font-medium">Delete project</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Permanently deletes the project, all its services, deployments, routes, and secrets.
              The Kubernetes namespace <code className="font-mono">{project.slug}</code> will also be removed.
            </p>
          </div>
          <div className="space-y-2">
            <p className="text-xs text-muted-foreground">
              Type <code className="font-mono text-foreground">{project.slug}</code> to confirm:
            </p>
            <input
              className={inputCls + " max-w-xs"}
              placeholder={project.slug}
              value={deleteConfirm}
              onChange={(e) => setDeleteConfirm(e.target.value)}
            />
          </div>
          <Button
            variant="destructive"
            size="sm"
            className="gap-1.5"
            disabled={deleteConfirm !== project.slug || deleteMut.isPending}
            onClick={() => deleteMut.mutate()}
          >
            {deleteMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
            Delete project
          </Button>
          {deleteMut.isError && (
            <p className="text-xs text-destructive">{(deleteMut.error as Error).message}</p>
          )}
        </div>
      </section>
    </div>
  )
}
