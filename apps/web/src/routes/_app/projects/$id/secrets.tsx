import { createFileRoute, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Eye, EyeOff, KeyRound, Loader2, Pencil, Plus, Trash2, Check, X } from "lucide-react"
import { secrets as secretsApi, type ApiSecret } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/secrets")({
  component: SecretsPage,
})

function SecretsPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/secrets" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const [showAdd, setShowAdd] = useState(false)
  const [newName, setNewName] = useState("")
  const [newValue, setNewValue] = useState("")
  const [showNewValue, setShowNewValue] = useState(false)

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["secrets", orgId, projectId],
    queryFn: () => secretsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ["secrets", orgId, projectId] })

  const addMut = useMutation({
    mutationFn: () => secretsApi.create(orgId, projectId, { name: newName.trim(), value: newValue }, token),
    onSuccess: () => { setNewName(""); setNewValue(""); setShowAdd(false); invalidate() },
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => secretsApi.delete(orgId, projectId, id, token),
    onSuccess: invalidate,
  })

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-medium">Secrets</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Encrypted key-value pairs. Attach them to services to inject as environment variables at deploy time.
          </p>
        </div>
        <Button size="sm" variant="outline" className="gap-1.5" onClick={() => setShowAdd(true)}>
          <Plus className="h-3.5 w-3.5" /> New secret
        </Button>
      </div>

      {/* Add form */}
      {showAdd && (
        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-3">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">New secret</p>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">Name</label>
              <input
                className={inputCls}
                placeholder="e.g. DATABASE_URL"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">Value</label>
              <div className="relative">
                <input
                  className={cn(inputCls, "pr-8")}
                  type={showNewValue ? "text" : "password"}
                  placeholder="Secret value"
                  value={newValue}
                  onChange={(e) => setNewValue(e.target.value)}
                />
                <button
                  type="button"
                  onClick={() => setShowNewValue((v) => !v)}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground"
                >
                  {showNewValue ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
              </div>
            </div>
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              onClick={() => addMut.mutate()}
              disabled={!newName.trim() || !newValue || addMut.isPending}
            >
              {addMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
              Save
            </Button>
            <Button size="sm" variant="ghost" onClick={() => { setShowAdd(false); setNewName(""); setNewValue("") }}>
              Cancel
            </Button>
          </div>
          {addMut.isError && (
            <p className="text-xs text-destructive">{(addMut.error as Error).message}</p>
          )}
        </div>
      )}

      {/* List */}
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-8">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading…
        </div>
      ) : list.length === 0 && !showAdd ? (
        <div className="flex flex-col items-center gap-3 py-16 text-center">
          <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-muted/50">
            <KeyRound className="h-4 w-4 text-muted-foreground/60" />
          </div>
          <p className="text-sm text-muted-foreground">No secrets yet</p>
          <Button size="sm" variant="outline" className="gap-1.5" onClick={() => setShowAdd(true)}>
            <Plus className="h-3.5 w-3.5" /> Add your first secret
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden">
          {list.map((s, i) => (
            <SecretRow
              key={s.id}
              secret={s}
              last={i === list.length - 1}
              orgId={orgId}
              projectId={projectId}
              token={token}
              onDelete={() => deleteMut.mutate(s.id)}
              isDeleting={deleteMut.isPending && deleteMut.variables === s.id}
              onUpdated={invalidate}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function SecretRow({
  secret, last, orgId, projectId, token, onDelete, isDeleting, onUpdated,
}: {
  secret: ApiSecret
  last: boolean
  orgId: string
  projectId: string
  token: string
  onDelete: () => void
  isDeleting: boolean
  onUpdated: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState("")
  const [showEdit, setShowEdit] = useState(false)

  const updateMut = useMutation({
    mutationFn: () => secretsApi.update(orgId, projectId, secret.id, editValue, token),
    onSuccess: () => { setEditing(false); onUpdated() },
  })

  return (
    <div className={cn(
      "flex items-center gap-3 px-4 py-3",
      !last && "border-b border-border/40"
    )}>
      <KeyRound className="h-3.5 w-3.5 text-muted-foreground/50 shrink-0" />
      <div className="flex-1 min-w-0">
        <code className="text-xs font-mono text-foreground">{secret.name}</code>
        {editing ? (
          <div className="flex items-center gap-2 mt-1.5">
            <div className="relative flex-1">
              <input
                className={cn(inputCls, "pr-8 text-xs h-7")}
                type={showEdit ? "text" : "password"}
                placeholder="New value"
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                autoFocus
              />
              <button
                type="button"
                onClick={() => setShowEdit((v) => !v)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground"
              >
                {showEdit ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
              </button>
            </div>
            <button
              onClick={() => updateMut.mutate()}
              disabled={!editValue || updateMut.isPending}
              className="text-muted-foreground hover:text-foreground disabled:opacity-40"
            >
              {updateMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
            </button>
            <button onClick={() => setEditing(false)} className="text-muted-foreground hover:text-foreground">
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : (
          <p className="text-[11px] text-muted-foreground/50 mt-0.5">••••••••</p>
        )}
      </div>
      {!editing && (
        <div className="flex items-center gap-1 shrink-0">
          <button
            onClick={() => { setEditing(true); setEditValue("") }}
            className="p-1.5 text-muted-foreground/40 hover:text-muted-foreground transition-colors"
            title="Edit value"
          >
            <Pencil className="h-3 w-3" />
          </button>
          <button
            onClick={onDelete}
            disabled={isDeleting}
            className="p-1.5 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-40"
            title="Delete"
          >
            {isDeleting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
          </button>
        </div>
      )}
    </div>
  )
}
