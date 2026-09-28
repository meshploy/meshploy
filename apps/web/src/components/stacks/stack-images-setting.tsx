import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, RotateCcw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { stacks as stacksApi } from "@/lib/api"

// How many images a stack's built services keep for rollback. A stack keeps
// every image until this is turned on; a service that says otherwise in the
// compose file (x-meshploy rollback) keeps its own setting.
export function StackImagesSetting({ orgId, projectId, stackId, token }: {
  orgId: string; projectId: string; stackId: string; token: string
}) {
  const qc = useQueryClient()
  const { data: stack } = useQuery({
    queryKey: ["stack", orgId, projectId, stackId],
    queryFn: () => stacksApi.get(orgId, projectId, stackId, token),
  })
  // What has been changed here and not saved; null shows the stack's own.
  const [draft, setDraft] = useState<{ enabled: boolean; retention: string } | null>(null)
  const enabled = draft?.enabled ?? !!stack?.rollback_enabled
  const retention = draft?.retention ?? String(stack?.image_retention || 3)
  const setEnabled = (v: boolean) => setDraft({ enabled: v, retention })
  const setRetention = (v: string) => setDraft({ enabled, retention: v })

  const save = useMutation({
    mutationFn: () => stacksApi.update(orgId, projectId, stackId, {
      rollback_enabled: enabled, image_retention: Math.max(1, parseInt(retention) || 3),
    }, token),
    onSuccess: () => {
      setDraft(null)
      qc.invalidateQueries({ queryKey: ["stack", orgId, projectId, stackId] })
      qc.invalidateQueries({ queryKey: ["build-config", orgId, projectId] })
    },
  })

  if (!stack) return null
  const changed = enabled !== !!stack.rollback_enabled
    || (enabled && (parseInt(retention) || 3) !== (stack.image_retention || 3))

  return (
    <div className="rounded-lg border border-border/60 px-4 py-3 space-y-2" data-testid="stack-images-setting">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        <div className="flex items-center gap-2">
          <RotateCcw className="h-3.5 w-3.5 text-muted-foreground/60" />
          <h2 className="text-sm font-medium">Images kept for rollback</h2>
        </div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Switch checked={enabled} onCheckedChange={(v) => setEnabled(Boolean(v))} aria-label="Limit kept images" />
          {enabled ? "Each built service keeps its last" : "Every image each built service builds"}
        </label>
        {enabled && (
          <input type="number" min={1} max={50} value={retention} onChange={(e) => setRetention(e.target.value)}
            aria-label="Images to keep"
            className="h-8 w-16 rounded-md border border-border/60 bg-muted/20 px-2 text-sm" />
        )}
        {changed && (
          <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending} className="ml-auto">
            {save.isPending && <Loader2 className="size-4 animate-spin" />}Save
          </Button>
        )}
      </div>
      <p className="text-[11px] text-muted-foreground/70">
        {enabled
          ? "Older images are removed when this is saved and after each build. A service that sets its own rollback in the compose file keeps its own."
          : "Nothing is removed, so the registry grows with every build. Any deployment whose image is kept can be rolled back to from its service's Deployments."}
      </p>
      {save.isError && <p className="text-xs text-destructive">{(save.error as Error).message}</p>}
    </div>
  )
}
