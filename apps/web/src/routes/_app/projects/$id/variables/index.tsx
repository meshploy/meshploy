import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { KeyRound, Layers, Loader2, Lock, Plus, Server, Trash2 } from "lucide-react"
import { variableGroups as groupsApi, type ApiVariableGroup } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"

export const Route = createFileRoute("/_app/projects/$id/variables/")({
  component: VariablesPage,
})

function GroupCard({ group, projectId, onDelete }: { group: ApiVariableGroup; projectId: string; onDelete: () => void }) {
  const navigate = useNavigate()
  const varCount = group.items.filter((i) => !i.is_secret).length
  const secretCount = group.items.filter((i) => i.is_secret).length

  return (
    <div
      onClick={() => navigate({ to: "/projects/$id/variables/$groupId", params: { id: projectId, groupId: group.id } })}
      className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4 hover:border-border transition-all cursor-pointer"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted border border-border/60 shrink-0">
            {group.system_managed
              ? <Server className="h-3.5 w-3.5 text-muted-foreground" />
              : <Layers className="h-3.5 w-3.5 text-muted-foreground" />
            }
          </div>
          <div>
            <div className="flex items-center gap-1.5">
              <p className="text-sm font-semibold text-foreground leading-tight">{group.name}</p>
              {group.system_managed && (
                <span className="flex items-center gap-1 text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border/60 shrink-0">
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
        </div>
        {!group.system_managed && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={(e) => { e.stopPropagation(); onDelete() }}
            className="text-muted-foreground hover:text-destructive transition-colors shrink-0"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </div>
  )
}

function VariablesPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/variables/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const navigate = useNavigate()

  const { data: groups = [], isLoading } = useQuery({
    queryKey: ["variable-groups", orgId, projectId],
    queryFn: () => groupsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const deleteMut = useMutation({
    mutationFn: (groupId: string) => groupsApi.delete(orgId, projectId, groupId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["variable-groups", orgId, projectId] }),
  })

  const goNew = () => navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "variable-group" } })

  const userGroups = groups.filter((g) => !g.system_managed)
  const systemGroups = groups.filter((g) => g.system_managed)

  return (
    <div className="p-6 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Variables</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && <span className="text-xs text-muted-foreground">{userGroups.length}</span>}
        </div>
        <Button size="sm" className="gap-1.5" onClick={goNew}>
          <Plus className="h-3.5 w-3.5" /> New group
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : userGroups.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <KeyRound className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No variable groups yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">Group related variables and secrets, then attach them to services</p>
          </div>
          <Button size="sm" className="gap-1.5 mt-1" onClick={goNew}>
            <Plus className="h-3.5 w-3.5" /> New group
          </Button>
        </div>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {userGroups.map((g) => (
            <GroupCard key={g.id} group={g} projectId={projectId} onDelete={() => deleteMut.mutate(g.id)} />
          ))}
        </div>
      )}

      {/* System-managed groups */}
      {systemGroups.length > 0 && (
        <div className="space-y-3">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Service generated groups</p>
          <div className="grid gap-3 md:grid-cols-2">
            {systemGroups.map((g) => (
              <GroupCard key={g.id} group={g} projectId={projectId} onDelete={() => {}} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
