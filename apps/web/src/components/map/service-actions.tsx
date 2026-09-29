import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowRight, ArrowUp, Loader2, RotateCcw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { PromoteDialog, builtHere, planPromotion, useRolledBackFrom } from "@/components/projects/environment-board"
import { deployments as deploymentsApi, projects as projectsApi, type ApiDeployment, type ApiService, type BoardGroup, type EnvironmentLevel } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"
import { cn } from "@/lib/utils"

// What the panel offers for a service beyond its problems: where its copies
// run across the levels, Promote into and out of this one (the board's own
// rules and dialog), and Roll back to what it ran before.

const tag = (image: string) => image.split(":").pop()?.slice(0, 12) ?? image

export function ServiceActions({ orgId, levelId, service, token }: {
  orgId: string; levelId: string; service: ApiService; token: string
}) {
  return (
    <div className="space-y-4">
      <AcrossLevels orgId={orgId} levelId={levelId} service={service} token={token} />
      <RollBack orgId={orgId} levelId={levelId} service={service} token={token} />
    </div>
  )
}

function AcrossLevels({ orgId, levelId, service, token }: { orgId: string; levelId: string; service: ApiService; token: string }) {
  const { data: board } = useQuery({
    queryKey: ["board", orgId, levelId],
    queryFn: () => projectsApi.board(orgId, levelId, token),
  })
  const [confirming, setConfirming] = useState<{ group: BoardGroup; from: EnvironmentLevel; to: EnvironmentLevel; overwrite: boolean } | null>(null)
  if (!board || board.levels.length < 2) return null

  const group = board.groups.find((g) => g.cells.some((c) => c.service_id === service.id))
  const byId = new Map(board.levels.map((l) => [l.project_id, l]))
  const here = byId.get(levelId)

  if (!group) {
    return (
      <Section title="Across levels">
        <p className="text-muted-foreground">Not in a promotion group, so it stays in {here?.name ?? "this level"}.</p>
        <Link to="/projects/$id" params={{ id: levelId }} className="mt-1 inline-flex items-center gap-1 text-primary hover:underline">
          Put it in a group on the board<ArrowRight className="h-3 w-3" />
        </Link>
      </Section>
    )
  }
  const mine = group.cells.find((c) => c.service_id === service.id)!
  const lineage = mine.lineage_id
  const path = group.path.map((id) => byId.get(id)).filter((l): l is EnvironmentLevel => !!l)
  const at = path.findIndex((l) => l.project_id === levelId)
  const below = at > 0 ? path[at - 1] : undefined
  const above = at >= 0 && at < path.length - 1 ? path[at + 1] : undefined
  // The board's own rules: newer images move; a hotfix above is replaced only by Overwrite.
  const offer = (from: EnvironmentLevel, to: EnvironmentLevel) => {
    const ahead = planPromotion(group, from, to).moves.length > 0
    const diverged = !ahead && group.cells.some((c) => c.level_id === to.project_id && builtHere(c, group)) &&
      planPromotion(group, from, to, true).moves.length > 0
    return { ahead, diverged }
  }

  return (
    <Section title={`Across levels · ${group.single ? "on its own" : `group ${group.name}`}`}>
      <div className="space-y-1">
        {[...path].reverse().map((l) => {
          const cell = group.cells.find((c) => c.level_id === l.project_id && c.lineage_id === lineage)
          return (
            <div key={l.project_id} className={cn("flex items-center justify-between gap-2 rounded-md px-2 py-1", l.project_id === levelId && "bg-muted/40")}>
              <span className="flex items-center gap-1.5">
                <span className={cn("h-1.5 w-1.5 rounded-full", l.production ? "bg-emerald-400" : "bg-sky-400")} />
                {l.project_id === levelId ? <span className="font-medium">{l.name}</span> : (
                  <Link to="/projects/$id/map" params={{ id: l.project_id }} className="hover:underline">{l.name}</Link>
                )}
              </span>
              <span className="truncate font-mono text-[11px] text-muted-foreground">
                {cell?.image ? tag(cell.image) : "not here"}{cell && builtHere(cell, group) ? " · hotfix" : ""}
              </span>
            </div>
          )
        })}
      </div>
      {mine.type === "database" ? (
        <p className="mt-2 text-muted-foreground">Databases are not promoted: each level borrows the one above, or keeps its own.</p>
      ) : (
        <div className="mt-2 flex flex-col gap-1.5">
          {below && <PromoteStep orgId={orgId} token={token} group={group} from={below} to={here!} {...offer(below, here!)} onClick={(overwrite) => setConfirming({ group, from: below, to: here!, overwrite })} />}
          {above && <PromoteStep orgId={orgId} token={token} group={group} from={here!} to={above} {...offer(here!, above)} onClick={(overwrite) => setConfirming({ group, from: here!, to: above, overwrite })} />}
        </div>
      )}
      {confirming && (
        <PromoteDialog group={confirming.group} from={confirming.from} to={confirming.to} overwrite={confirming.overwrite}
          orgId={orgId} token={token} onClose={() => setConfirming(null)} />
      )}
    </Section>
  )
}

// A Promote button, and under it what the target rolled back from, when the
// image it would move is one of those.
function PromoteStep({ orgId, token, group, ...button }: {
  orgId: string; token: string; group: BoardGroup
  from: EnvironmentLevel; to: EnvironmentLevel; ahead: boolean; diverged: boolean; onClick: (overwrite: boolean) => void
}) {
  const rolledBack = useRolledBackFrom(orgId, token, group, button.from, button.to)
  return (
    <div className="space-y-1">
      <PromoteButton {...button} />
      {(button.ahead || button.diverged) && rolledBack.map((r) => (
        <p key={r.service_name} className="text-amber-300">
          {button.to.name} rolled back from {r.service_name}&apos;s {tag(r.image)} {formatRelativeTime(new Date(r.at))}.
        </p>
      ))}
    </div>
  )
}

function PromoteButton({ from, to, ahead, diverged, onClick }: {
  from: EnvironmentLevel; to: EnvironmentLevel; ahead: boolean; diverged: boolean; onClick: (overwrite: boolean) => void
}) {
  return (
    <Button size="sm" variant={ahead ? "default" : "outline"} disabled={!ahead && !diverged}
      className={cn("h-7 justify-start gap-1.5 text-xs", !ahead && diverged && "border-amber-500/40 text-amber-300 hover:bg-amber-500/10")}
      onClick={() => onClick(!ahead)}>
      <ArrowUp className="h-3 w-3" />
      {ahead ? "Promote" : diverged ? "Overwrite" : "Nothing newer to promote"} {from.name} <ArrowRight className="h-3 w-3" /> {to.name}
    </Button>
  )
}

// Back to the image it ran before the one it runs now: after a promotion
// that went wrong, what production ran until then. Promote is offered again
// afterwards, since what is below is newer than what runs here once more.
function RollBack({ orgId, levelId, service, token }: { orgId: string; levelId: string; service: ApiService; token: string }) {
  const qc = useQueryClient()
  const [confirm, setConfirm] = useState(false)
  const { data: deps = [] } = useQuery({
    queryKey: ["deployments", orgId, levelId, service.id],
    queryFn: () => deploymentsApi.list(orgId, levelId, service.id, token),
  })
  const current = deps.find((d) => d.status === "success")
  const previous: ApiDeployment | undefined = current
    ? deps.find((d) => d.status === "success" && d.id !== current.id && d.image && d.image !== current.image && !d.image_removed_at)
    : undefined
  const rollback = useMutation({
    mutationFn: () => deploymentsApi.rollback(orgId, levelId, service.id, previous!.id, token),
    onSuccess: () => {
      setConfirm(false)
      for (const key of [["deployments", orgId, levelId, service.id], ["board", orgId], ["services", orgId, levelId], ["project-health", orgId, levelId], ["project-map", orgId]]) {
        qc.invalidateQueries({ queryKey: key })
      }
    },
  })
  if (service.type === "database" || service.run_once) return null

  return (
    <Section title="Roll back">
      {!previous ? (
        <p className="text-muted-foreground">Nothing earlier to go back to: no other image it ran is still kept.</p>
      ) : (
        <>
          <p className="text-muted-foreground">
            Back to <span className="font-mono text-foreground">{tag(previous.image)}</span>, which it ran{" "}
            {formatRelativeTime(new Date(previous.created_at))}
            {previous.source === "promotion" && previous.from_level ? `, promoted from ${previous.from_level}` : ""}
            {previous.source_commit_message ? `: "${previous.source_commit_message}"` : ""}.
          </p>
          {confirm ? (
            <div className="mt-2 flex items-center gap-2">
              <Button size="sm" variant="destructive" className="h-7 text-xs" disabled={rollback.isPending} onClick={() => rollback.mutate()}>
                {rollback.isPending && <Loader2 className="size-3 animate-spin" />}Roll back now
              </Button>
              <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setConfirm(false)}>Cancel</Button>
            </div>
          ) : (
            <Button size="sm" variant="outline" className="mt-2 h-7 gap-1.5 text-xs" onClick={() => setConfirm(true)}>
              <RotateCcw className="h-3 w-3" />Roll back to {tag(previous.image)}
            </Button>
          )}
          {rollback.isError && <p className="mt-1 text-destructive">{(rollback.error as Error).message}</p>}
          {rollback.isSuccess && <p className="mt-1 text-emerald-400">Rolling back. Its deployment shows how it goes.</p>}
        </>
      )}
    </Section>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="mb-1.5 text-[11px] uppercase tracking-wide text-muted-foreground">{title}</p>
      {children}
    </div>
  )
}
