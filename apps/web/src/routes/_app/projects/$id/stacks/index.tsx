import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Layers, Plus, Trash2, AlertCircle } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { stacks as stacksApi, type ApiStack } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"


export const Route = createFileRoute("/_app/projects/$id/stacks/")({
  component: StacksTab,
})

const STATUS_STYLES: Record<ApiStack["status"], string> = {
  idle:     "bg-muted text-muted-foreground border-border",
  applying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:   "bg-destructive/10 text-destructive border-destructive/20",
}

function StackCard({ stack, projectId }: { stack: ApiStack; projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const queryKey = ["stacks", orgId, projectId]

  const deleteMutation = useMutation({
    mutationFn: () => stacksApi.delete(orgId, projectId, stack.id, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })

  return (
    <div
      className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4 hover:border-border transition-all cursor-pointer"
      onClick={() => navigate({ to: "/projects/$id/stacks/$stackId", params: { id: projectId, stackId: stack.id } })}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted border border-border/60 shrink-0">
            <Layers className="h-3.5 w-3.5 text-muted-foreground" />
          </div>
          <div>
            <p className="text-sm font-semibold text-foreground leading-tight">{stack.name}</p>
            <p className="text-[11px] text-muted-foreground">
              {stack.last_applied_at
                ? `Applied ${formatRelativeTime(new Date(stack.last_applied_at))}`
                : "Never applied"}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border shrink-0 ${STATUS_STYLES[stack.status]}`}>
            {stack.status}
          </Badge>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={(e) => { e.stopPropagation(); deleteMutation.mutate() }}
            disabled={deleteMutation.isPending}
            title="Delete stack"
            className="text-muted-foreground hover:text-destructive transition-colors disabled:opacity-50"
          >
            {deleteMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
          </Button>
        </div>
      </div>

      {stack.status === "failed" && (
        <div className="flex items-center gap-1.5 text-xs text-destructive">
          <AlertCircle className="h-3 w-3 shrink-0" />
          <span>Last apply failed — open to review and re-apply</span>
        </div>
      )}
    </div>
  )
}

function StacksTab() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/stacks/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()
  const queryKey = ["stacks", orgId, projectId]

  const { data: stackList = [], isLoading } = useQuery({
    queryKey,
    queryFn: () => stacksApi.list(orgId!, projectId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const data = query.state.data as ApiStack[] | undefined
      return data?.some((s) => s.status === "applying") ? 3000 : false
    },
  })

  const goToNew = () =>
    navigate({ to: "/projects/$id/new", params: { id: projectId }, search: { type: "stack" } })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Stacks</h2>
          {isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {!isLoading && (
            <span className="text-xs text-muted-foreground">{stackList.length}</span>
          )}
        </div>
        <Button
          size="sm"
          className="gap-1.5"
          onClick={goToNew}
        >
          <Plus className="h-3.5 w-3.5" />
          New Stack
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-40">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : stackList.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/60 py-14 flex flex-col items-center gap-3">
          <Layers className="h-7 w-7 text-muted-foreground/40" />
          <div className="text-center">
            <p className="text-sm text-muted-foreground">No stacks yet</p>
            <p className="text-xs text-muted-foreground/60 mt-0.5">
              Deploy multiple services together with a single YAML spec
            </p>
          </div>
          <Button
            size="sm"
            className="gap-1.5 mt-1"
            onClick={goToNew}
          >
            <Plus className="h-3.5 w-3.5" />
            New Stack
          </Button>
        </div>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {stackList.map((stack) => (
            <StackCard key={stack.id} stack={stack} projectId={projectId} />
          ))}
        </div>
      )}

    </div>
  )
}
