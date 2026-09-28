import { createFileRoute, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState, useEffect } from "react"
import { GitBranch, Loader2, Pencil, Save } from "lucide-react"
import { Button } from "@/components/ui/button"
import { gitIntegrations, stacks as stacksApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { StackEditor } from "@/components/stacks/stack-editor"
import { StackSourceDialog } from "@/components/stacks/stack-source-dialog"
import { Link } from "@tanstack/react-router"
import { ResourceFact, ResourceIntro, ResourcePanel } from "@/components/layout/resource-workbench"
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

  const { data: stack, isLoading } = useQuery({
    queryKey: stackQueryKey,
    queryFn: () => stacksApi.get(orgId!, projectId, stackId, token),
    enabled: !!orgId,
  })

  const [spec, setSpec] = useState("")
  const [dirty, setDirty] = useState(false)

  const isGitSourced = !!stack?.git_mode
  const [editingSource, setEditingSource] = useState(false)
  const { data: integrations = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitIntegrations.list(orgId!, token),
    enabled: !!orgId && isGitSourced,
  })
  const access = integrations.find((i) => i.id === stack?.git_integration_id)

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

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const repoShort = stack?.git_repo?.replace(/^https?:\/\//, "").replace(/\.git$/, "")
  const failure = saveMutation.error as Error | null

  return (
    <div className="console-page space-y-6">
      <ResourceIntro
        title="Compose file"
        description={isGitSourced
          ? "Read from git. Sync, at the top, fetches the file from its branch and applies it: services with an image pull it, services with a build: section build from their context in the same repository."
          : "The stack's Compose file. Save keeps your edits; Apply, at the top, makes the cluster match the saved file."}
        action={!isGitSourced && (
          <Button size="sm" className="gap-1.5" onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending || !dirty}>
            {saveMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Save
          </Button>
        )}
      />

      {isGitSourced && stack && (
        <ResourcePanel title="Source" description="Where the file and everything it builds from come from."
          action={<Button size="sm" variant="outline" className="gap-1.5" onClick={() => setEditingSource(true)}>
            <Pencil className="h-3.5 w-3.5" />Edit
          </Button>}>
          <ResourceFact label="Git access">
            {access
              ? <span>{access.name} <span className="text-muted-foreground">({access.provider}{access.connected ? "" : ", not authorized yet"})</span></span>
              : <span className="text-amber-400/90">
                  None: Sync and builds cannot read this repository.{" "}
                  {integrations.length === 0
                    ? <Link to="/integrations/new" search={{ category: "git" }} className="text-primary hover:underline">Connect one</Link>
                    : <button type="button" onClick={() => setEditingSource(true)} className="text-primary hover:underline">Choose one</button>}
                </span>}
          </ResourceFact>
          <ResourceFact label="Repository">
            <span className="inline-flex items-center gap-1.5 font-mono">
              <GitBranch className="h-3.5 w-3.5 text-primary/70 shrink-0" />{repoShort}
            </span>
          </ResourceFact>
          <ResourceFact label="Branch"><span className="font-mono">{stack.git_branch || "default"}</span></ResourceFact>
          <ResourceFact label="Compose file"><span className="font-mono">{stack.git_path || "docker-compose.yml"}</span></ResourceFact>
          <ResourceFact label="Reads">
            {stack.git_mode === "repo"
              ? "The whole repository: files the compose file mounts come from it too"
              : "The compose file alone"}
          </ResourceFact>
          <ResourceFact label="Last sync">
            {stack.git_last_synced_at
              ? <>{formatRelativeTime(new Date(stack.git_last_synced_at))}
                  {stack.git_last_sync_sha && <span className="font-mono text-muted-foreground"> · {stack.git_last_sync_sha.slice(0, 7)}</span>}</>
              : <span className="text-muted-foreground">Not synced yet: the file below is the one stored with the stack</span>}
          </ResourceFact>
        </ResourcePanel>
      )}

      {failure && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
          {failure.message ?? "Operation failed"}
        </div>
      )}

      {isGitSourced && stack && orgId && (
        <StackSourceDialog open={editingSource} onOpenChange={setEditingSource} stack={stack}
          orgId={orgId} projectId={projectId} token={token} />
      )}

      <ResourcePanel
        title="Spec"
        description={isGitSourced ? "Read-only here: change it in the repository, then Sync." : undefined}
        action={!isGitSourced && dirty ? <span className="text-[11px] text-amber-400/80 font-mono">unsaved changes</span> : undefined}
      >
        <StackEditor
          value={spec}
          projectId={projectId}
          onChange={isGitSourced ? undefined : (value) => {
            setSpec(value)
            setDirty(value !== (stack?.spec ?? ""))
          }}
          minHeight="calc(100vh - 360px)"
          readOnly={isGitSourced}
        />
      </ResourcePanel>
    </div>
  )
}
