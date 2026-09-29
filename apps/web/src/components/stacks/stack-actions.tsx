import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { AlertTriangle, Loader2, PlayCircle, RefreshCw, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import {
  stacks as stacksApi,
  type ApiStack,
  type ApplyStackResult,
  type DestroyStackResult,
  type SyncStackResult,
} from "@/lib/api"
import { invalidateProjectViews } from "@/lib/project-views"

// What a stack does, in its header, as a service's Deploy and Start are in
// its: Sync for a stack read from git, Apply, Destroy. They were spread over
// the Editor and Services tabs, so the stack's own actions moved with the tab
// you happened to be on.

// Sync and Apply open their rollout, which goes on after the request; what
// is kept here is Destroy's result, which has none.
type Outcome = { kind: "destroy"; result: DestroyStackResult }

export function useStackActions({ stack, orgId, projectId, token }: {
  stack: ApiStack | undefined
  orgId: string | undefined
  projectId: string
  token: string
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const stackId = stack?.id ?? ""
  const isGit = !!stack?.git_mode
  const synced = !!stack?.git_last_synced_at
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const [warning, setWarning] = useState<{ message: string; mode: string } | null>(null)
  const [confirmDestroy, setConfirmDestroy] = useState(false)
  // Both off on every open: the extra destruction is opt-in each time, never
  // remembered from a previous run.
  const [deleteVolumes, setDeleteVolumes] = useState(false)
  const [deleteRoutes, setDeleteRoutes] = useState(false)

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["stack", orgId, projectId, stackId] })
    qc.invalidateQueries({ queryKey: ["stack-services", orgId, projectId, stackId] })
    qc.invalidateQueries({ queryKey: ["volumes", orgId, projectId] })
    qc.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
    qc.invalidateQueries({ queryKey: ["stack-runs", orgId, projectId, stackId] })
    invalidateProjectViews(qc, orgId, projectId)
  }

  const apply = useMutation({
    mutationFn: () => stacksApi.apply(orgId!, projectId, stackId, token),
    onSuccess: (result) => { setOutcome(null); refresh(); openRollout(result) },
  })
  const sync = useMutation({
    mutationFn: () => stacksApi.sync(orgId!, projectId, stackId, token),
    onSuccess: (result) => {
      setOutcome(null)
      openRollout(result)
      setWarning(result.warning && result.suggested_mode && result.suggested_mode !== stack?.git_mode
        ? { message: result.warning, mode: result.suggested_mode } : null)
      refresh()
    },
  })
  const switchMode = useMutation({
    mutationFn: (mode: string) => stacksApi.update(orgId!, projectId, stackId, { git_mode: mode as "file" | "repo" }, token),
    onSuccess: () => { setWarning(null); refresh() },
  })
  const destroy = useMutation({
    mutationFn: () => stacksApi.destroy(orgId!, projectId, stackId, token,
      { delete_volumes: deleteVolumes, delete_routes: deleteRoutes }),
    onSuccess: (result) => { setConfirmDestroy(false); setOutcome({ kind: "destroy", result }); refresh() },
  })

  // What Destroy would take, said before it takes it.
  const { data: services = [] } = useQuery({
    queryKey: ["stack-services", orgId, projectId, stackId],
    queryFn: () => stacksApi.listServices(orgId!, projectId, stackId, token),
    enabled: !!orgId && !!stackId && confirmDestroy,
  })

  // Where a Sync or Apply is followed: its run, on the Rollouts tab.
  function openRollout(result: ApplyStackResult | SyncStackResult) {
    if (!result.run_id) return
    navigate({ to: "/projects/$id/stacks/$stackId/rollouts/$runId",
      params: { id: projectId, stackId, runId: result.run_id } })
  }

  const busy = apply.isPending || sync.isPending || destroy.isPending
  const failure = (apply.error || sync.error || destroy.error || switchMode.error) as Error | null

  const buttons = stack && (
    <>
      <Button size="sm" variant="outline" className="text-destructive hover:text-destructive" disabled={busy}
        onClick={() => { setDeleteVolumes(false); setDeleteRoutes(false); setConfirmDestroy(true) }}>
        {destroy.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}Destroy
      </Button>
      {/* On a git stack Apply is the last sync again - its file, its mounted
          files and, for what it builds, its commit - and never fetches:
          only Sync brings in anything newer. Before a first sync there is
          nothing to apply again. */}
      <Button size="sm" variant={isGit ? "outline" : "default"} disabled={busy || (isGit && !synced)} onClick={() => apply.mutate()}
        title={isGit
          ? synced
            ? `Apply the last sync again${stack.git_last_sync_sha ? `, commit ${stack.git_last_sync_sha.slice(0, 7)}` : ""}: nothing new is fetched`
            : "Sync first: there is nothing to apply again yet"
          : undefined}>
        {apply.isPending ? <Loader2 className="size-4 animate-spin" /> : <PlayCircle className="size-4" />}{isGit ? "Apply again" : "Apply"}
      </Button>
      {isGit && (
        <Button size="sm" disabled={busy} onClick={() => sync.mutate()}>
          {sync.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}Sync
        </Button>
      )}
    </>
  )

  const panel = (outcome || warning || failure) && (
    <div className="space-y-2">
      {failure && (
        <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {failure.message}
        </div>
      )}
      {warning && (
        <div className="flex items-start justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3">
          <div className="flex items-start gap-2.5 min-w-0">
            <AlertTriangle className="h-4 w-4 text-amber-400 mt-0.5 shrink-0" />
            <p className="text-sm text-amber-300/90">{warning.message}</p>
          </div>
          <Button size="sm" variant="outline" className="shrink-0" disabled={switchMode.isPending}
            onClick={() => switchMode.mutate(warning.mode)}>
            {switchMode.isPending && <Loader2 className="size-4 animate-spin" />}
            Switch to {warning.mode === "repo" ? "whole repository" : "file only"}
          </Button>
        </div>
      )}
      {outcome && <OutcomePanel outcome={outcome} onClose={() => setOutcome(null)} />}
    </div>
  )

  const dialog = (
    <Dialog open={confirmDestroy} onOpenChange={setConfirmDestroy}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Destroy this stack?</DialogTitle>
          <DialogDescription>
            {services.length > 0
              ? `The ${services.length} service${services.length === 1 ? "" : "s"} this stack created ${services.length === 1 ? "is" : "are"} removed from the cluster and from Meshploy. The stack and its file stay, so Apply recreates them.`
              : "The stack and its file stay, so Apply recreates what it runs."}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-1">
          <Toggle title="Also delete volumes" checked={deleteVolumes} onChange={setDeleteVolumes}
            text="Deletes the volumes this stack created and everything stored in them. This cannot be undone: applying again gives you empty volumes." />
          <Toggle title="Also delete routes" checked={deleteRoutes} onChange={setDeleteRoutes}
            text="Frees the hostnames this stack published. Anyone using them stops being able to reach it, and applying again may not produce the same hostname." />
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => setConfirmDestroy(false)}>Cancel</Button>
          <Button variant="destructive" size="sm" onClick={() => destroy.mutate()} disabled={destroy.isPending}>
            {destroy.isPending && <Loader2 className="size-4 animate-spin" />}Destroy
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )

  return { buttons, panel, dialog }
}

function OutcomePanel({ outcome, onClose }: { outcome: Outcome; onClose: () => void }) {
  const rows: [string, string[]][] = [["Destroyed", outcome.result.destroyed ?? []],
    ["Volumes deleted", outcome.result.volumes ?? []], ["Routes deleted", outcome.result.routes ?? []]]
  const errors = outcome.result.errors ?? []
  const shown = rows.filter(([, v]) => v.length > 0)
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3 text-xs space-y-1.5">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm font-medium">
          Destroyed{shown.length === 0 && errors.length === 0 && (
            <span className="font-normal text-muted-foreground">: nothing was left to remove</span>
          )}
        </p>
        <button type="button" onClick={onClose} aria-label="Dismiss" className="text-muted-foreground hover:text-foreground">
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
      {shown.map(([label, v]) => (
        <p key={label}><span className="text-muted-foreground">{label}: </span>{v.join(", ")}</p>
      ))}
      {errors.map((e) => <p key={e} className="text-destructive">{e}</p>)}
    </div>
  )
}

function Toggle({ title, text, checked, onChange }: {
  title: string; text: string; checked: boolean; onChange: (v: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/20 p-3">
      <div className="space-y-0.5">
        <p className="text-xs font-medium text-foreground">{title}</p>
        <p className="text-[11px] text-muted-foreground/70">{text}</p>
      </div>
      <Switch checked={checked} onCheckedChange={(v) => onChange(Boolean(v))} />
    </div>
  )
}
