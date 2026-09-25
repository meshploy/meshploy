import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Check, ChevronsUpDown, Loader2, Pencil, Plus, Trash2 } from "lucide-react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { OptionSelect } from "@/components/layout/option-select"
import { projects as projectsApi, ApiError } from "@/lib/api"
import type { EnvironmentLevel } from "@/lib/api/projects"
import { cn } from "@/lib/utils"

/**
 * The project's environment levels, and moving between them.
 *
 * A level is a project of its own underneath (its own namespace, services and
 * routes), so switching is navigating to it: every tab works there unchanged.
 * The switch keeps the tab you are on, since the question is usually "what does
 * staging have here", not "take me to staging's front page".
 */
export function EnvironmentSwitcher({
  orgId,
  projectId,
  section,
  token,
  className,
}: {
  orgId: string
  projectId: string
  /** The project tab being viewed, kept when switching. */
  section: string
  token: string
  className?: string
}) {
  const navigate = useNavigate()
  const [adding, setAdding] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const { data: levels = [] } = useQuery({
    queryKey: ["environments", orgId, projectId],
    queryFn: () => projectsApi.environments(orgId, projectId, token),
  })
  const current = levels.find((l) => l.project_id === projectId)
  const go = (l: EnvironmentLevel) =>
    navigate({ to: `/projects/${l.project_id}${section ? `/${section}` : "/"}` })

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          aria-label={`Environment: ${current?.name ?? "production"}`}
          className={cn(
            "mt-2 flex w-full items-center gap-2 rounded-md border border-border/60 px-2 py-1.5 text-xs outline-none transition-colors hover:bg-muted/30 focus-visible:ring-2 focus-visible:ring-ring",
            className
          )}
        >
          <LevelDot production={current?.production ?? true} />
          <span className="flex-1 min-w-0 truncate text-left font-medium">{current?.name ?? "production"}</span>
          <ChevronsUpDown className="h-3 w-3 shrink-0 opacity-50" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" sideOffset={6} className="w-[240px]">
          <DropdownMenuLabel className="text-xs text-muted-foreground">Environments</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {levels.map((l) => (
            <DropdownMenuItem key={l.project_id} onClick={() => go(l)} className="gap-2 text-sm">
              <LevelDot production={l.production} />
              <span className="flex-1 min-w-0 truncate">{l.name}</span>
              <span className="min-w-[2.5rem] text-right text-[11px] tabular-nums text-muted-foreground">
                {l.services_count + l.databases_count || "empty"}
              </span>
              {/* Every row keeps the check's room, so the counts line up. */}
              <Check className={cn("h-3.5 w-3.5 shrink-0 text-primary", l.project_id !== projectId && "invisible")} />
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setAdding(true)} className="gap-2 text-sm text-primary">
            <Plus className="h-3.5 w-3.5" />
            New level
          </DropdownMenuItem>
          {current && !current.production && (
            <DropdownMenuItem onClick={() => setRenaming(true)} className="gap-2 text-sm">
              <Pencil className="h-3.5 w-3.5" />
              Rename {current.name}
            </DropdownMenuItem>
          )}
          {current && !current.production && (
            <DropdownMenuItem onClick={() => setDeleting(true)} className="gap-2 text-sm text-destructive">
              <Trash2 className="h-3.5 w-3.5" />
              Delete {current.name}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {deleting && current && (
        <DeleteLevelDialog
          orgId={orgId}
          level={current}
          token={token}
          onClose={() => setDeleting(false)}
          onDeleted={() => {
            setDeleting(false)
            const production = levels.find((l) => l.production)
            if (production) go(production)
          }}
        />
      )}
      {renaming && current && (
        <RenameLevelDialog orgId={orgId} level={current} token={token} onClose={() => setRenaming(false)} />
      )}
      {adding && (
        <NewLevelDialog
          orgId={orgId}
          projectId={projectId}
          levels={levels}
          token={token}
          onClose={() => setAdding(false)}
          onCreated={(l) => {
            setAdding(false)
            go(l)
          }}
        />
      )}
    </>
  )
}

export function LevelDot({ production }: { production: boolean }) {
  return (
    <span
      aria-hidden
      className={cn("size-2 shrink-0 rounded-full", production ? "bg-emerald-400" : "bg-amber-400")}
    />
  )
}

/**
 * Adding a level, placed in the chain.
 *
 * The chain is drawn with the new level where it will go, because "above
 * staging" and "below qa" are easy to confuse and the picture is not.
 */
function NewLevelDialog({
  orgId,
  projectId,
  levels,
  token,
  onClose,
  onCreated,
}: {
  orgId: string
  projectId: string
  levels: EnvironmentLevel[]
  token: string
  onClose: () => void
  onCreated: (l: EnvironmentLevel) => void
}) {
  const qc = useQueryClient()
  const lowest = levels[levels.length - 1]
  const [name, setName] = useState("")
  const [anchorId, setAnchorId] = useState(lowest?.project_id ?? projectId)
  const anchor = levels.find((l) => l.project_id === anchorId)
  // Production has nothing above it, so only "below" is offered there.
  const [placement, setPlacement] = useState<"above" | "below">("below")
  const effective = anchor?.production ? "below" : placement

  const create = useMutation({
    mutationFn: () =>
      projectsApi.createEnvironment(orgId, projectId, { name, relative_to: anchorId, placement: effective }, token),
    onSuccess: (l) => {
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      onCreated(l)
    },
  })

  // The chain as it will be, lowest on the left, production on the right:
  // promotion reads left to right.
  const proposed = [...levels].reverse().map((l) => ({ key: l.project_id, name: l.name, production: l.production, isNew: false }))
  const at = proposed.findIndex((l) => l.key === anchorId)
  const newChip = { key: "new", name: name || "new level", production: false, isNew: true }
  if (at >= 0) proposed.splice(effective === "below" ? at : at + 1, 0, newChip)

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New level</DialogTitle>
          <DialogDescription>
            A new namespace in this project&apos;s chain. It starts empty: services arrive by being promoted
            into it, and anything it does not have is used from the level above.
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
            <Input
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase())}
              placeholder="staging"
              className="h-9 font-mono"
              autoFocus
            />
          </label>

          <div className="space-y-2">
            <p className="text-xs text-muted-foreground">Where it goes</p>
            <div className="flex flex-wrap items-center gap-2">
              <SegmentedControl
                className="h-8"
                value={effective}
                onValueChange={(v) => setPlacement(v as "above" | "below")}
                options={
                  anchor?.production
                    ? [{ value: "below", label: "Below" }]
                    : [
                        { value: "below", label: "Below" },
                        { value: "above", label: "Above" },
                      ]
                }
              />
              <OptionSelect
                label="Level to place it next to"
                value={anchorId}
                onChange={setAnchorId}
                options={levels.map((l) => ({ value: l.project_id, label: l.name }))}
                className="h-8 w-40 text-xs"
              />
            </div>
            <ol className="flex flex-wrap items-center gap-1.5 pt-1" aria-label="The chain, lowest first">
              {proposed.map((l, i) => (
                <li key={l.key} className="flex items-center gap-1.5">
                  {i > 0 && <span className="text-muted-foreground/50">→</span>}
                  <span
                    className={cn(
                      "rounded-md border px-2 py-1 font-mono text-[11px]",
                      l.isNew ? "border-dashed border-primary text-primary" : "border-border/60 text-muted-foreground"
                    )}
                  >
                    {l.name}
                  </span>
                </li>
              ))}
            </ol>
          </div>

          {create.error && (
            <p className="text-xs text-destructive">
              {create.error instanceof ApiError ? create.error.message : "Could not add the level."}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Add level
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Renaming a level. Its hostnames carry its name, so they change with it and
 * the old ones stop answering; its namespace cannot be renamed, and stays.
 * Both are said before anything happens.
 */
function RenameLevelDialog({
  orgId,
  level,
  token,
  onClose,
}: {
  orgId: string
  level: EnvironmentLevel
  token: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState(level.name)
  const rename = useMutation({
    mutationFn: () => projectsApi.renameEnvironment(orgId, level.project_id, name, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, level.project_id] })
      qc.invalidateQueries({ queryKey: ["routes", orgId, level.project_id] })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Rename {level.name}</DialogTitle>
          <DialogDescription>
            Its routes are renamed with it: app-{level.name} becomes app-{name || "…"}, and links to the old names stop
            working. Its namespace, {level.namespace}, cannot be renamed and stays as it is.
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            rename.mutate()
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} className="h-9 font-mono" autoFocus aria-label="New name" />
          {rename.error && (
            <p className="text-xs text-destructive">
              {rename.error instanceof ApiError ? rename.error.message : "Could not rename it."}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || name === level.name || rename.isPending}>
              {rename.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Rename
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Deleting a level. Everything in it goes, from the cluster too; groups that
 * passed through it skip it, and one that built there builds at the next
 * level on its path. Asked for by name, since it cannot be undone.
 */
function DeleteLevelDialog({
  orgId,
  level,
  token,
  onClose,
  onDeleted,
}: {
  orgId: string
  level: EnvironmentLevel
  token: string
  onClose: () => void
  onDeleted: () => void
}) {
  const qc = useQueryClient()
  const [typed, setTyped] = useState("")
  const remove = useMutation({
    mutationFn: () => projectsApi.deleteEnvironment(orgId, level.project_id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["environments", orgId] })
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      qc.invalidateQueries({ queryKey: ["projects", orgId] })
      onDeleted()
    },
  })
  const count = level.services_count + level.databases_count
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Delete {level.name}</DialogTitle>
          <DialogDescription>This cannot be undone.</DialogDescription>
        </DialogHeader>
        <ul className="list-disc space-y-1.5 pl-5 text-sm text-muted-foreground">
          <li>
            {count > 0
              ? `Its ${count} ${count === 1 ? "service" : "services"}, with their routes, volumes, jobs and data, are deleted, from the cluster too.`
              : "It has no services; its routes, volumes and jobs are deleted with it."}
          </li>
          <li>Its namespace, {level.namespace}, is deleted.</li>
          <li>Groups that pass through it skip it from now on. A group that builds here builds at the next level on its path instead.</li>
          <li>Levels below it use what the level above has, as they do for anything a level lacks.</li>
        </ul>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (typed === level.name) remove.mutate()
          }}
        >
          <label className="block text-xs text-muted-foreground">
            Type <span className="font-mono text-foreground">{level.name}</span> to confirm
            <Input value={typed} onChange={(e) => setTyped(e.target.value)} className="mt-1.5 h-9 font-mono" autoFocus aria-label="Level name" />
          </label>
          {remove.error && (
            <p className="text-xs text-destructive">
              {remove.error instanceof ApiError ? remove.error.message : "Could not delete it."}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="destructive" disabled={typed !== level.name || remove.isPending}>
              {remove.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Delete {level.name}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
