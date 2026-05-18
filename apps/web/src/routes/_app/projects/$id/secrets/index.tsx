import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Eye, EyeOff, KeyRound, Loader2, Pencil, Plus, Trash2, X } from "lucide-react"
import { secrets as secretsApi, type ApiSecret } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/secrets/")({
  component: SecretsPage,
})

function SecretsPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/secrets/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["secrets", orgId, projectId],
    queryFn: () => secretsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ["secrets", orgId, projectId] })

  const deleteMut = useMutation({
    mutationFn: (id: string) => secretsApi.delete(orgId, projectId, id, token),
    onSuccess: invalidate,
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Secrets</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && <span className="text-xs text-muted-foreground">{list.length}</span>}
        </div>
        <Link to="/projects/$id/new" params={{ id: projectId }} search={{ type: "secret" }}>
          <Button size="sm" className="gap-1.5">
            <Plus className="h-3.5 w-3.5" /> New secret
          </Button>
        </Link>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <KeyRound className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No secrets yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Create encrypted key-value pairs and attach them to services</p>
          </div>
          <Link to="/projects/$id/new" params={{ id: projectId }} search={{ type: "secret" }}>
            <Button size="sm" className="gap-1.5 mt-1">
              <Plus className="h-3.5 w-3.5" /> New secret
            </Button>
          </Link>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden">
          <Table>
            <TableHeader className="bg-muted/20">
              <TableRow className="border-b border-border/40 hover:bg-transparent">
                <TableHead className="px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Name</TableHead>
                <TableHead className="px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider w-[220px]">Value</TableHead>
                <TableHead className="px-4 py-2.5 text-[10px] font-medium text-muted-foreground uppercase tracking-wider w-[160px]">Updated</TableHead>
                <TableHead className="px-4 py-2.5 w-[72px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((s) => (
                <SecretRow
                  key={s.id}
                  secret={s}
                  orgId={orgId}
                  projectId={projectId}
                  token={token}
                  onDelete={() => deleteMut.mutate(s.id)}
                  isDeleting={deleteMut.isPending && deleteMut.variables === s.id}
                  onUpdated={invalidate}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function SecretRow({
  secret, orgId, projectId, token, onDelete, isDeleting, onUpdated,
}: {
  secret: ApiSecret
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

  const relTime = new Date(secret.updated_at).toLocaleDateString(undefined, {
    month: "short", day: "numeric", year: "numeric",
  })

  return (
    <TableRow className="border-b border-border/30 hover:bg-muted/10">
      <TableCell className="px-4 py-3">
        <div className="flex items-center gap-2">
          <KeyRound className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" />
          <code className="text-xs font-mono text-foreground">{secret.name}</code>
        </div>
      </TableCell>
      <TableCell className="px-4 py-3">
        {editing ? (
          <div className="flex items-center gap-1.5">
            <div className="relative flex-1 min-w-0">
              <input
                className={cn(inputCls, "pr-7 text-xs h-7")}
                type={showEdit ? "text" : "password"}
                placeholder="New value"
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === "Enter") updateMut.mutate()
                  if (e.key === "Escape") setEditing(false)
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => setShowEdit((v) => !v)}
                className="absolute right-0.5 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground"
              >
                {showEdit ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
              </Button>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => updateMut.mutate()}
              disabled={!editValue || updateMut.isPending}
              className="text-muted-foreground hover:text-foreground shrink-0"
            >
              {updateMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setEditing(false)}
              className="text-muted-foreground hover:text-foreground shrink-0"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        ) : (
          <span className="text-xs font-mono text-muted-foreground/40 tracking-widest">••••••••</span>
        )}
      </TableCell>
      <TableCell className="px-4 py-3">
        <span className="text-xs text-muted-foreground/60">{relTime}</span>
      </TableCell>
      <TableCell className="px-4 py-3">
        <div className="flex items-center justify-end gap-0.5">
          {!editing && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => { setEditing(true); setEditValue("") }}
              title="Edit value"
              className="text-muted-foreground/40 hover:text-muted-foreground"
            >
              <Pencil className="h-3 w-3" />
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onDelete}
            disabled={isDeleting}
            title="Delete"
            className="text-muted-foreground/40 hover:text-destructive"
          >
            {isDeleting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}
