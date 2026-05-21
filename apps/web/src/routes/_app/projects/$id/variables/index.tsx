import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { KeyRound, Layers, Lock, Loader2, Plus, Server, Trash2 } from "lucide-react"
import { useState } from "react"
import { variableGroups as groupsApi, type ApiVariableGroup } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/variables/")({
  component: VariablesPage,
})

function GroupCard({ group, onClick, onDelete }: { group: ApiVariableGroup; onClick: () => void; onDelete: () => void }) {
  const varCount = group.items.filter((i) => !i.is_secret).length
  const secretCount = group.items.filter((i) => i.is_secret).length

  return (
    <div
      onClick={onClick}
      className="flex items-center gap-3 rounded-lg border border-border/60 bg-card px-4 py-3 hover:border-border transition-all cursor-pointer"
    >
      <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted border border-border/60 shrink-0">
        {group.system_managed ? <Server className="h-3.5 w-3.5 text-muted-foreground" /> : <Layers className="h-3.5 w-3.5 text-muted-foreground" />}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-semibold text-foreground truncate">{group.name}</p>
          {group.system_managed && (
            <span className="flex items-center gap-1 text-[10px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border/60 shrink-0">
              <Lock className="h-2.5 w-2.5" /> auto
            </span>
          )}
        </div>
        <p className="text-[11px] text-muted-foreground mt-0.5">
          {varCount > 0 && `${varCount} var${varCount !== 1 ? "s" : ""}`}
          {varCount > 0 && secretCount > 0 && " · "}
          {secretCount > 0 && `${secretCount} secret${secretCount !== 1 ? "s" : ""}`}
          {group.items.length === 0 && "empty"}
        </p>
      </div>
      {!group.system_managed && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={(e) => { e.stopPropagation(); onDelete() }}
          className="text-muted-foreground/30 hover:text-destructive transition-colors shrink-0"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      )}
    </div>
  )
}

function VariablesPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/variables/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState("")
  const [newDesc, setNewDesc] = useState("")

  const { data: groups = [], isLoading } = useQuery({
    queryKey: ["variable-groups", orgId, projectId],
    queryFn: () => groupsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const createMut = useMutation({
    mutationFn: () => groupsApi.create(orgId, projectId, { name: newName.trim(), description: newDesc.trim() }, token),
    onSuccess: (g) => {
      qc.invalidateQueries({ queryKey: ["variable-groups", orgId, projectId] })
      setShowCreate(false)
      setNewName("")
      setNewDesc("")
      navigate({ to: "/projects/$id/variables/$groupId", params: { id: projectId, groupId: g.id } })
    },
  })

  const deleteMut = useMutation({
    mutationFn: (groupId: string) => groupsApi.delete(orgId, projectId, groupId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["variable-groups", orgId, projectId] }),
  })

  const userGroups = groups.filter((g) => !g.system_managed)
  const systemGroups = groups.filter((g) => g.system_managed)

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Variables</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && <span className="text-xs text-muted-foreground">{userGroups.length}</span>}
        </div>
        <Button size="sm" className="gap-1.5" onClick={() => setShowCreate(true)}>
          <Plus className="h-3.5 w-3.5" /> New group
        </Button>
      </div>

      {/* Create form */}
      {showCreate && (
        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-3">
          <p className="text-xs font-medium">New variable group</p>
          <input
            autoFocus
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="Group name (e.g. Database, Stripe)"
            className={inputCls}
          />
          <input
            value={newDesc}
            onChange={(e) => setNewDesc(e.target.value)}
            placeholder="Description (optional)"
            className={cn(inputCls, "text-xs")}
          />
          <div className="flex gap-2">
            <Button size="sm" onClick={() => createMut.mutate()} disabled={!newName.trim() || createMut.isPending}>
              {createMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
              Create
            </Button>
            <Button size="sm" variant="ghost" onClick={() => { setShowCreate(false); setNewName(""); setNewDesc("") }}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {/* User groups */}
      {!isLoading && userGroups.length === 0 && !showCreate && (
        <div className="rounded-lg border border-dashed border-border/60 py-12 flex flex-col items-center gap-3">
          <KeyRound className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No variable groups yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Group related variables and secrets, then attach them to services</p>
          </div>
          <Button size="sm" className="gap-1.5 mt-1" onClick={() => setShowCreate(true)}>
            <Plus className="h-3.5 w-3.5" /> New group
          </Button>
        </div>
      )}

      {userGroups.length > 0 && (
        <div className="space-y-2">
          {userGroups.map((g) => (
            <GroupCard
              key={g.id}
              group={g}
              onClick={() => navigate({ to: "/projects/$id/variables/$groupId", params: { id: projectId, groupId: g.id } })}
              onDelete={() => deleteMut.mutate(g.id)}
            />
          ))}
        </div>
      )}

      {/* System-managed groups */}
      {systemGroups.length > 0 && (
        <div className="space-y-2">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Service discovery</p>
          {systemGroups.map((g) => (
            <GroupCard
              key={g.id}
              group={g}
              onClick={() => navigate({ to: "/projects/$id/variables/$groupId", params: { id: projectId, groupId: g.id } })}
              onDelete={() => {}}
            />
          ))}
        </div>
      )}
    </div>
  )
}
