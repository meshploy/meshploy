import { createFileRoute, useNavigate, useParams, Link } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Box, FileCog, Loader2, Plus, Trash2, Unplug } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import {
  configFiles as configFilesApi,
  services as servicesApi,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { DetailPageHeader } from "@/components/layout/detail-page-header"
import { ConfigFileEditor } from "@/components/config-files/config-file-editor"
import { StackPill, useStackNames } from "@/components/stacks/stack-pill"
import { cn, formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/config-files/$fileId")({
  component: ConfigFileDetailPage,
})

// ─── Attachments ──────────────────────────────────────────────────────────────

/**
 * The services mounting this file.
 *
 * Detaching re-applies the service immediately. A `subPath` mount is a copy
 * taken at container start, so without that re-apply the file would linger in
 * the running pod and vanish at some later restart — failing for a reason
 * nobody would connect back to this action.
 */
function AttachmentsSection({
  fileId, projectId, orgId, token, attached,
}: {
  fileId: string
  projectId: string
  orgId: string
  token: string
  attached: { id: string; name: string }[]
}) {
  const qc = useQueryClient()
  const [selectedId, setSelectedId] = useState("")

  const { data: allServices = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const attachedIds = new Set(attached.map((a) => a.id))
  // Only applications mount config files: a managed database's container is
  // ours, and its configuration comes from the engine settings, not a file.
  const available = allServices.filter((s) => s.type === "application" && !attachedIds.has(s.id))

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["config-file", orgId, projectId, fileId] })
    qc.invalidateQueries({ queryKey: ["config-files", orgId, projectId] })
  }
  const attachMut = useMutation({
    mutationFn: () => configFilesApi.attach(orgId, projectId, fileId, selectedId, token),
    onSuccess: () => { setSelectedId(""); invalidate() },
  })
  const detachMut = useMutation({
    mutationFn: (serviceId: string) => configFilesApi.detach(orgId, projectId, fileId, serviceId, token),
    onSuccess: invalidate,
  })

  const selectedName = available.find((s) => s.id === selectedId)?.name

  return (
    <Section
      title="Attached services"
      subtitle="Each service mounts this file at its path. Attaching or detaching re-applies the service."
    >
      {attached.length > 0 ? (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {attached.map((s) => (
            <div key={s.id} className="flex items-center gap-3 px-4 py-2.5">
              <Box className="h-3.5 w-3.5 text-muted-foreground/60 shrink-0" />
              <Link
                to="/projects/$id/services/$serviceId/overview"
                params={{ id: projectId, serviceId: s.id }}
                className="text-sm text-foreground hover:underline flex-1 min-w-0 truncate"
              >
                {s.name}
              </Link>
              <Button
                size="sm"
                variant="ghost"
                className="h-6 gap-1.5 px-2 text-[11px] text-muted-foreground hover:text-destructive"
                disabled={detachMut.isPending}
                onClick={() => detachMut.mutate(s.id)}
              >
                <Unplug className="h-3 w-3" />
                Detach
              </Button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">
          Not mounted by any service. Until it is attached, this file exists only in meshploy.
        </p>
      )}

      {detachMut.isError && (
        <p className="text-xs text-destructive">{(detachMut.error as Error).message}</p>
      )}

      <div className="flex items-center gap-2 pt-1">
        <Select value={selectedId} onValueChange={(v) => setSelectedId(v ?? "")}>
          <SelectTrigger className="h-8 text-xs flex-1">
            <SelectValue placeholder={available.length ? "Select a service…" : "No services available"}>
              {selectedName}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {available.map((s) => (
              <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="sm"
          className="gap-1.5"
          disabled={!selectedId || attachMut.isPending}
          onClick={() => attachMut.mutate()}
        >
          {attachMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
          Attach
        </Button>
      </div>
      {attachMut.isError && (
        <p className="text-xs text-destructive">{(attachMut.error as Error).message}</p>
      )}
    </Section>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function ConfigFileDetailPage() {
  const { id: projectId, fileId } = useParams({ from: "/_app/projects/$id/config-files/$fileId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()
  const stackNames = useStackNames(orgId, projectId)

  const { data: file, isLoading } = useQuery({
    queryKey: ["config-file", orgId, projectId, fileId],
    queryFn: () => configFilesApi.get(orgId, projectId, fileId, token),
    enabled: !!orgId,
  })

  const [name, setName] = useState<string | null>(null)
  const [path, setPath] = useState<string | null>(null)
  const [content, setContent] = useState("")
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  // The form mirrors the file until the user edits a field, so a background
  // refetch does not overwrite what they are typing.
  const nameValue = name ?? file?.name ?? ""
  const pathValue = path ?? file?.path ?? ""
  const dirty = nameValue !== (file?.name ?? "") || pathValue !== (file?.path ?? "") || content !== ""

  const saveMut = useMutation({
    mutationFn: () =>
      configFilesApi.update(orgId, projectId, fileId, { name: nameValue, path: pathValue, content }, token),
    onSuccess: () => {
      setName(null); setPath(null); setContent("")
      qc.invalidateQueries({ queryKey: ["config-file", orgId, projectId, fileId] })
      qc.invalidateQueries({ queryKey: ["config-files", orgId, projectId] })
    },
  })

  const deleteMut = useMutation({
    mutationFn: () => configFilesApi.delete(orgId, projectId, fileId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config-files", orgId, projectId] })
      navigate({ to: "/projects/$id/config-files", params: { id: projectId } })
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
  if (!file) {
    return <div className="p-6 text-sm text-muted-foreground">Config file not found.</div>
  }

  const attachedCount = file.attached_services.length

  return (
    <div className="flex flex-col min-h-full">
      <DetailPageHeader
        backTo="/projects/$id/config-files"
        backLabel="Back to config files"
        backParams={{ id: projectId }}
        icon={<FileCog className="h-4 w-4 text-muted-foreground" />}
        name={file.name}
        badge={<StackPill stackId={file.stack_id} stackNames={stackNames} orgId={orgId} projectId={projectId} />}
        subtitle={file.path}
      />

      <div className="p-6 max-w-2xl space-y-6">
        {/* Stats */}
        <div className="grid grid-cols-3 gap-4 rounded-lg border border-border/60 bg-card px-4 py-3.5">
          <div>
            <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Size</p>
            <p className="text-sm text-foreground font-mono">{file.size} B</p>
          </div>
          <div>
            <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Mounted by</p>
            <p className="text-sm text-foreground">
              {attachedCount === 0 ? "No services" : `${attachedCount} service${attachedCount === 1 ? "" : "s"}`}
            </p>
          </div>
          <div>
            <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-0.5">Updated</p>
            <p className="text-sm text-foreground">{formatRelativeTime(new Date(file.updated_at))}</p>
          </div>
        </div>

        <AttachmentsSection
          fileId={fileId}
          projectId={projectId}
          orgId={orgId}
          token={token}
          attached={file.attached_services}
        />

        <Section
          title="File"
          subtitle="Contents are stored encrypted and never shown again, so saving replaces them rather than editing in place. Leave the editor empty to change only the name or path."
        >
          <Field label="Name">
            <input
              value={nameValue}
              onChange={(e) => setName(e.target.value)}
              className={inputCls}
            />
          </Field>
          <Field label="Mount path">
            <input
              value={pathValue}
              onChange={(e) => setPath(e.target.value)}
              className={cn(inputCls, "font-mono text-xs")}
              spellCheck={false}
            />
          </Field>
          <Field label="Replace contents">
            <ConfigFileEditor value={content} onChange={setContent} path={pathValue} height="280px" placeholder="New file contents" />
          </Field>

          {attachedCount > 0 && content !== "" && (
            <p className="text-xs text-amber-400">
              Saving redeploys {attachedCount} service{attachedCount === 1 ? "" : "s"}:{" "}
              {file.attached_services.map((s) => s.name).join(", ")}.
            </p>
          )}
          {saveMut.isError && (
            <p className="text-xs text-destructive">{(saveMut.error as Error).message}</p>
          )}

          <div className="flex items-center gap-2">
            <Button size="sm" className="gap-1.5" disabled={!dirty || saveMut.isPending} onClick={() => saveMut.mutate()}>
              {saveMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Save changes
            </Button>
            {dirty && (
              <Button size="sm" variant="ghost" onClick={() => { setName(null); setPath(null); setContent("") }}>
                Discard
              </Button>
            )}
          </div>
        </Section>

        <Section title="Danger zone" subtitle="Deleting a config file cannot be undone.">
          <div className="flex items-center justify-between gap-4 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3">
            <div className="min-w-0">
              <p className="text-sm text-foreground">Delete this config file</p>
              <p className="text-xs text-muted-foreground mt-0.5">
                {attachedCount > 0
                  ? `Detach it from ${attachedCount === 1 ? "its service" : "all services"} first — deleting a file something still mounts is refused.`
                  : "Not mounted by any service, so it can be removed safely."}
              </p>
            </div>
            <Button
              size="sm"
              variant="destructive"
              className="gap-1.5 shrink-0"
              disabled={attachedCount > 0}
              onClick={() => setConfirmingDelete(true)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete
            </Button>
          </div>
        </Section>
      </div>

      <Dialog open={confirmingDelete} onOpenChange={setConfirmingDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {file.name}?</DialogTitle>
            <DialogDescription>
              This cannot be undone. The contents are not recoverable — they are stored encrypted and were
              never shown again after saving.
            </DialogDescription>
          </DialogHeader>
          {deleteMut.isError && <p className="text-xs text-destructive">{(deleteMut.error as Error).message}</p>}
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setConfirmingDelete(false)}>Cancel</Button>
            <Button
              variant="destructive"
              size="sm"
              className="gap-1.5"
              disabled={deleteMut.isPending}
              onClick={() => deleteMut.mutate()}
            >
              {deleteMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
