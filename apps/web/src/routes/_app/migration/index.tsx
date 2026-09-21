import { createFileRoute } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { ArrowRight, Check, Loader2, RotateCcw, ShieldAlert, Trash2 } from "lucide-react"
import { system as systemApi, type GroupProgress, type MigrationStage, type MigrationState } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ResourceIntro } from "@/components/layout/resource-workbench"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { useAuthStore } from "@/store/auth-store"
import { formatRelativeTime } from "@/lib/utils"

// Migrating this server off another platform.
//
// Nothing here runs in the browser or in the API: every action queues a request
// for the host agent on the gateway, which does the work as root and writes
// back what happened. So the page is a view of the host's own record, and the
// buttons are requests - which is why each one reports "queued" and the state
// arrives on the next refresh.
//
// The order is the safety property and the page shows it as an order: prepare
// builds the new side with nothing serving, each group moves when the operator
// says so, cutover hands over the ports, and finish - the one thing that cannot
// be undone - waits until everything else has happened.

export const Route = createFileRoute("/_app/migration/")({
  component: MigrationPage,
})

function MigrationPage() {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [confirmFinish, setConfirmFinish] = useState(false)
  const [takeVolumes, setTakeVolumes] = useState(false)
  const [rollbackGroup, setRollbackGroup] = useState<GroupProgress | null>(null)

  const { data, isPending, error } = useQuery<MigrationState>({
    queryKey: ["migration"],
    queryFn: () => systemApi.migration(token),
    // A stage runs on the host; this is how its progress arrives.
    refetchInterval: 5_000,
    retry: false,
    throwOnError: false,
  })

  const run = useMutation({
    mutationFn: ({ stage, group, volumes }: { stage: MigrationStage; group?: string; volumes?: boolean }) =>
      systemApi.requestMigration(token, stage, { group, volumes }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["migration"] }),
  })

  if (isPending) {
    return (
      <div className="console-page p-6 flex items-center gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Reading the host…</span>
      </div>
    )
  }

  // Only the owner sees this page at all, and a server that was never planned
  // has nothing to show.
  if (error || !data) {
    return (
      <div className="console-page p-6">
        <EmptyState>
          This server has no migration. One is planned during setup, when Meshploy is installed beside
          another platform.
        </EmptyState>
      </div>
    )
  }

  const status = data.status
  const groups = status?.groups ?? []
  const moved = groups.filter((g) => g.moved).length
  const allMoved = groups.length > 0 && moved === groups.length
  const busy = run.isPending || Object.values(data.requests ?? {}).some((r) => r.state === "queued" || r.state === "running")

  return (
    <div className="console-page p-6 space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Migration</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Moving this server's workloads onto Meshploy, a group at a time. Every step runs on the machine
          itself; nothing is taken until you ask for it.
        </p>
      </div>

      {!data.agent_reporting && (
        <Notice tone="warn">
          The host agent is not reporting, so nothing would run these steps. On the gateway:{" "}
          <code className="font-mono">sudo meshploy host start</code>
        </Notice>
      )}

      {status?.finished ? (
        <Notice tone="done">
          This migration is finished. The platform it moved from has been removed, and there is nothing
          left to undo.
        </Notice>
      ) : (
        <Stages status={status} moved={moved} total={groups.length} />
      )}

      {/* ── Stage 1 ─────────────────────────────────────────────────────── */}
      {!status?.finished && (
        <section className="space-y-3">
          <ResourceIntro
            title="Prepare"
            description="Builds the Meshploy side of the plan with everything stopped and every route paused. It touches nothing of the old platform's, and can be run again."
            action={
              <Button size="sm" variant={status?.prepared ? "outline" : "default"} disabled={busy || !data.agent_reporting}
                onClick={() => run.mutate({ stage: "prepare" })}>
                {status?.prepared ? "Run again" : "Prepare"}
              </Button>
            }
          />
          <RequestLine state={data.requests?.["migrate.prepare"]} />
        </section>
      )}

      {/* ── Stage 2 ─────────────────────────────────────────────────────── */}
      {!status?.finished && (
        <section className="space-y-3">
          <ResourceIntro
            title="Groups"
            description="A group is what has to move together so its data never lives in two places: a database with every application that uses it. Moving one stops the old copies, brings the new ones up and switches its domains."
          />
          {groups.length === 0 ? (
            <EmptyState>No groups yet. Prepare first, or re-read the plan from setup.</EmptyState>
          ) : (
            <div className="space-y-2">
              {groups.map((g) => (
                <GroupRow
                  key={g.id}
                  group={g}
                  busy={busy || !status?.prepared || !data.agent_reporting}
                  onMove={() => run.mutate({ stage: "move", group: g.id })}
                  onRollback={() => setRollbackGroup(g)}
                />
              ))}
            </div>
          )}
        </section>
      )}

      {/* ── Stage 3 ─────────────────────────────────────────────────────── */}
      {!status?.finished && (
        <section className="space-y-3">
          <ResourceIntro
            title="Hand over the ports"
            description="Carries the certificates across, stops the old edge and starts Meshploy's on 80 and 443. Seconds of refused connections, and every group must have moved first."
            action={
              <Button size="sm" variant={status?.cut_over ? "outline" : "default"}
                disabled={busy || !allMoved || !data.agent_reporting || status?.cut_over}
                onClick={() => run.mutate({ stage: "cutover" })}>
                {status?.cut_over ? "Done" : "Cut over"}
              </Button>
            }
          />
          {!allMoved && groups.length > 0 && (
            <p className="text-xs text-muted-foreground">
              {groups.length - moved} {groups.length - moved === 1 ? "group has" : "groups have"} not moved yet.
            </p>
          )}
          <RequestLine state={data.requests?.["migrate.cutover"]} />
        </section>
      )}

      {/* ── Stage 4 ─────────────────────────────────────────────────────── */}
      {!status?.finished && (
        <section className="space-y-3">
          <ResourceIntro
            title="Finish"
            description="Removes what is left of the old platform. Until you do this, everything above can be put back; afterwards, nothing can."
            action={
              <Button size="sm" variant="destructive"
                disabled={busy || !status?.cut_over || !data.agent_reporting}
                onClick={() => setConfirmFinish(true)}>
                <Trash2 className="h-3.5 w-3.5" />Finish
              </Button>
            }
          />
          {!status?.cut_over && (
            <p className="text-xs text-muted-foreground">Available once the ports have been handed over.</p>
          )}
          <RequestLine state={data.requests?.["migrate.finish"]} />
        </section>
      )}

      {/* ── Undo ────────────────────────────────────────────────────────── */}
      {!status?.finished && (moved > 0 || status?.cut_over) && (
        <section className="space-y-2">
          <div className="flex items-center justify-between rounded-lg border border-border p-4">
            <div>
              <p className="text-sm font-medium">Put everything back</p>
              <p className="text-xs text-muted-foreground mt-0.5">
                Returns every group to the old platform and gives it back the ports. Data written into
                Meshploy since a group moved stays in Meshploy.
              </p>
            </div>
            <Button size="sm" variant="outline" disabled={busy || !data.agent_reporting}
              onClick={() => run.mutate({ stage: "rollback" })}>
              <RotateCcw className="h-3.5 w-3.5" />Roll back all
            </Button>
          </div>
          <RequestLine state={data.requests?.["migrate.rollback"]} />
        </section>
      )}

      {status?.updated_at && (
        <p className="text-[11px] text-muted-foreground/50">
          Read from the gateway {formatRelativeTime(new Date(status.updated_at))}.
        </p>
      )}

      {/* Finishing is the one thing that cannot be undone, so it is spelled out. */}
      <Dialog open={confirmFinish} onOpenChange={setConfirmFinish}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove the old platform?</DialogTitle>
            <DialogDescription>
              Its workloads, its own components, its network and its configuration directory are removed
              from this machine. Afterwards there is nothing to roll back to.
            </DialogDescription>
          </DialogHeader>
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1" checked={takeVolumes}
              onChange={(e) => setTakeVolumes(e.target.checked)} />
            <span>
              Remove its volumes too.
              <span className="block text-xs text-muted-foreground">
                Their data is gone for good. Left unticked, the volumes stay on the disk holding the data
                as it was when each group moved.
              </span>
            </span>
          </label>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmFinish(false)}>Cancel</Button>
            <Button variant="destructive" onClick={() => {
              run.mutate({ stage: "finish", volumes: takeVolumes })
              setConfirmFinish(false)
            }}>
              Remove it
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!rollbackGroup} onOpenChange={(open) => !open && setRollbackGroup(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Put {rollbackGroup?.name} back?</DialogTitle>
            <DialogDescription>
              Its domains go back to the old platform and its copies there start again, with their data as
              it was at the move. Anything written into Meshploy since then stays in Meshploy.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRollbackGroup(null)}>Cancel</Button>
            <Button variant="outline" onClick={() => {
              run.mutate({ stage: "rollback", group: rollbackGroup!.id })
              setRollbackGroup(null)
            }}>
              Roll it back
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ── Pieces ────────────────────────────────────────────────────────────────────

function Stages({ status, moved, total }: { status: MigrationState["status"]; moved: number; total: number }) {
  const steps = [
    { label: "Prepared", done: !!status?.prepared },
    { label: total > 0 ? `Moved ${moved}/${total}` : "Groups moved", done: total > 0 && moved === total },
    { label: "Ports handed over", done: !!status?.cut_over },
    { label: "Finished", done: !!status?.finished },
  ]
  return (
    <div className="flex flex-wrap items-center gap-2">
      {steps.map((s, i) => (
        <div key={s.label} className="flex items-center gap-2">
          <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs ${
            s.done ? "border-emerald-500/30 bg-emerald-500/5 text-emerald-300" : "border-border text-muted-foreground"
          }`}>
            {s.done && <Check className="h-3 w-3" />}
            {s.label}
          </span>
          {i < steps.length - 1 && <ArrowRight className="h-3 w-3 text-muted-foreground/30" />}
        </div>
      ))}
    </div>
  )
}

function GroupRow({ group, busy, onMove, onRollback }: {
  group: GroupProgress
  busy: boolean
  onMove: () => void
  onRollback: () => void
}) {
  return (
    <div className="rounded-lg border border-border p-4">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium truncate">{group.name}</p>
            {group.moved ? (
              <Badge variant="outline" className="border-emerald-500/30 text-emerald-300">Moved</Badge>
            ) : !group.can_move ? (
              <Badge variant="outline" className="border-amber-500/30 text-amber-300">Needs you</Badge>
            ) : null}
          </div>
          {group.members && group.members.length > 0 && (
            <p className="text-xs text-muted-foreground mt-1">{group.members.join(", ")}</p>
          )}
          {group.data && group.data.length > 0 && (
            <p className="text-xs text-muted-foreground/70 mt-1">carries {group.data.join(", ")}</p>
          )}
          {!group.moved && group.downtime && (
            <p className="text-xs text-muted-foreground/70 mt-1">downtime: {group.downtime}</p>
          )}
          {group.moved && group.moved_at && (
            <p className="text-xs text-muted-foreground/70 mt-1">moved {formatRelativeTime(new Date(group.moved_at))}</p>
          )}
          {group.blockers?.map((b) => (
            <p key={b} className="text-xs text-amber-400/80 mt-1">{b}</p>
          ))}
          {/* A failed move put the group back, so this is a reason, not damage. */}
          {group.error && (
            <p className="text-xs text-destructive mt-1 flex items-start gap-1.5">
              <ShieldAlert className="h-3.5 w-3.5 shrink-0 mt-0.5" />
              <span>{group.error}</span>
            </p>
          )}
        </div>
        {group.moved ? (
          <Button size="sm" variant="ghost" disabled={busy} onClick={onRollback}>
            <RotateCcw className="h-3.5 w-3.5" />Put back
          </Button>
        ) : (
          <Button size="sm" disabled={busy || !group.can_move} onClick={onMove}>Move</Button>
        )}
      </div>
    </div>
  )
}

// RequestLine shows what the host agent is doing with the last request of this
// kind: queued until it picks it up, then running, then its outcome.
function RequestLine({ state }: { state?: { state: string; requested_at: string; error?: string } }) {
  if (!state) return null
  const running = state.state === "queued" || state.state === "running"
  return (
    <p className={`text-xs flex items-center gap-1.5 ${state.state === "failed" ? "text-destructive" : "text-muted-foreground"}`}>
      {running && <Loader2 className="h-3 w-3 animate-spin" />}
      {state.state === "queued" && "Queued for the gateway…"}
      {state.state === "running" && "Running on the gateway…"}
      {state.state === "succeeded" && `Finished ${formatRelativeTime(new Date(state.requested_at))}`}
      {state.state === "failed" && (state.error || "It did not finish")}
    </p>
  )
}

function Notice({ tone, children }: { tone: "warn" | "done"; children: React.ReactNode }) {
  const cls = tone === "done"
    ? "border-emerald-500/30 bg-emerald-500/5"
    : "border-amber-500/30 bg-amber-500/5"
  return <div className={`rounded-lg border p-4 text-sm ${cls}`}>{children}</div>
}

function EmptyState({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-border p-8 text-center text-sm text-muted-foreground/60">
      {children}
    </div>
  )
}
