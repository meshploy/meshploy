import { useEffect, useRef, useState } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowRight, ArrowUpRight, Box, ExternalLink, Database, Loader2, Minus, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react"
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { projects as projectsApi, ApiError } from "@/lib/api"
import type { Board, BoardCell, BoardGroup, EnvironmentLevel } from "@/lib/api/projects"
import { cn, formatRelativeTime } from "@/lib/utils"
import { livePoll } from "@/lib/live-poll"
import { OriginLine } from "@/components/services/deployment-origin"
import { LevelDot, NewLevelDialog } from "@/components/projects/environment-switcher"
import { HelpButton } from "@/help/help-button"
import { TermInfo } from "@/help/term"
import { LevelChain, HelpMarkdown } from "@/help/render"
import { topic as helpTopic } from "@/help/topics"
import { useHelp } from "@/help/store"

/**
 * The project's environments at a glance, and moving services up them.
 *
 * Levels are columns, lowest on the left and production on the right, because
 * promotion reads left to right. Each promotion group is a row with its own
 * path: a column the path skips holds none of the group, which is then used
 * from the nearest level above. Databases are a row of their own, since they
 * never move: each level has its own, or uses the one above.
 */
export function EnvironmentBoard({ orgId, projectId, token }: { orgId: string; projectId: string; token: string }) {
  const { data: board } = useQuery({
    queryKey: ["board", orgId, projectId],
    queryFn: () => projectsApi.board(orgId, projectId, token),
    // Live while anything on it deploys: a finished deploy changes its card.
    refetchInterval: livePoll<Board>((b) =>
      [...b.ungrouped, ...b.groups.flatMap((g) => g.cells)].some((c) => c.status === "deploying")
    ),
  })
  const [creating, setCreating] = useState(false)
  // Too narrow for every level side by side, the board shows one at a time.
  const [width, setWidth] = useState(0)
  const sectionRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const el = sectionRef.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(el)
    return () => observer.disconnect()
  }, [board === undefined])
  const [shownLevel, setShownLevel] = useState<string | null>(null)
  const [owning, setOwning] = useState<{ level: EnvironmentLevel; from: EnvironmentLevel; source: BoardCell } | null>(null)
  const [removing, setRemoving] = useState<{ cell: BoardCell; group: BoardGroup | null } | null>(null)
  const qc = useQueryClient()
  // One runner for every card action, so each says the same way when it fails.
  const act = useMutation({
    mutationFn: (run: () => Promise<unknown>) => run(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
    },
  })
  // Production alone still renders: the board teaches what levels are then.
  if (!board) return null

  // What a card's menu offers. A service in no group can be copied down (as a
  // group of its own) or added to a group; a grouped service can be brought
  // down to reproduce what runs above; a single-service group's service can
  // also join a named group, which absorbs it.
  const named = board.groups.filter((g) => !g.single)
  const levelOf = (id: string) => board.levels.find((l) => l.project_id === id)!
  const actionsFor = (cell: BoardCell, group: BoardGroup | null): CardAction[] => {
    const here = levelOf(cell.level_id)
    // Any copy below production can go; the level then uses the one above.
    // One that started here has nothing above it, so removing it deletes it.
    const onlyCopy = !copyAbove(board, cell)
    const remove: CardAction[] = here.production
      ? []
      : [{ section: `This level`, label: onlyCopy ? `Delete ${cell.service_name}` : `Remove from ${here.name}`, open: () => setRemoving({ cell, group }) }]
    if (cell.type === "database") return remove
    const below = board.levels.filter((l) => l.level > here.level).sort((a, b) => a.level - b.level)
    const out: CardAction[] = []
    if (!group) {
      for (const l of below)
        out.push({ section: "Copy to", label: l.name, run: () => projectsApi.copyToLevel(orgId, cell.level_id, cell.service_id, l.project_id, token) })
    } else {
      for (const l of below)
        out.push({ section: "Bring down to", label: l.name, run: () => projectsApi.bringDown(orgId, cell.level_id, cell.service_id, l.project_id, token) })
    }
    if (!group || group.single) {
      for (const g of named.filter((g) => g.id !== group?.id))
        out.push({ section: "Add to group", label: g.name, run: () => projectsApi.addToGroup(orgId, projectId, g.id, [cell.service_id], token) })
    }
    // Leaving a group deletes nothing: every copy keeps running, and only
    // stops moving with the group, so it needs no confirmation.
    const leave: CardAction[] = group
      ? [{
          section: "Group",
          label: group.single ? "Ungroup" : `Remove from ${group.name}`,
          run: () => projectsApi.removeFromGroup(orgId, projectId, group.id, cell.lineage_id, token),
        }]
      : []
    return [...out, ...leave, ...remove]
  }

  // Lowest first: left to right is the way services move.
  const allColumns = [...board.levels].reverse()
  const narrow = width > 0 && width < 180 + allColumns.length * 230
  const shown = allColumns.find((l) => l.project_id === shownLevel) ?? allColumns[0]
  const columns = narrow ? [shown] : allColumns
  // Narrow, a row's label sits above its cards instead of beside them.
  const grid = narrow
    ? { gridTemplateColumns: "minmax(0, 1fr)" }
    : { gridTemplateColumns: `minmax(150px, 180px) repeat(${columns.length}, minmax(190px, 1fr))` }
  const apps = board.ungrouped.filter((c) => c.type !== "database")
  const dbs = board.ungrouped.filter((c) => c.type === "database")

  return (
    <section ref={sectionRef} className="space-y-3" aria-label="Environments">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-1 text-sm font-semibold">
            Environments
            <HelpButton topic="environments" label="How environments work" className="size-6" />
          </h2>
          {board.levels.length > 1 && (
            <p className="mt-1 text-xs text-muted-foreground">
              What each level runs. Promote moves the same image up a group&apos;s path; nothing is rebuilt.
            </p>
          )}
        </div>
        {board.levels.length > 1 && (
          <Button size="sm" variant="outline" className="h-8 gap-1.5 text-xs" onClick={() => setCreating(true)}>
            <Plus className="h-3.5 w-3.5" />
            Group
          </Button>
        )}
      </div>

      {/* Production alone: nothing to move between yet, so the board teaches
          what levels are instead of showing one column. */}
      {board.levels.length < 2 && <NoLevels orgId={orgId} projectId={projectId} token={token} levels={board.levels} />}

      {act.error && (
        <p role="alert" className="text-xs text-destructive">
          {act.error instanceof ApiError ? act.error.message : "That did not work."}
        </p>
      )}

      {narrow && board.levels.length > 1 && (
        <div role="tablist" aria-label="Level" className="quiet-surface flex gap-1 overflow-x-auto rounded-lg border border-border p-1">
          {allColumns.map((l, i) => (
            <button
              key={l.project_id}
              type="button"
              role="tab"
              aria-selected={l.project_id === shown.project_id}
              onClick={() => setShownLevel(l.project_id)}
              className={cn(
                "flex shrink-0 items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors",
                l.project_id === shown.project_id ? "bg-secondary text-foreground" : "hover:text-foreground"
              )}
            >
              {i > 0 && <ArrowRight className="-ml-1 mr-0.5 h-3 w-3 text-muted-foreground/50" />}
              <LevelDot production={l.production} />
              {l.name}
            </button>
          ))}
        </div>
      )}

      {/* Wide enough, every level side by side; the grid never scrolls the page. */}
      {board.levels.length > 1 && <div className="env-board overflow-x-auto rounded-xl border border-border">
        <div className={cn("grid", !narrow && "min-w-max")} style={grid}>
          {!narrow && <div className="env-cell env-head" />}
          {columns.map((l) => (
            <div key={l.project_id} className="env-cell env-head px-3 py-2.5">
              <p className="flex items-center gap-2 text-sm font-semibold">
                <LevelDot production={l.production} />
                {l.name}
              </p>
              <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{l.namespace}</p>
            </div>
          ))}

          {board.groups.map((g) => (
            <GroupRow
              key={g.id}
              group={g}
              columns={columns}
              board={board}
              orgId={orgId}
              projectId={projectId}
              token={token}
              actionsFor={(c) => actionsFor(c, g)}
              onAction={(run) => act.mutate(run)}
            />
          ))}

          {apps.length > 0 && (
            <>
              <RowLabel
                title="Not in a group"
                detail="Deployed on their own; they do not move between levels. A level without one uses it from the nearest level above."
              />
              {columns.map((l) => {
                const own = apps.filter((c) => c.level_id === l.project_id)
                // Each service this level does not have, and whose copy it uses.
                const missing = [...new Set(apps.map((c) => c.lineage_id))]
                  .filter((lineage) => !own.some((c) => c.lineage_id === lineage))
                  .map((lineage) => {
                    const from = nearestAboveWith(l, board.levels, (lvl) =>
                      apps.some((c) => c.level_id === lvl.project_id && c.lineage_id === lineage)
                    )
                    const name = apps.find((c) => c.lineage_id === lineage)!.service_name
                    return from ? `${name}: uses ${from.name}'s` : null
                  })
                  .filter(Boolean) as string[]
                return (
                  <div key={l.project_id} className="env-cell space-y-2 p-2">
                    {own.map((c) => (
                      <ServiceCard key={c.service_id} cell={c} actions={actionsFor(c, null)} onAction={(run) => act.mutate(run)} />
                    ))}
                    {missing.length > 0 && <Borrowed text={missing.join(" · ")} term="environments.borrowed" />}
                  </div>
                )
              })}
            </>
          )}

          <RowLabel title="Databases" detail="Never promoted. A level without its own copy uses the one above, data and all." term="environments.borrowed-database" />
          {columns.map((l) => {
            const own = dbs.filter((c) => c.level_id === l.project_id)
            // Each database this level does not have, and the copy it uses.
            const used = [...new Set(dbs.map((c) => c.lineage_id))]
              .filter((lineage) => !own.some((c) => c.lineage_id === lineage))
              .map((lineage) => {
                const from = nearestAboveWith(l, board.levels, (lvl) =>
                  dbs.some((c) => c.level_id === lvl.project_id && c.lineage_id === lineage)
                )
                const source = from && dbs.find((c) => c.level_id === from.project_id && c.lineage_id === lineage)
                return from && source ? { from, source } : null
              })
              .filter(Boolean) as { from: EnvironmentLevel; source: BoardCell }[]
            return (
              <div key={l.project_id} className="env-cell space-y-2 p-2">
                {own.map((c) => (
                  <ServiceCard key={c.service_id} cell={c} actions={actionsFor(c, null)} onAction={(run) => act.mutate(run)} />
                ))}
                {used.map(({ from, source }) => (
                  <div
                    key={source.lineage_id}
                    className="flex items-center justify-between gap-2 rounded-md border border-dashed border-amber-500/40 bg-amber-500/5 px-2.5 py-2 text-[11px] text-amber-300/90"
                  >
                    <span>
                      <strong className="font-semibold">{source.service_name}</strong>: uses {from.name}&apos;s. Writes here change{" "}
                      {from.name}&apos;s data.
                    </span>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-6 shrink-0 px-2 text-[11px]"
                      onClick={() => setOwning({ level: l, from, source })}
                    >
                      Own copy
                    </Button>
                  </div>
                ))}
                {own.length === 0 && used.length === 0 && <Borrowed text="None" />}
              </div>
            )
          })}
        </div>
      </div>}

      {creating && (
        <NewGroupDialog board={board} orgId={orgId} projectId={projectId} token={token} onClose={() => setCreating(false)} />
      )}
      {owning && <OwnDatabaseDialog {...owning} orgId={orgId} token={token} onClose={() => setOwning(null)} />}
      {removing && (
        <RemoveFromLevelDialog
          {...removing}
          board={board}
          orgId={orgId}
          token={token}
          onClose={() => setRemoving(null)}
        />
      )}
    </section>
  )
}

function nearestAboveWith(level: EnvironmentLevel, levels: EnvironmentLevel[], has: (l: EnvironmentLevel) => boolean) {
  // levels run production first, so walking backwards from this one climbs.
  const above = levels.filter((l) => l.level < level.level).sort((a, b) => b.level - a.level)
  return above.find(has)
}

function RowLabel({ title, detail, term, children }: { title: string; detail: string; term?: string; children?: React.ReactNode }) {
  return (
    <div className="env-cell px-3 py-3">
      <p className="flex items-center gap-1 text-xs font-semibold">
        {title}
        {term && <TermInfo id={term} />}
      </p>
      <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{detail}</p>
      {children}
    </div>
  )
}

function Borrowed({ text, warn, term }: { text: string; warn?: boolean; term?: string }) {
  return (
    <div
      className={cn(
        "flex min-h-12 items-center justify-between gap-2 rounded-md border border-dashed px-2.5 py-2 text-[11px] leading-relaxed",
        warn ? "border-amber-500/40 bg-amber-500/5 text-amber-300/90" : "border-border/70 text-muted-foreground"
      )}
    >
      <span>{text}</span>
      {term && <TermInfo id={term} />}
    </div>
  )
}

/**
 * A project with production alone: what levels are, drawn and in two
 * sentences from the help topic, with the way to add one and the way to read
 * more. The first time someone meets environments is the moment they want to
 * learn them.
 */
function NoLevels({ orgId, projectId, token, levels }: { orgId: string; projectId: string; token: string; levels: EnvironmentLevel[] }) {
  const [adding, setAdding] = useState(false)
  const show = useHelp((s) => s.show)
  const navigate = useNavigate()
  const intro = helpTopic("environments")?.sections.find((s) => s.id === "levels")?.body.split("\n\n")[0] ?? ""
  return (
    <div className="env-board space-y-4 rounded-xl border border-border p-5" data-testid="no-levels">
      <LevelChain levels={["dev", "staging", "production"]} className="w-fit" />
      <HelpMarkdown source={intro} className="max-w-2xl" />
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={() => setAdding(true)}>
          <Plus className="h-3.5 w-3.5" />
          Add a level
        </Button>
        <Button size="sm" variant="outline" onClick={() => show("environments")}>
          How environments work
        </Button>
      </div>
      {adding && (
        <NewLevelDialog
          orgId={orgId}
          projectId={projectId}
          levels={levels}
          token={token}
          onClose={() => setAdding(false)}
          onCreated={(l) => {
            setAdding(false)
            navigate({ to: `/projects/${l.project_id}/` })
          }}
        />
      )}
    </div>
  )
}

function shortImage(image: string) {
  const last = image.split("/").pop() ?? image
  return last.length > 28 ? last.slice(0, 27) + "…" : last
}

interface CardAction {
  section: string
  label: string
  /** Done at once. */
  run?: () => Promise<unknown>
  /** Opens a confirmation instead, for what cannot be undone. */
  open?: () => void
}

function ServiceCard({
  cell,
  note,
  hotfix = false,
  actions = [],
  onAction,
}: {
  cell: BoardCell
  note?: string
  /** Runs something built at this level instead of promoted to it. */
  hotfix?: boolean
  actions?: CardAction[]
  onAction?: (run: () => Promise<unknown>) => void
}) {
  const running = cell.status === "running"
  const sections = [...new Set(actions.map((a) => a.section))]
  return (
    <div className="relative rounded-md border border-border/60 bg-background/40 transition-colors hover:border-primary/40">
      <Link
        to="/projects/$id/services/$serviceId"
        params={{ id: cell.level_id, serviceId: cell.service_id }}
        className="block px-2.5 py-2"
      >
      <p className={cn("flex items-center justify-between gap-2 text-xs font-medium", actions.length > 0 && "pr-6")}>
        <span className="flex min-w-0 items-center gap-1.5">
          {cell.type === "database" ? <Database className="h-3 w-3 shrink-0" /> : <Box className="h-3 w-3 shrink-0" />}
          <span className="truncate">{cell.service_name}</span>
          {hotfix && (
            <span
              className="shrink-0 rounded border border-amber-500/40 px-1 text-[10px] font-normal text-amber-300"
              title="Built at this level, not promoted to it: a hotfix, until a promotion replaces it"
            >
              built here
            </span>
          )}
        </span>
        <span className={cn("size-1.5 shrink-0 rounded-full", running ? "bg-emerald-400" : "bg-muted-foreground/40")} />
      </p>
      <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
        {cell.image ? shortImage(cell.image) : "not deployed"}
      </p>
      {cell.image && <OriginLine origin={cell} className="mt-0.5" />}
      {(note || (cell.image && cell.deployed_at)) && (
        <p className="mt-0.5 text-[11px] text-muted-foreground/80">
          {note ?? formatRelativeTime(new Date(cell.deployed_at!))}
        </p>
      )}
      </Link>
      {cell.routes && cell.routes.length > 0 && <CardRoute routes={cell.routes} />}
      {actions.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label={`More for ${cell.service_name}`}
            className="absolute right-1 top-1 rounded p-1 text-muted-foreground outline-none hover:bg-muted/40 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
          >
            <MoreHorizontal className="h-3.5 w-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-[200px]">
            {sections.map((section, i) => (
              <div key={section}>
                {i > 0 && <DropdownMenuSeparator />}
                <DropdownMenuLabel className="text-xs text-muted-foreground">{section}</DropdownMenuLabel>
                {actions
                  .filter((a) => a.section === section)
                  .map((a) => (
                    <DropdownMenuItem
                      key={a.label}
                      onClick={() => (a.open ? a.open() : a.run && onAction?.(a.run))}
                      className={cn("text-sm", a.open && "text-destructive")}
                    >
                      {a.label}
                    </DropdownMenuItem>
                  ))}
              </div>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}

/**
 * The address a card opens: the service's first live route at this level,
 * with the rest counted. A route waiting for the level's first deploy is
 * shown, but not as a link, so nobody opens a name that serves nothing yet.
 */
function CardRoute({ routes }: { routes: NonNullable<BoardCell["routes"]> }) {
  const [first, ...rest] = routes
  const more = rest.length > 0 && (
    <span className="shrink-0 text-muted-foreground" title={rest.map((r) => r.hostname).join("\n")}>+{rest.length}</span>
  )
  return (
    <div className="-mt-1 flex min-w-0 items-center gap-1.5 px-2.5 pb-2 text-xs" data-testid="card-route">
      {first.live ? (
        <a
          href={`https://${first.hostname}`}
          target="_blank"
          rel="noopener noreferrer"
          className="flex min-w-0 items-center gap-1 text-primary hover:underline"
          aria-label={`Open ${first.hostname}`}
        >
          <ExternalLink className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{first.hostname}</span>
        </a>
      ) : (
        <span className="flex min-w-0 items-center gap-1 text-muted-foreground" title="Goes live with this level's first deploy">
          <ExternalLink className="h-3.5 w-3.5 shrink-0 opacity-50" />
          <span className="truncate">{first.hostname}</span>
          <span className="shrink-0 text-[11px] text-muted-foreground/70">after first deploy</span>
          <TermInfo id="environments.after-first-deploy" />
        </span>
      )}
      {more}
    </div>
  )
}

/**
 * One group across the levels. A column on its path holds the group's
 * services there, and between a column and the next one up on the path sits
 * Promote, enabled when something here differs from what is up there.
 */
function GroupRow({
  group,
  columns,
  board,
  orgId,
  projectId,
  token,
  actionsFor,
  onAction,
}: {
  group: BoardGroup
  columns: EnvironmentLevel[]
  board: Board
  orgId: string
  projectId: string
  token: string
  actionsFor: (cell: BoardCell) => CardAction[]
  onAction: (run: () => Promise<unknown>) => void
}) {
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState<{ from: EnvironmentLevel; to: EnvironmentLevel; overwrite: boolean } | null>(null)
  const [editing, setEditing] = useState(false)
  const remove = useMutation({
    mutationFn: () => projectsApi.deleteGroup(orgId, projectId, group.id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["board", orgId] }),
  })
  const byId = new Map(board.levels.map((l) => [l.project_id, l]))
  const entry = byId.get(group.path[0])

  return (
    <>
      <RowLabel
        term={group.single ? "environments.single-group" : "environments.promotion-group"}
        title={group.single ? `${group.name} · just this service` : group.name}
        detail={`Builds in ${entry?.name ?? "?"}, then ${group.path.slice(1).map((id) => byId.get(id)?.name).join(" → ")}`}
      >
        <div className="mt-2 flex flex-wrap gap-3">
          <button
            type="button"
            onClick={() => setEditing(true)}
            className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
          >
            <Pencil className="h-3 w-3" />
            Edit
          </button>
          <button
            type="button"
            onClick={() => remove.mutate()}
            className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-3 w-3" />
            Remove group
          </button>
        </div>
      </RowLabel>
      {columns.map((l) => {
        const at = group.path.indexOf(l.project_id)
        const cells = group.cells.filter((c) => c.level_id === l.project_id)
        if (at < 0) {
          const above = group.path.map((id) => byId.get(id)!).filter((p) => p && p.level < l.level).sort((a, b) => b.level - a.level)[0]
          return (
            <div key={l.project_id} className="env-cell p-2">
              <Borrowed text={`Not on this group's path: uses ${above?.name ?? "production"}'s`} />
            </div>
          )
        }
        const next = at < group.path.length - 1 ? byId.get(group.path[at + 1]) : undefined
        // Enabled when anything here is newer than what runs above: Promote
        // moves those, and leaves the rest with the reason.
        const ahead = next ? planPromotion(group, l, next).moves.length > 0 : false
        // The level above runs something built there, a hotfix, and nothing
        // here is newer: offered as Overwrite, which says what it replaces.
        const divergedAbove =
          !!next &&
          !ahead &&
          group.cells.some((c) => c.level_id === next.project_id && builtHere(c, group)) &&
          planPromotion(group, l, next, true).moves.length > 0
        return (
          <div key={l.project_id} className="env-cell flex flex-col gap-2 p-2">
            {cells.length > 0
              ? cells.map((c) => <ServiceCard key={c.service_id} cell={c} hotfix={builtHere(c, group)} actions={actionsFor(c)} onAction={onAction} />)
              : <Borrowed text={at === 0 ? "Not built yet" : "Not promoted here yet"} />}
            {next && (
              <Button
                size="sm"
                variant={ahead ? "default" : "outline"}
                disabled={!ahead && !divergedAbove}
                className={cn("mt-auto h-7 gap-1 text-xs", !ahead && divergedAbove && "border-amber-500/40 text-amber-300 hover:bg-amber-500/10")}
                onClick={() => setConfirming({ from: l, to: next, overwrite: !ahead })}
              >
                {ahead ? "Promote" : divergedAbove ? "Overwrite" : cells.some((c) => c.image) ? "Nothing newer for" : "Nothing built for"}{" "}
                <ArrowRight className="h-3 w-3" /> {next.name}
              </Button>
            )}
          </div>
        )
      })}
      {editing && (
        <EditGroupDialog group={group} board={board} orgId={orgId} projectId={projectId} token={token} onClose={() => setEditing(false)} />
      )}
      {confirming && (
        <PromoteDialog
          group={group}
          from={confirming.from}
          to={confirming.to}
          overwrite={confirming.overwrite}
          orgId={orgId}
          token={token}
          onClose={() => setConfirming(null)}
        />
      )}
    </>
  )
}

function PromoteDialog({
  group,
  from,
  to,
  overwrite = false,
  orgId,
  token,
  onClose,
}: {
  group: BoardGroup
  from: EnvironmentLevel
  to: EnvironmentLevel
  /** Replace what the level above built there, even with older images. */
  overwrite?: boolean
  orgId: string
  token: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const promote = useMutation({
    mutationFn: () => projectsApi.promote(orgId, from.project_id, group.id, token, overwrite),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      onClose()
    },
  })
  // What the server says before anything moves: a service whose variables
  // would come from below the target stays, with what the target needs; one
  // new to the target brings its own variables as they are.
  const { data: notes = [] } = useQuery({
    queryKey: ["promote-preflight", orgId, from.project_id, group.id],
    queryFn: () => projectsApi.promotePreflight(orgId, from.project_id, group.id, token),
  })
  const plan = planPromotion(group, from, to, overwrite)
  // What the level above built itself, which this replaces or must include.
  const hotfixes = group.cells.filter(
    (c) => c.level_id === to.project_id && builtHere(c, group) && plan.moves.some((m) => m.lineage_id === c.lineage_id)
  )
  const blocked = new Map(notes.filter((n) => n.blocked).map((n) => [n.service_name, n.blocked!]))
  const moves = plan.moves.filter((c) => !blocked.has(c.service_name))
  const stays: { name: string; reason: Skip; detail?: string }[] = [
    ...plan.moves.filter((c) => blocked.has(c.service_name)).map((c) => ({ name: c.service_name, reason: "from_below" as const, detail: blocked.get(c.service_name) })),
    ...plan.stays,
  ]
  const arriving = notes.filter((n) => n.new_there && !n.blocked && moves.some((c) => c.service_name === n.service_name))
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {overwrite ? `Overwrite ${to.name} with ${from.name}'s ${group.name}?` : `Promote ${group.name} to ${to.name}?`}
          </DialogTitle>
          <DialogDescription>
            Each service runs in {to.name} exactly the image it runs in {from.name}, with {to.name}&apos;s own
            variables. A service {to.name} does not have yet is created there first.
          </DialogDescription>
        </DialogHeader>
        {/* Told apart by colour before reading: green for what moves, the
            promote button's colour; muted and dashed for what stays, which is
            not a problem and so is not a warning colour. */}
        <div className="space-y-1.5">
          <p className="flex items-center gap-1.5 text-xs font-medium text-emerald-400">
            <ArrowUpRight className="h-3.5 w-3.5" />
            Moves to {to.name}
          </p>
          <ul className="space-y-1.5 rounded-md border border-emerald-500/30 bg-emerald-500/5 p-3 text-xs">
            {moves.length === 0 && <li className="text-muted-foreground">Nothing can move yet.</li>}
            {moves.map((c) => (
              <li key={c.service_id} className="flex items-center justify-between gap-3">
                <span className="font-medium text-foreground">{c.service_name}</span>
                <span className="truncate font-mono text-emerald-300/80">{shortImage(c.image)}</span>
              </li>
            ))}
          </ul>
        </div>
        {stays.length > 0 && (
          <div className="space-y-1.5">
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Minus className="h-3.5 w-3.5" />
              Stays as it is in {to.name}
            </p>
            <ul className="space-y-1.5 rounded-md border border-dashed border-border/60 bg-muted/10 p-3 text-xs text-muted-foreground">
              {stays.map((st) => (
                <li key={st.name} className="flex items-start justify-between gap-3">
                  <span className="font-medium">{st.name}</span>
                  <span className={cn("text-right", st.reason === "from_below" ? "text-amber-300/90" : "text-muted-foreground")}>
                    {skipText(st.reason, from.name, to.name, st.detail)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
        {hotfixes.length > 0 && (
          <div
            className={cn(
              "space-y-1 rounded-md border px-3 py-2 text-xs",
              overwrite ? "border-destructive/40 bg-destructive/5 text-red-300" : "border-amber-500/30 bg-amber-500/5 text-amber-300/90"
            )}
            data-testid="hotfixes"
          >
            {hotfixes.map((h) => {
              const incoming = moves.find((m) => m.lineage_id === h.lineage_id)
              const what = [h.source_branch, h.source_commit?.slice(0, 7)].filter(Boolean).join("@") || shortImage(h.image)
              const theirs = incoming && ([incoming.source_branch, incoming.source_commit?.slice(0, 7)].filter(Boolean).join("@") || shortImage(incoming.image))
              return (
                <p key={h.service_id}>
                  <strong className="font-semibold">{h.service_name}</strong> in {to.name} runs {what}, built there.{" "}
                  {overwrite
                    ? `Overwriting replaces it with ${from.name}'s ${theirs}, built earlier: the fix is gone unless that build has it.`
                    : `Make sure ${from.name}'s ${theirs} includes it.`}
                </p>
              )
            })}
          </div>
        )}
        {arriving.length > 0 && (
          <div className="space-y-1 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90" data-testid="arriving">
            {arriving.map((n) => (
              <p key={n.service_name}>
                <strong className="font-semibold">{n.service_name}</strong> is new to {to.name}.{" "}
                {n.own_variables > 0
                  ? `Its ${n.own_variables} ${n.own_variables === 1 ? "variable" : "variables"} of its own come from ${from.name} as they are: review them on its page in ${to.name}.`
                  : `It has no variables of its own to review.`}
              </p>
            ))}
          </div>
        )}
        {promote.error && (
          <p className="text-xs text-destructive">
            {promote.error instanceof ApiError ? promote.error.message : "Could not promote."}
          </p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={promote.isPending}>
            Cancel
          </Button>
          <Button variant={overwrite ? "destructive" : "default"} onClick={() => promote.mutate()} disabled={promote.isPending || moves.length === 0}>
            {promote.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {overwrite ? `Overwrite ${to.name}` : `Promote to ${to.name}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * A new group: which services move together, and through which levels. The
 * lowest level ticked is where the group builds; production is always the end.
 */
function NewGroupDialog({
  board,
  orgId,
  projectId,
  token,
  onClose,
}: {
  board: Board
  orgId: string
  projectId: string
  token: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const production = board.levels.find((l) => l.production)!
  const lower = board.levels.filter((l) => !l.production)
  const [name, setName] = useState("")
  const [levels, setLevels] = useState<Set<string>>(new Set(lower.length ? [lower[lower.length - 1].project_id] : []))
  const [picked, setPicked] = useState<Set<string>>(new Set())

  // One entry per service, whichever level it is in; those in a group already
  // are not offered.
  const candidates = new Map<string, BoardCell>()
  for (const c of board.ungrouped) {
    if (c.type === "database") continue
    const seen = candidates.get(c.lineage_id)
    if (!seen || (byLevel(board, c.level_id) ?? 99) < (byLevel(board, seen.level_id) ?? 99)) candidates.set(c.lineage_id, c)
  }
  const path = [...lower.filter((l) => levels.has(l.project_id))].sort((a, b) => b.level - a.level).map((l) => l.project_id)
  const fullPath = [...path, production.project_id]
  const entry = board.levels.find((l) => l.project_id === fullPath[0])

  const create = useMutation({
    mutationFn: () =>
      projectsApi.createGroup(orgId, projectId, { name, service_ids: [...picked], path: fullPath }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      onClose()
    },
  })
  const toggle = (set: Set<string>, id: string, update: (s: Set<string>) => void) => {
    const next = new Set(set)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    update(next)
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New promotion group</DialogTitle>
          <DialogDescription>
            Services that move up the levels together. They build only in the lowest level you pick, and each
            level above receives the same image by promotion.
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            create.mutate()
          }}
        >
          <label className="flex flex-col gap-1.5 text-xs text-muted-foreground">
            Name
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="frontend" className="h-9" autoFocus />
          </label>

          <fieldset className="space-y-1.5">
            <legend className="text-xs text-muted-foreground">Services</legend>
            {candidates.size === 0 && <p className="text-xs text-muted-foreground">Every service is in a group already.</p>}
            {[...candidates.values()].map((c) => (
              <label key={c.lineage_id} className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="accent-primary"
                  checked={picked.has(c.service_id)}
                  onChange={() => toggle(picked, c.service_id, setPicked)}
                />
                {c.service_name}
              </label>
            ))}
          </fieldset>

          <fieldset className="space-y-1.5">
            <legend className="text-xs text-muted-foreground">Levels it passes through</legend>
            {[...lower].reverse().map((l) => (
              <label key={l.project_id} className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="accent-primary" checked={levels.has(l.project_id)} onChange={() => toggle(levels, l.project_id, setLevels)} />
                {l.name}
              </label>
            ))}
            <label className="flex items-center gap-2 text-sm text-muted-foreground">
              <input type="checkbox" className="accent-primary" checked disabled />
              production (always)
            </label>
            {entry && !entry.production && (
              <p className="text-[11px] text-muted-foreground">
                Builds in <strong className="font-semibold text-foreground">{entry.name}</strong>, then{" "}
                {fullPath.slice(1).map((id) => board.levels.find((l) => l.project_id === id)?.name).join(" → ")}. Levels
                not ticked use these services from the level above.
              </p>
            )}
            {entry && !entry.production && (() => {
              const dbNames = [...new Set(board.ungrouped.filter((c) => c.type === "database").map((c) => c.lineage_id))]
                .filter((lineage) => !board.ungrouped.some((c) => c.level_id === entry.project_id && c.lineage_id === lineage))
                .map((lineage) => board.ungrouped.find((c) => c.lineage_id === lineage)!.service_name)
              return dbNames.length > 0 ? (
                <p className="text-[11px] text-amber-300/90">
                  {entry.name} has no database of its own for {dbNames.join(", ")}, so these services will use the level
                  above&apos;s data. Give {entry.name} its own copy from the board&apos;s Databases row.
                </p>
              ) : null
            })()}
          </fieldset>

          {create.error && (
            <p className="text-xs text-destructive">
              {create.error instanceof ApiError ? create.error.message : "Could not create the group."}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || picked.size === 0 || path.length === 0 || create.isPending}>
              {create.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Create group
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function byLevel(board: Board, levelId: string) {
  return board.levels.find((l) => l.project_id === levelId)?.level
}

/**
 * Giving a level its own copy of a database it uses from above: empty, or
 * cloned from the source's latest backup. A clone copies real users' data into
 * a lower level, which is said in plain words before it happens.
 */
function OwnDatabaseDialog({
  level,
  from,
  source,
  orgId,
  token,
  onClose,
}: {
  level: EnvironmentLevel
  from: EnvironmentLevel
  source: BoardCell
  orgId: string
  token: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [mode, setMode] = useState<"empty" | "clone">("empty")
  const own = useMutation({
    mutationFn: () => projectsApi.ownDatabase(orgId, level.project_id, { source_service_id: source.service_id, mode }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      onClose()
    },
  })
  const option = (value: "empty" | "clone", title: string, detail: string, disabled?: boolean) => (
    <label
      className={cn(
        "flex items-start gap-3 rounded-lg border px-3 py-2.5",
        mode === value ? "border-primary bg-primary/5" : "border-border/60",
        disabled && "opacity-50"
      )}
    >
      <input type="radio" name="own-db" className="accent-primary mt-1" checked={mode === value} disabled={disabled} onChange={() => setMode(value)} />
      <span className="space-y-0.5">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block text-xs text-muted-foreground">{detail}</span>
      </span>
    </label>
  )
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            Give {level.name} its own {source.service_name}
          </DialogTitle>
          <DialogDescription>
            {level.name} uses {from.name}&apos;s {source.service_name} today. Its own copy has the same engine and version,
            its own password and storage, and its services switch to it on their next deploy.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          {option("empty", "Empty", "A fresh database: the application creates what it needs.")}
          {option(
            "clone",
            `Cloned from ${from.name}`,
            source.has_backup
              ? `${from.name}'s latest backup is restored into it once it is up.`
              : `${from.name}'s ${source.service_name} has no backup yet. Take one from its Backups tab first.`,
            !source.has_backup
          )}
        </div>
        {mode === "clone" && (
          <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90">
            A clone copies real users&apos; data into {level.name}. Anyone who can reach {level.name} can read it.
          </p>
        )}
        {own.error && (
          <p className="text-xs text-destructive">{own.error instanceof ApiError ? own.error.message : "Could not create it."}</p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={own.isPending}>
            Cancel
          </Button>
          <Button onClick={() => own.mutate()} disabled={own.isPending}>
            {own.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {mode === "clone" ? "Create and clone" : "Create empty"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Editing a group: its name, and which services move with it. Removing a
 * service leaves its copies running in every level; it only stops moving with
 * the group. Adding one puts it in the level the group enters at.
 */
function EditGroupDialog({
  group,
  board,
  orgId,
  projectId,
  token,
  onClose,
}: {
  group: BoardGroup
  board: Board
  orgId: string
  projectId: string
  token: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState(group.single ? "" : group.name)
  const [adding, setAdding] = useState<Set<string>>(new Set())
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["board", orgId] })
    qc.invalidateQueries({ queryKey: ["environments", orgId] })
  }

  // One entry per service in the group, and per service that could join it:
  // those in no group, and those in a single-service group.
  const members = new Map<string, BoardCell>()
  for (const c of group.cells) if (!members.has(c.lineage_id)) members.set(c.lineage_id, c)
  const joinable = new Map<string, BoardCell>()
  for (const c of [...board.ungrouped, ...board.groups.filter((g) => g.single && g.id !== group.id).flatMap((g) => g.cells)]) {
    if (c.type === "database" || members.has(c.lineage_id) || joinable.has(c.lineage_id)) continue
    joinable.set(c.lineage_id, c)
  }

  const removeOne = useMutation({
    mutationFn: (lineage: string) => projectsApi.removeFromGroup(orgId, projectId, group.id, lineage, token),
    onSuccess: (res) => {
      refresh()
      if (res.group_deleted) onClose()
    },
  })
  const save = useMutation({
    mutationFn: async () => {
      if (name.trim() && name.trim() !== group.name) await projectsApi.renameGroup(orgId, projectId, group.id, name.trim(), token)
      if (adding.size > 0) await projectsApi.addToGroup(orgId, projectId, group.id, [...adding], token)
    },
    onSuccess: () => {
      refresh()
      onClose()
    },
  })
  const error = save.error ?? removeOne.error

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Edit {group.name}</DialogTitle>
          <DialogDescription>
            {group.single
              ? "A copy of one service. Give it a name to make it a group other services can join."
              : "Which services move up the levels together."}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <label className="flex flex-col gap-1.5 text-xs text-muted-foreground">
            Name
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={group.name} className="h-9" />
          </label>

          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">In this group</p>
            {[...members.values()].map((c) => (
              <div key={c.lineage_id} className="flex items-center justify-between gap-2 text-sm">
                {c.service_name}
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-7 text-xs text-muted-foreground"
                  disabled={removeOne.isPending}
                  onClick={() => removeOne.mutate(c.lineage_id)}
                >
                  Remove
                </Button>
              </div>
            ))}
            <p className="text-[11px] text-muted-foreground">
              A removed service keeps running in every level; it only stops moving with this group.
            </p>
          </div>

          {!group.single && joinable.size > 0 && (
            <fieldset className="space-y-1.5">
              <legend className="text-xs text-muted-foreground">Add</legend>
              {[...joinable.values()].map((c) => (
                <label key={c.lineage_id} className="flex items-center gap-2 text-sm">
                  <input type="checkbox" className="accent-primary"
                    checked={adding.has(c.service_id)}
                    onChange={() => {
                      const next = new Set(adding)
                      if (next.has(c.service_id)) next.delete(c.service_id)
                      else next.add(c.service_id)
                      setAdding(next)
                    }}
                  />
                  {c.service_name}
                </label>
              ))}
            </fieldset>
          )}

          {error && <p className="text-xs text-destructive">{error instanceof ApiError ? error.message : "Could not save."}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={save.isPending}>
              {save.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Removing a level's copy of a service. What goes, what stays, and what the
 * level uses afterwards are all said before anything is deleted.
 */
/** The nearest level above cell's that has its own copy of the service. */
function copyAbove(board: Board, cell: BoardCell) {
  const level = board.levels.find((l) => l.project_id === cell.level_id)!
  const cells = [...board.ungrouped, ...board.groups.flatMap((g) => g.cells)]
  return board.levels
    .filter((l) => l.level < level.level)
    .sort((a, b) => b.level - a.level)
    .find((l) => cells.some((c) => c.level_id === l.project_id && c.lineage_id === cell.lineage_id))
}

function RemoveFromLevelDialog({
  cell,
  group,
  board,
  orgId,
  token,
  onClose,
}: {
  cell: BoardCell
  group: BoardGroup | null
  board: Board
  orgId: string
  token: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const level = board.levels.find((l) => l.project_id === cell.level_id)!
  const above = copyAbove(board, cell)
  const buildsHere = group && group.path[0] === level.project_id
  const remove = useMutation({
    mutationFn: () => projectsApi.removeFromLevel(orgId, level.project_id, cell.service_id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {above ? `Remove ${cell.service_name} from ${level.name}?` : `Delete ${cell.service_name}?`}
          </DialogTitle>
          <DialogDescription>
            {above
              ? `${level.name}'s ${cell.service_name} is deleted, with ${level.name}'s routes to it. ${above.name}'s ${cell.service_name} is untouched, and ${level.name} uses it from then on.`
              : `${cell.service_name} started in ${level.name} and no level above has it, so this deletes the service, with its routes and deployments. Levels below that use it from ${level.name} lose it too. This cannot be undone.`}
          </DialogDescription>
        </DialogHeader>
        {cell.type === "database" && above && (
          <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90">
            {level.name}&apos;s own data in it is lost, and its services write to {above.name}&apos;s data instead.
          </p>
        )}
        {buildsHere && (
          <p className="text-xs text-muted-foreground">
            {level.name} is where {group!.single ? "it" : group!.name} builds, so {cell.service_name} also leaves{" "}
            {group!.single ? "its own group, which goes with it" : `the ${group!.name} group`}.
          </p>
        )}
        {remove.error && (
          <p className="text-xs text-destructive">{remove.error instanceof ApiError ? remove.error.message : "Could not remove it."}</p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={remove.isPending}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={() => remove.mutate()} disabled={remove.isPending}>
            {remove.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {above ? `Remove from ${level.name}` : `Delete ${cell.service_name}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type Skip = "unchanged" | "older" | "never_built" | "not_here" | "from_below"

/**
 * What promoting a group from one level to the next would do, by the rule the
 * API applies: a service moves when its image here was built after what runs
 * above (or nothing runs above yet), and otherwise stays, for a reason.
 */
/** When a cell's image was built: what Promote compares, as the API does. */
const builtAt = (c: BoardCell) => new Date(c.image_built_at ?? c.deployed_at ?? 0)

/** A level above a group's entry running something made there, not promoted to it: a hotfix. */
export function builtHere(cell: BoardCell | undefined, group: BoardGroup | null) {
  if (!cell || !group || group.path[0] === cell.level_id || !group.path.includes(cell.level_id)) return false
  return cell.arrival === "build" || cell.arrival === "image"
}

function planPromotion(group: BoardGroup, from: EnvironmentLevel, to: EnvironmentLevel, overwrite = false) {
  const lineages = [...new Set(group.cells.map((c) => c.lineage_id))]
  const moves: BoardCell[] = []
  const stays: { name: string; reason: Skip }[] = []
  for (const lineage of lineages) {
    const here = group.cells.find((c) => c.level_id === from.project_id && c.lineage_id === lineage)
    const up = group.cells.find((c) => c.level_id === to.project_id && c.lineage_id === lineage)
    const name = (here ?? up ?? group.cells.find((c) => c.lineage_id === lineage))!.service_name
    if (!here) stays.push({ name, reason: "not_here" })
    else if (!here.image) stays.push({ name, reason: "never_built" })
    else if (up?.image && up.image === here.image) stays.push({ name, reason: "unchanged" })
    else if (!overwrite && up?.image && builtAt(here) <= builtAt(up))
      stays.push({ name, reason: "older" })
    else moves.push(here)
  }
  return { moves, stays }
}

function skipText(reason: Skip, from: string, to: string, detail?: string) {
  switch (reason) {
    case "from_below":
      return detail ?? `would use variables from below ${to}`
    case "unchanged":
      return `unchanged: ${to} runs this image`
    case "older":
      return `built before what ${to} runs`
    case "never_built":
      return `never built in ${from}`
    default:
      return `not in ${from}`
  }
}
