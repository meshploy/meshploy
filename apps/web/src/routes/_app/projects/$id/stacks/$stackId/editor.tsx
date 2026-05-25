import { createFileRoute, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState, useEffect } from "react"
import { AlertTriangle, GitBranch, Loader2, PlayCircle, RefreshCw, Save } from "lucide-react"
import { Button } from "@/components/ui/button"
import { stacks as stacksApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { StackEditor } from "@/components/stacks/stack-editor"
import { formatRelativeTime } from "@/lib/utils"

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
  const [syncWarning, setSyncWarning] = useState<{ message: string; suggestedMode: string } | null>(null)

  const isGitSourced = !!stack?.git_mode

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

  const syncMutation = useMutation({
    mutationFn: () => stacksApi.sync(orgId!, projectId, stackId, token),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: stackQueryKey })
      queryClient.invalidateQueries({ queryKey: servicesQueryKey })
      setDirty(false)
      if (result.warning && result.suggested_mode && result.suggested_mode !== stack?.git_mode) {
        setSyncWarning({ message: result.warning, suggestedMode: result.suggested_mode })
      } else {
        setSyncWarning(null)
      }
    },
  })

  const switchModeMutation = useMutation({
    mutationFn: (mode: string) =>
      stacksApi.update(orgId!, projectId, stackId, { git_mode: mode as "file" | "repo" }, token),
    onSuccess: (updated) => {
      queryClient.setQueryData(stackQueryKey, updated)
      setSyncWarning(null)
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const repoShort = stack?.git_repo?.replace(/^https?:\/\//, "").replace(/\.git$/, "")

  return (
    <div className="flex flex-col h-full">
      {/* Git source banner */}
      {isGitSourced && stack && (
        <div className="flex items-center justify-between px-6 py-2.5 border-b border-border/60 bg-muted/30 shrink-0">
          <div className="flex items-center gap-3 text-xs text-muted-foreground min-w-0">
            <GitBranch className="h-3.5 w-3.5 shrink-0 text-primary/70" />
            <span className="truncate font-mono">{repoShort}</span>
            <span className="text-border">·</span>
            <span>{stack.git_branch}</span>
            <span className="text-border">·</span>
            <span>
              {stack.git_last_synced_at
                ? `synced ${formatRelativeTime(new Date(stack.git_last_synced_at))}`
                : "never synced"}
            </span>
            {stack.git_last_sync_sha && (
              <>
                <span className="text-border">·</span>
                <span className="font-mono">{stack.git_last_sync_sha.slice(0, 7)}</span>
              </>
            )}
          </div>
          <Button
            size="sm"
            variant="outline"
            className="gap-1.5 h-7 text-xs shrink-0"
            onClick={() => syncMutation.mutate()}
            disabled={syncMutation.isPending}
          >
            {syncMutation.isPending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <RefreshCw className="h-3 w-3" />
            )}
            Sync
          </Button>
        </div>
      )}

      {/* Mode mismatch warning */}
      {syncWarning && (
        <div className="flex items-start gap-3 px-6 py-2.5 border-b border-amber-500/20 bg-amber-500/5 shrink-0">
          <AlertTriangle className="h-3.5 w-3.5 text-amber-400 mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="text-xs text-amber-300/90">{syncWarning.message}</p>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="h-6 text-xs shrink-0 border-amber-500/30 text-amber-300 hover:bg-amber-500/10"
            onClick={() => switchModeMutation.mutate(syncWarning.suggestedMode)}
            disabled={switchModeMutation.isPending}
          >
            {switchModeMutation.isPending && <Loader2 className="h-3 w-3 animate-spin mr-1" />}
            Switch to {syncWarning.suggestedMode === "repo" ? "whole repo" : "file only"}
          </Button>
        </div>
      )}

      {/* Toolbar */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-border/60 shrink-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium">Compose Spec</p>
          {isGitSourced && (
            <span className="text-[11px] text-muted-foreground/60 bg-muted/40 px-1.5 py-0.5 rounded">
              read-only
            </span>
          )}
          {!isGitSourced && dirty && (
            <span className="text-[11px] text-amber-400/80 font-mono">unsaved changes</span>
          )}
        </div>
        {!isGitSourced && (
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
        )}
      </div>

      {/* Error banner */}
      {(saveMutation.isError || applyMutation.isError || syncMutation.isError) && (
        <div className="px-6 py-2 bg-destructive/10 border-b border-destructive/20 shrink-0">
          <p className="text-xs text-destructive">
            {((saveMutation.error || applyMutation.error || syncMutation.error) as Error)?.message ?? "Operation failed"}
          </p>
        </div>
      )}

      {/* Editor */}
      <div className="flex-1 overflow-auto p-4">
        <StackEditor
          value={spec}
          onChange={isGitSourced ? undefined : (value) => {
            setSpec(value)
            setDirty(value !== (stack?.spec ?? ""))
          }}
          minHeight="calc(100vh - 200px)"
          readOnly={isGitSourced}
        />
      </div>

      {/* Apply/sync result */}
      {(applyMutation.isSuccess || syncMutation.isSuccess) && (
        <div className="px-6 py-3 border-t border-border/60 bg-muted/20 shrink-0">
          {(() => {
            const data = applyMutation.data ?? syncMutation.data
            if (!data) return null
            return (
              <p className="text-xs text-muted-foreground">
                {syncMutation.isSuccess ? "Sync" : "Apply"} complete —
                {data.created.length > 0 && ` created: ${data.created.join(", ")}`}
                {data.updated.length > 0 && ` updated: ${data.updated.join(", ")}`}
                {data.deleted.length > 0 && ` unlinked: ${data.deleted.join(", ")}`}
                {data.errors.length > 0 && (
                  <span className="text-destructive"> errors: {data.errors.join("; ")}</span>
                )}
                {data.created.length === 0 &&
                  data.updated.length === 0 &&
                  data.errors.length === 0 && " no changes"}
              </p>
            )
          })()}
        </div>
      )}
    </div>
  )
}
