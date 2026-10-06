import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronDown, ChevronRight, Loader2, Lock } from "lucide-react"
import { useState } from "react"
import {
  projects as projectsApi,
  permissions as permissionsApi,
  type ApiPermission,
  viaText,
  type ApiProject,
  RESOURCE_ACTIONS,
  type ResourceAction,
} from "@/lib/api"
import { cn } from "@/lib/utils"

const ACTION_LABELS: Record<ResourceAction, string> = {
  view: "view",
  create: "create",
  deploy: "deploy",
  update: "update",
  delete: "delete",
}

/**
 * PrincipalPermissions renders the project-level permission grid for any
 * principal - a human member or an agent. Both are just a user id from the
 * permission system's point of view, so this component is shared between the
 * Users detail page and the Agents detail page.
 */
export function PrincipalPermissions({ orgId, principalId, token }: {
  orgId: string
  principalId: string
  token: string
}) {
  const qc = useQueryClient()
  const [pending, setPending] = useState<Set<string>>(new Set())

  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const { data: perms = [], isLoading: permsLoading } = useQuery({
    queryKey: ["member-permissions", orgId, principalId],
    queryFn: () => permissionsApi.listForMember(orgId, principalId, token),
    enabled: !!orgId && !!principalId,
  })

  const { mutate } = useMutation({
    mutationFn: ({ projectId, action, granted }: { projectId: string; action: ResourceAction; granted: boolean }) => {
      const body = { resource_type: "project" as const, resource_id: projectId, action }
      return granted
        ? permissionsApi.revoke(orgId, principalId, body, token)
        : permissionsApi.grant(orgId, principalId, body, token)
    },
    onMutate: ({ projectId, action }) => {
      setPending((s) => new Set(s).add(`${projectId}-${action}`))
    },
    onSettled: (_, __, { projectId, action }) => {
      setPending((s) => { const n = new Set(s); n.delete(`${projectId}-${action}`); return n })
      qc.invalidateQueries({ queryKey: ["member-permissions", orgId, principalId] })
    },
  })

  if (projectsLoading || permsLoading) {
    return (
      <div className="flex items-center gap-2 text-muted-foreground text-sm">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>Loading…</span>
      </div>
    )
  }

  const projectGrantMap = buildProjectGrantMap(perms.filter((p) => !p.via))
  const projectViaMap = buildProjectViaMap(perms)
  const resourcePerms = perms.filter((p) => p.resource_type !== "project")
  const overridesByProject = buildOverrideMap(resourcePerms)

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-medium">Project Permissions</h2>
        <p className="text-xs text-muted-foreground">Click an action to grant or revoke access</p>
      </div>

      {projects.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/50 py-8 flex flex-col items-center gap-2 text-muted-foreground">
          <p className="text-xs">No projects yet</p>
        </div>
      ) : (
        <div className="console-record-list rounded-xl border border-border overflow-hidden divide-y divide-border/40">
          {projects.map((project) => {
            const granted = projectGrantMap.get(project.id) ?? new Set<ResourceAction>()
            const viaFor = projectViaMap.get(project.id) ?? new Map<ResourceAction, string>()
            const overrides = overridesByProject.get(project.id) ?? []
            return (
              <ProjectRow
                key={project.id}
                project={project}
                granted={granted}
                viaFor={viaFor}
                overrides={overrides}
                pending={pending}
                onToggle={(action) => mutate({ projectId: project.id, action, granted: granted.has(action) })}
              />
            )
          })}
        </div>
      )}
    </div>
  )
}

function ProjectRow({
  project, granted, viaFor, overrides, pending, onToggle,
}: {
  project: ApiProject
  granted: Set<ResourceAction>
  /** Actions granted from another source, by where they come from. */
  viaFor: Map<ResourceAction, string>
  overrides: ApiPermission[]
  pending: Set<string>
  onToggle: (action: ResourceAction) => void
}) {
  const [expanded, setExpanded] = useState(false)

  const byResource = groupByResource(overrides)
  const hasOverrides = byResource.length > 0
  // An action granted on some of what is inside the project, and not on the
  // project: the names it is granted on, for the partial pill.
  const partly = (action: ResourceAction) =>
    granted.has(action) ? [] : byResource.filter((r) => r.actions.includes(action)).map((r) => r.resourceName)

  return (
    <div>
      <div className="flex items-center gap-3 px-4 py-3">
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium">{project.name}</p>
          <p className="text-xs text-muted-foreground/50 font-mono">{project.slug}</p>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {RESOURCE_ACTIONS.map((action) => viaFor.has(action) && !granted.has(action) ? (
            <span key={action} title={`Granted via ${viaFor.get(action)}`}
              className="h-6 px-2.5 text-[11px] font-medium rounded-md border border-border/30 bg-muted/30 text-muted-foreground/60 flex items-center gap-1 cursor-default select-none">
              <Lock className="h-2.5 w-2.5" />{ACTION_LABELS[action]}
            </span>
          ) : (
            <ActionPill
              key={action}
              action={action}
              granted={granted.has(action)}
              partly={partly(action)}
              loading={pending.has(`${project.id}-${action}`)}
              onToggle={() => onToggle(action)}
            />
          ))}
        </div>

        {hasOverrides ? (
          <button
            onClick={() => setExpanded((v) => !v)}
            className="flex items-center gap-1 text-[10px] text-muted-foreground/50 hover:text-muted-foreground transition-colors shrink-0 ml-2 w-20 justify-end"
          >
            {byResource.length} override{byResource.length !== 1 ? "s" : ""}
            {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          </button>
        ) : (
          <span className="w-20 shrink-0" />
        )}
      </div>

      {expanded && hasOverrides && (
        <div className="border-t border-border/40 bg-muted/10 divide-y divide-border/30">
          {byResource.map(({ resourceId, resourceName, resourceType, actions, via }) => (
            <ResourceOverrideRow
              key={resourceId}
              resourceName={resourceName}
              resourceType={resourceType}
              actions={actions}
              via={via}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function ResourceOverrideRow({ resourceName, resourceType, actions, via }: {
  resourceName: string
  resourceType: string
  actions: ResourceAction[]
  /** Where these grants come from, when not given directly. */
  via?: string
}) {
  return (
    <div className="flex items-center gap-3 px-6 py-2.5">
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <span className="text-[10px] font-medium px-1.5 py-0.5 rounded border border-border/40 text-muted-foreground/60 shrink-0">
          {resourceType}
        </span>
        <p className="text-sm text-muted-foreground truncate">{resourceName}</p>
        {via && <span className="shrink-0 text-[11px] text-muted-foreground/70">via {via}</span>}
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        {actions.map((action) => (
          <span
            key={action}
            className="h-5 px-2 text-[10px] font-medium rounded border bg-primary/10 text-primary border-primary/25"
          >
            {action}
          </span>
        ))}
      </div>
      <span className="w-20 shrink-0" />
    </div>
  )
}

/**
 * One action on a project, in three states: granted on the project (filled),
 * granted only on some of what is inside it (outlined, `partly` names those),
 * or neither (muted). Clicking grants or revokes it on the project alone, so a
 * partial pill fills, and a filled one falls back to partial while the
 * overrides stay; they are changed where they were given.
 */
function ActionPill({ action, granted, partly, loading, onToggle }: {
  action: ResourceAction
  granted: boolean
  partly: string[]
  loading: boolean
  onToggle: () => void
}) {
  const partial = !granted && partly.length > 0
  return (
    <button
      onClick={onToggle}
      disabled={loading}
      title={partial ? `${ACTION_LABELS[action]} on ${partly.join(", ")} only. Click to grant it on the whole project.` : undefined}
      className={cn(
        "h-6 px-2.5 text-[11px] font-medium rounded-md border transition-colors select-none",
        granted
          ? "bg-primary/15 text-primary border-primary/30 hover:bg-primary/25"
          : partial
            ? "bg-transparent text-primary/80 border-dashed border-primary/45 hover:bg-primary/10"
            : "bg-transparent text-muted-foreground/35 border-border/30 hover:text-muted-foreground/70 hover:border-border/60",
        loading && "opacity-40 cursor-wait pointer-events-none"
      )}
    >
      {ACTION_LABELS[action]}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function buildOverrideMap(perms: ApiPermission[]): Map<string, ApiPermission[]> {
  const map = new Map<string, ApiPermission[]>()
  for (const p of perms) {
    if (p.resource_type === "project" || !p.parent_project_id) continue
    const list = map.get(p.parent_project_id) ?? []
    list.push(p)
    map.set(p.parent_project_id, list)
  }
  return map
}

function groupByResource(perms: ApiPermission[]): {
  resourceId: string
  resourceName: string
  resourceType: string
  actions: ResourceAction[]
  via?: string
}[] {
  // One line per resource and source: a direct grant and one from another
  // source are changed in different places.
  const map = new Map<string, { resourceId: string; resourceName: string; resourceType: string; actions: ResourceAction[]; via?: string }>()
  for (const p of perms) {
    const via = p.via ? viaText(p.via) : undefined
    const key = `${p.resource_id}|${via ?? ""}`
    const existing = map.get(key)
    if (existing) {
      if (!existing.actions.includes(p.action)) existing.actions.push(p.action)
    } else {
      map.set(key, {
        resourceId: p.resource_id,
        resourceName: p.resource_name ?? p.resource_id.slice(0, 8),
        resourceType: p.resource_type,
        actions: [p.action],
        via,
      })
    }
  }
  return Array.from(map.values())
}

/** Project actions granted from another source, by where each comes from. */
function buildProjectViaMap(perms: ApiPermission[]): Map<string, Map<ResourceAction, string>> {
  const map = new Map<string, Map<ResourceAction, string>>()
  for (const p of perms) {
    if (p.resource_type !== "project" || !p.via) continue
    if (!map.has(p.resource_id)) map.set(p.resource_id, new Map())
    map.get(p.resource_id)!.set(p.action, viaText(p.via))
  }
  return map
}

function buildProjectGrantMap(perms: ApiPermission[]): Map<string, Set<ResourceAction>> {
  const map = new Map<string, Set<ResourceAction>>()
  for (const p of perms) {
    if (p.resource_type !== "project") continue
    if (!map.has(p.resource_id)) map.set(p.resource_id, new Set())
    map.get(p.resource_id)!.add(p.action)
  }
  return map
}
