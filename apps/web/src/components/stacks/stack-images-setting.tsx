import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { RotateCcw } from "lucide-react"
import { Switch } from "@/components/ui/switch"
import { stacks as stacksApi } from "@/lib/api"
import { CountStepper } from "@/components/forms/steppers"
import { useConfigDraft, useConfigSave } from "@/components/layout/config-save-bar"

// How many images a stack's built services keep for rollback. A stack keeps
// every image until this is turned on; a service that says otherwise in the
// compose file (x-meshploy rollback) keeps its own setting. Saved with the
// page's floating bar, and laid out the same on or off.
export function StackImagesSetting({ orgId, projectId, stackId, token }: {
  orgId: string; projectId: string; stackId: string; token: string
}) {
  const qc = useQueryClient()
  const { data: stack } = useQuery({
    queryKey: ["stack", orgId, projectId, stackId],
    queryFn: () => stacksApi.get(orgId, projectId, stackId, token),
  })
  const draft = useConfigDraft({ on: false, count: 3 })
  useEffect(() => {
    if (!stack) return
    draft.sync({ on: !!stack.rollback_enabled, count: stack.image_retention || 3 })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stack])
  useConfigSave("Images kept for rollback", draft, async () => {
    await stacksApi.update(orgId, projectId, stackId, { rollback_enabled: draft.value.on, image_retention: draft.value.count }, token)
    await qc.invalidateQueries({ queryKey: ["stack", orgId, projectId, stackId] })
    await qc.invalidateQueries({ queryKey: ["build-config", orgId, projectId] })
  }, !!stack)

  const { on, count } = draft.value
  const set = (p: Partial<typeof draft.value>) => draft.setValue((v) => ({ ...v, ...p }))

  return (
    <div className="rounded-lg border border-border/60 px-4 py-3 space-y-2" data-testid="stack-images-setting">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        <div className="flex w-52 items-center gap-2">
          <RotateCcw className="h-3.5 w-3.5 text-muted-foreground/60" />
          <h2 className="text-sm font-medium">Images kept for rollback</h2>
        </div>
        <label className="flex w-72 items-center gap-2 text-xs text-muted-foreground">
          <Switch checked={on} onCheckedChange={(v) => set({ on: Boolean(v) })} aria-label="Limit kept images" />
          {on ? "Each built service keeps its last" : "Every image each built service builds"}
        </label>
        <CountStepper value={count} min={1} max={50} unit="image" aria-label="Images to keep" disabled={!on}
          className="w-40" onChange={(n) => set({ count: n })} />
      </div>
      <p className="min-h-8 text-[11px] text-muted-foreground/70">
        {on
          ? "Older images are removed when this is saved and after each build. A service that sets its own rollback in the compose file keeps its own."
          : "Nothing is removed, so the registry grows with every build. Any deployment whose image is kept can be rolled back to from its service's Deployments."}
      </p>
    </div>
  )
}
