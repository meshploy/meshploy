import { SettingsWorkspace } from "@/components/layout/settings-workspace"
import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Loader2, Pencil, Trash2, X } from "lucide-react"
import { projects as projectsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
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
    <div className="console-page settings-page space-y-6">
      <div><h1>Project settings</h1><p className="mt-2 text-sm text-muted-foreground">Manage project identity and lifecycle.</p></div>
      <SettingsWorkspace sections={[["project-general", "General"], ["project-danger", "Danger zone"]]}>
      {/* General */}
      <section id="project-general" className="console-section space-y-4">
        <div>
          <h2 className="text-sm font-medium">General</h2>
          <p className="text-xs text-muted-foreground mt-0.5">Basic project information.</p>
        </div>

        <div className="space-y-4">
          {/* The field owns the row, with its actions beside it. */}
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Name</label>
            {editingName ? (
              <div className="flex items-center gap-2">
                <Input
                  value={nameInput}
                  onChange={(e) => setNameInput(e.target.value)}
                  className="h-9 text-sm"
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === "Enter") renameMut.mutate()
                    if (e.key === "Escape") setEditingName(false)
                  }}
                />
                <Button
                  size="icon"
                  variant="ghost"
                  className="h-9 w-9 shrink-0"
                  onClick={() => renameMut.mutate()}
                  disabled={!nameInput.trim() || renameMut.isPending}
                >
                  {renameMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5 text-primary" />}
                </Button>
                <Button size="icon" variant="ghost" className="h-9 w-9 shrink-0" onClick={() => setEditingName(false)}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            ) : (
              <div className="flex items-center gap-2">
                <div className="flex items-center h-9 px-3 rounded-md border border-border/60 bg-muted/20 flex-1 min-w-0">
                  <span className="text-sm truncate">{project.name}</span>
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  className="h-9 w-9 shrink-0"
                  onClick={() => { setNameInput(project.name); setEditingName(true) }}
                >
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
              </div>
            )}
          </div>

          {/* Slug (read-only) */}
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Slug</label>
            <div className="flex items-center gap-2">
              <div className="flex items-center justify-between h-9 px-3 rounded-md border border-border/60 bg-muted/20 flex-1 min-w-0">
                <code className="text-xs font-mono text-foreground truncate">{project.slug}</code>
                <span className="text-[11px] text-muted-foreground/40 border border-border/40 px-1.5 py-0.5 rounded shrink-0">
                  K8s namespace
                </span>
              </div>
              {/* Keeps the field the same width as the name's, which has a button. */}
              <span className="h-9 w-9 shrink-0" aria-hidden />
            </div>
          </div>
        </div>
      </section>

      {/* Danger zone */}
      <section id="project-danger" className="console-section console-section-danger space-y-4">
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
      </SettingsWorkspace>
    </div>
  )
}
