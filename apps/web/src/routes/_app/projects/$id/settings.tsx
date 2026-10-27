import { SettingsWorkspace } from "@/components/layout/settings-workspace"
import { createFileRoute, Link, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Eraser, Loader2, Pencil, Trash2, X } from "lucide-react"
import { TermInfo } from "@/help/term"
import { orgs as orgsApi, projects as projectsApi, type ApiProject } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useIsAdmin, useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

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
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["projects", orgId] })
      setEditingName(false)
    },
  })

  const clearCache = useMutation({
    mutationFn: () => projectsApi.clearBuildCache(orgId, projectId, token),
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
      <SettingsWorkspace sections={[["project-general", "General"], ["project-isolation", "Isolation"], ["project-build-cache", "Build cache"], ["project-danger", "Danger zone"]]}>
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

      <IsolationSection orgId={orgId} project={project} />

      {/* The project's build cache: one for all its services, trimmed after
          every build to the server's limit, and cleared here at any time. */}
      <section id="project-build-cache" className="console-section space-y-4">
        <div>
          <h2 className="flex items-center gap-1 text-sm font-medium">
            Build cache
            <TermInfo id="services.build-cache" />
          </h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Layers kept from earlier builds, so the next one is faster. Shared by every service in this project, and
            trimmed back to the server&apos;s limit after each build. Clear it to free the space now, or to force a
            clean build; the next build is slower while it fills again.
          </p>
        </div>
        {clearCache.isError && <p className="text-xs text-destructive">{(clearCache.error as Error).message}</p>}
        {clearCache.isSuccess && <p className="text-xs text-emerald-400">Cleared. The next build starts fresh.</p>}
        <Button size="sm" variant="outline" className="gap-1.5" disabled={clearCache.isPending} onClick={() => clearCache.mutate()}>
          {clearCache.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Eraser className="h-3.5 w-3.5" />}
          Clear build cache
        </Button>
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

// How this project, or this level, is kept apart from the rest of the
// cluster: one row per kind of isolation. Network follows the organization's
// switch unless opened here as an exception. Only an owner or admin may open
// one: it undoes, here, a choice the organization made.
function IsolationSection({ orgId, project }: { orgId: string; project: ApiProject }) {
  const token = useAuthStore((s) => s.token)!
  const isAdmin = useIsAdmin()
  const qc = useQueryClient()
  const { data: org } = useQuery({
    queryKey: ["org", orgId],
    queryFn: () => orgsApi.get(orgId, token),
  })
  const setOpen = useMutation({
    mutationFn: (open: boolean) => projectsApi.setNetworkOpen(orgId, project.id, open, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["project", orgId, project.id] }),
  })

  const orgIsolated = !!org?.network_isolation
  const isolated = orgIsolated && !project.network_open
  const what = project.parent_project_id ? "this level" : "this project"

  return (
    <section id="project-isolation" className="console-section space-y-4">
      <div>
        <h2 className="text-sm font-medium">Isolation</h2>
        <p className="text-xs text-muted-foreground mt-0.5">How {what} is kept apart from the rest of the cluster.</p>
      </div>
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <p className="flex items-center gap-1.5 text-sm font-medium">
            Network
            <TermInfo id="environments.network-isolation" />
            <span className={cn(
              "rounded-full border px-1.5 py-0.5 text-[11px] font-normal",
              isolated ? "border-emerald-500/20 bg-emerald-500/15 text-emerald-400" : "border-border/60 text-muted-foreground",
            )}>
              {isolated ? "Isolated" : "Open to every project"}
            </span>
          </p>
          <p className="text-xs text-muted-foreground leading-relaxed">
            {!orgIsolated ? (
              <>
                Network isolation is off for the organization, so every project is open.{" "}
                <Link to="/settings" hash="isolation" className="text-primary hover:underline">Turn it on in Settings</Link>.
              </>
            ) : isolated ? (
              <>Who inside the cluster may reach {what}&apos;s services: only its own services, its levels, projects that use its services&apos; published variables, and the machines. Switch off to open {what} as an exception.</>
            ) : (
              <>Opened as an exception: any pod in the cluster may reach it, while the organization&apos;s other projects stay isolated.</>
            )}
          </p>
          {orgIsolated && !isAdmin && <p className="text-xs text-muted-foreground">Only an owner or admin can change this.</p>}
          {setOpen.isError && <p className="text-xs text-destructive">{(setOpen.error as Error).message}</p>}
        </div>
        <div className="shrink-0 pt-0.5">
          {setOpen.isPending
            ? <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
            : <Switch
                checked={isolated}
                disabled={!orgIsolated || !isAdmin}
                onCheckedChange={(v) => setOpen.mutate(!v)}
                aria-label={`Isolate ${what} on the network`}
              />}
        </div>
      </div>
      <SandboxRow orgId={orgId} project={project} what={what} />
    </section>
  )
}

// Whether this project's, or level's, apps run on gVisor. Switching moves the
// running ones at once, so it asks first.
function SandboxRow({ orgId, project, what }: { orgId: string; project: ApiProject; what: string }) {
  const token = useAuthStore((s) => s.token)!
  const isAdmin = useIsAdmin()
  const qc = useQueryClient()
  const [asking, setAsking] = useState<boolean | null>(null)
  const { data: state } = useQuery({
    queryKey: ["project-sandbox", orgId, project.id],
    queryFn: () => projectsApi.sandbox(orgId, project.id, token),
  })
  const { data: overview } = useQuery({
    queryKey: ["org-sandbox", orgId],
    queryFn: () => orgsApi.sandbox(orgId, token),
  })
  const set = useMutation({
    mutationFn: (on: boolean) => projectsApi.setSandbox(orgId, project.id, on, token),
    onSuccess: (updated) => {
      qc.setQueryData(["project-sandbox", orgId, project.id], updated)
      qc.invalidateQueries({ queryKey: ["org-sandbox", orgId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, project.id] })
      setAsking(null)
    },
  })

  const on = !!state?.on
  const noNode = overview !== undefined && overview.nodes.length === 0
  const locked = !!state?.required

  return (
    <div className="flex items-start justify-between gap-4 border-t border-border/60 pt-4">
      <div className="space-y-1">
        <p className="flex items-center gap-1.5 text-sm font-medium">
          Sandbox
          <TermInfo id="environments.sandbox" />
          <span className={cn(
            "rounded-full border px-1.5 py-0.5 text-[11px] font-normal",
            on ? "border-emerald-500/20 bg-emerald-500/15 text-emerald-400" : "border-border/60 text-muted-foreground",
          )}>
            {on ? "Sandboxed" : "Host kernel"}
          </span>
        </p>
        <p className="text-xs text-muted-foreground leading-relaxed">
          {locked
            ? <>Required: {state?.why}</>
            : <>Runs {what}&apos;s apps and jobs on gVisor, a kernel of their own, so code that escapes its container lands there and not on the machine. Each sandboxed pod takes some 35 MiB more memory. Databases stay on the host&apos;s kernel, where they run at full speed, and builds are never sandboxed.</>}
        </p>
        {!on && noNode && (
          <p className="text-xs text-muted-foreground">
            No node can run sandboxed projects yet. Run <code className="font-mono text-foreground">sudo meshploy install sandbox</code> on one first.
          </p>
        )}
        {!isAdmin && !locked && <p className="text-xs text-muted-foreground">Only an owner or admin can change this.</p>}
        {set.isError && asking === null && <p className="text-xs text-destructive">{(set.error as Error).message}</p>}
      </div>
      <div className="shrink-0 pt-0.5">
        {set.isPending
          ? <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          : <Switch
              checked={on}
              disabled={!state || locked || !isAdmin || (!on && noNode)}
              onCheckedChange={(v) => { set.reset(); setAsking(v) }}
              aria-label={`Run ${what}'s apps sandboxed`}
            />}
      </div>

      <Dialog open={asking !== null} onOpenChange={(open) => { if (!open && !set.isPending) setAsking(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{asking ? `Sandbox ${what}?` : `Stop sandboxing ${what}?`}</DialogTitle>
            <DialogDescription>
              {asking
                ? "Its running apps roll onto gVisor now, without stopping, and each takes a little more memory from then on. Its databases stay as they are."
                : "Its running apps roll back onto the host's kernel now, without stopping. Its databases stay as they are."}
            </DialogDescription>
          </DialogHeader>
          {set.isError && <p className="text-xs text-destructive">{(set.error as Error).message}</p>}
          <DialogFooter>
            <Button variant="outline" onClick={() => setAsking(null)} disabled={set.isPending}>Cancel</Button>
            <Button onClick={() => set.mutate(!!asking)} disabled={set.isPending}>
              {set.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {asking ? "Sandbox" : "Stop sandboxing"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
