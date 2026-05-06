import { createFileRoute, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState, useEffect } from "react"
import { Loader2, Save, PlayCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { stacks as stacksApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { StackEditor } from "@/components/stacks/stack-editor"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/editor")({
  component: StackEditorTab,
})

function StackEditorTab() {
  const { id: projectId, stackId } = useParams({ from: "/_app/projects/$id/stacks/$stackId/editor" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const queryClient = useQueryClient()

  const stackQueryKey = ["stack", orgId, projectId, stackId]
  const servicesQueryKey = ["stack-services", orgId, projectId, stackId]

  const { data: stack, isLoading } = useQuery({
    queryKey: stackQueryKey,
    queryFn: () => stacksApi.get(orgId!, projectId, stackId, token),
    enabled: !!orgId,
  })

  const [spec, setSpec] = useState("")
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (stack && !dirty) {
      setSpec(stack.spec)
    }
  }, [stack, dirty])

  const saveMutation = useMutation({
    mutationFn: () => stacksApi.update(orgId!, projectId, stackId, { spec }, token),
    onSuccess: (updated) => {
      queryClient.setQueryData(stackQueryKey, updated)
      setDirty(false)
    },
  })

  const applyMutation = useMutation({
    mutationFn: async () => {
      if (dirty) {
        await stacksApi.update(orgId!, projectId, stackId, { spec }, token)
      }
      return stacksApi.apply(orgId!, projectId, stackId, token)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: stackQueryKey })
      queryClient.invalidateQueries({ queryKey: servicesQueryKey })
      setDirty(false)
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-border/60 shrink-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium">Compose Spec</p>
          {dirty && (
            <span className="text-[11px] text-amber-400/80 font-mono">unsaved changes</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            className="gap-1.5"
            onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending || !dirty}
          >
            {saveMutation.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            Save
          </Button>
          <Button
            size="sm"
            className="gap-1.5"
            onClick={() => applyMutation.mutate()}
            disabled={applyMutation.isPending}
          >
            {applyMutation.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <PlayCircle className="h-3.5 w-3.5" />
            )}
            {dirty ? "Save & Apply" : "Apply"}
          </Button>
        </div>
      </div>

      {/* Error banner */}
      {(saveMutation.isError || applyMutation.isError) && (
        <div className="px-6 py-2 bg-destructive/10 border-b border-destructive/20 shrink-0">
          <p className="text-xs text-destructive">
            {((saveMutation.error || applyMutation.error) as Error)?.message ?? "Operation failed"}
          </p>
        </div>
      )}

      {/* Editor */}
      <div className="flex-1 overflow-auto p-4">
        <StackEditor
          value={spec}
          onChange={(value) => {
            setSpec(value)
            setDirty(value !== (stack?.spec ?? ""))
          }}
          minHeight="calc(100vh - 200px)"
        />
      </div>

      {/* Apply result */}
      {applyMutation.isSuccess && applyMutation.data && (
        <div className="px-6 py-3 border-t border-border/60 bg-muted/20 shrink-0">
          <p className="text-xs text-muted-foreground">
            Apply complete —
            {applyMutation.data.created.length > 0 && ` created: ${applyMutation.data.created.join(", ")}`}
            {applyMutation.data.updated.length > 0 && ` updated: ${applyMutation.data.updated.join(", ")}`}
            {applyMutation.data.deleted.length > 0 && ` unlinked: ${applyMutation.data.deleted.join(", ")}`}
            {applyMutation.data.errors.length > 0 && (
              <span className="text-destructive"> errors: {applyMutation.data.errors.join("; ")}</span>
            )}
            {applyMutation.data.created.length === 0 &&
              applyMutation.data.updated.length === 0 &&
              applyMutation.data.errors.length === 0 && " no changes"}
          </p>
        </div>
      )}
    </div>
  )
}
