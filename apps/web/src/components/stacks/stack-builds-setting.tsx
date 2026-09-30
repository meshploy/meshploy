import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Hammer } from "lucide-react"
import { Switch } from "@/components/ui/switch"
import { stacks as stacksApi } from "@/lib/api"
import { CountStepper } from "@/components/forms/steppers"
import { useConfigDraft, useConfigSave } from "@/components/layout/config-save-bar"

// How many of a stack's services a rollout builds at once. Off, every
// service in a layer builds together, which a small server may not have the
// memory or CPU for; on, the rest wait their turn, and a service that only
// runs an image never waits. Saved with the page's floating bar, and laid out
// the same on or off, so turning it over moves nothing.
export function StackBuildsSetting({ orgId, projectId, stackId, token }: {
  orgId: string; projectId: string; stackId: string; token: string
}) {
  const qc = useQueryClient()
  const { data: stack } = useQuery({
    queryKey: ["stack", orgId, projectId, stackId],
    queryFn: () => stacksApi.get(orgId, projectId, stackId, token),
  })
  const draft = useConfigDraft({ on: false, count: 2 })
  useEffect(() => {
    if (!stack) return
    const max = stack.max_parallel_builds ?? 0
    draft.sync({ on: max > 0, count: max > 0 ? max : 2 })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stack])
  useConfigSave("Builds at once", draft, async () => {
    await stacksApi.update(orgId, projectId, stackId, { max_parallel_builds: draft.value.on ? draft.value.count : 0 }, token)
    await qc.invalidateQueries({ queryKey: ["stack", orgId, projectId, stackId] })
  }, !!stack)

  const { on, count } = draft.value
  const set = (p: Partial<typeof draft.value>) => draft.setValue((v) => ({ ...v, ...p }))

  return (
    <div className="rounded-lg border border-border/60 px-4 py-3 space-y-2" data-testid="stack-builds-setting">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        <div className="flex w-52 items-center gap-2">
          <Hammer className="h-3.5 w-3.5 text-muted-foreground/60" />
          <h2 className="text-sm font-medium">Builds at once</h2>
        </div>
        <label className="flex w-72 items-center gap-2 text-xs text-muted-foreground">
          <Switch checked={on} onCheckedChange={(v) => set({ on: Boolean(v) })} aria-label="Limit parallel builds" />
          {on ? "A rollout builds at most" : "No limit: a layer builds together"}
        </label>
        <CountStepper value={count} min={1} max={20} unit="build" aria-label="Builds at once" disabled={!on}
          className="w-40" onChange={(n) => set({ count: n })} />
      </div>
      <p className="min-h-8 text-[11px] text-muted-foreground/70">
        {on
          ? "The next build starts as one finishes building, and a service that only runs an image never waits. For a small server a layer of builds would run out of memory or CPU."
          : "Each build may use up to 4 GiB and all the spare CPU, so many at once can run a small server out of memory. Limit it if builds make the server unresponsive."}
      </p>
    </div>
  )
}
