import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Lock, Loader2 } from "lucide-react"
import {
  nodes as nodesApi,
  type MemberReach,
  permissions as permissionsApi,
  type PermissionsWithUserDTO,
  viaText,
  RESOURCE_ACTIONS,
  type ResourceAction,
  type ResourceType,
} from "@/lib/api"
import { cn } from "@/lib/utils"
import { useIsAdmin } from "@/store/org-store"
import { MeshReachLine } from "./mesh-reach-line"

interface Props {
  orgId: string
  projectId: string
  resourceType: "service" | "stack" | "job"
  resourceId: string
  token: string
}

interface CombinedUserGrant {
  userId: string
  userName: string
  userEmail: string
  projectActions: Set<ResourceAction>
  resourceActions: Set<ResourceAction>
  /** Actions that come from somewhere other than this tab, by where: locked here. */
  lockedVia: Map<ResourceAction, string>
}

function buildCombinedGrants(
  projectRows: PermissionsWithUserDTO[],
  resourceRows: PermissionsWithUserDTO[],
): Map<string, CombinedUserGrant> {
  const map = new Map<string, CombinedUserGrant>()

  const entry = (row: PermissionsWithUserDTO) => {
    let e = map.get(row.user_id)
    if (!e) {
      e = { userId: row.user_id, userName: row.user_name, userEmail: row.user_email,
        projectActions: new Set(), resourceActions: new Set(), lockedVia: new Map() }
      map.set(row.user_id, e)
    }
    return e
  }
  for (const row of projectRows) {
    const e = entry(row)
    e.projectActions.add(row.action)
    if (!e.lockedVia.has(row.action)) e.lockedVia.set(row.action, row.via ? `${viaText(row.via)}, on the project` : "the project")
  }
  for (const row of resourceRows) {
    const e = entry(row)
    if (row.via) {
      if (!e.lockedVia.has(row.action)) e.lockedVia.set(row.action, viaText(row.via))
    } else {
      e.resourceActions.add(row.action)
    }
  }
  return map
}

export function ResourcePermissionsSection({ orgId, projectId, resourceType, resourceId, token }: Props) {
  const qc = useQueryClient()
  const resourceQueryKey = ["resource-permissions", orgId, resourceType, resourceId]
  const projectQueryKey = ["resource-permissions", orgId, "project", projectId]

  const { data: resourceRows = [], isLoading: resourceLoading } = useQuery({
    queryKey: resourceQueryKey,
    queryFn: () => permissionsApi.listForResource(orgId, resourceType, resourceId, token),
    enabled: !!orgId && !!resourceId,
  })

  const { data: projectRows = [], isLoading: projectLoading } = useQuery({
    queryKey: projectQueryKey,
    queryFn: () => permissionsApi.listForResource(orgId, "project", projectId, token),
    enabled: !!orgId && !!projectId,
  })

  const [pending, setPending] = useState<Set<string>>(new Set())

  // Whether each member's own machines reach this on the mesh. Jobs have no
  // ports to reach; only admins change it.
  const isAdmin = useIsAdmin()
  const meshKind = resourceType === "job" ? null : resourceType
  const reachKey = ["mesh-reach", orgId, resourceType, resourceId]
  const { data: reachRows = [] } = useQuery({
    queryKey: reachKey,
    queryFn: () => nodesApi.meshReach(orgId, meshKind!, resourceId, token),
    enabled: !!orgId && !!resourceId && !!meshKind && isAdmin,
  })
  const reachByUser = new Map<string, MemberReach>(reachRows.map((r) => [r.user_id, r]))
  const setReach = useMutation({
    mutationFn: ({ userId, reach }: { userId: string; reach: boolean }) =>
      nodesApi.setMeshReach(orgId, { user_id: userId, resource_type: meshKind!, resource_id: resourceId, reach }, token),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: reachKey })
      qc.invalidateQueries({ queryKey: ["mesh-access", orgId] })
    },
  })

  const { mutate } = useMutation({
    mutationFn: ({ userId, action, granted }: { userId: string; action: ResourceAction; granted: boolean }) => {
      const body = { resource_type: resourceType as ResourceType, resource_id: resourceId, action }
      return granted
        ? permissionsApi.revoke(orgId, userId, body, token)
        : permissionsApi.grant(orgId, userId, body, token)
    },
    onMutate: ({ userId, action }) => {
      setPending((s) => new Set(s).add(`${userId}-${action}`))
    },
    onSettled: (_, __, { userId, action }) => {
      setPending((s) => { const n = new Set(s); n.delete(`${userId}-${action}`); return n })
      qc.invalidateQueries({ queryKey: resourceQueryKey })
      qc.invalidateQueries({ queryKey: reachKey })
    },
  })

  if (resourceLoading || projectLoading) {
    return (
      <div className="flex items-center gap-2 text-muted-foreground text-sm">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>Loading…</span>
      </div>
    )
  }

  const combinedMap = buildCombinedGrants(projectRows, resourceRows)
  const userGrants = Array.from(combinedMap.values())

  return (
    <div className="space-y-4">
      {/* The page above it gives the heading. */}
      <p className="text-xs text-muted-foreground">
        <Lock className="inline h-3 w-3 mr-0.5 mb-0.5" />
        Locked permissions are inherited from the project. Use the other controls to change access for this resource.
      </p>

      {userGrants.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/50 py-8 flex flex-col items-center gap-2 text-muted-foreground">
          <p className="text-xs">No members have access to this {resourceType} yet</p>
        </div>
      ) : (
        <div className="console-record-list rounded-xl border border-border overflow-hidden divide-y divide-border/40">
          {userGrants.map((user) => (
            <UserGrantRow
              key={user.userId}
              user={user}
              pending={pending}
              onToggle={(action) =>
                mutate({ userId: user.userId, action, granted: user.resourceActions.has(action) })
              }
              reach={reachByUser.get(user.userId)}
              reachPending={setReach.isPending && setReach.variables?.userId === user.userId}
              onReach={(reach) => setReach.mutate({ userId: user.userId, reach })}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function UserGrantRow({ user, pending, onToggle, reach, reachPending, onReach }: {
  user: CombinedUserGrant
  pending: Set<string>
  onToggle: (action: ResourceAction) => void
  reach?: MemberReach
  reachPending: boolean
  onReach: (reach: boolean) => void
}) {
  const initials = user.userName.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()

  return (
    <div className="space-y-3 px-4 py-4">
    <div className="flex flex-wrap items-center gap-3">
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <span className="text-xs font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{user.userName}</p>
        <p className="text-xs text-muted-foreground">{user.userEmail}</p>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {RESOURCE_ACTIONS.map((action) => {
          const lockedVia = user.lockedVia.get(action)
          const fromResource = user.resourceActions.has(action)
          const isLoading = pending.has(`${user.userId}-${action}`)

          if (lockedVia && !fromResource) {
            // Locked: granted on the project, or from another source, and
            // changed there.
            return (
              <span
                key={action}
                title={`Granted via ${lockedVia}`}
                className="h-6 px-2.5 text-[11px] font-medium rounded-md border border-border/30 bg-muted/30 text-muted-foreground/50 flex items-center gap-1 cursor-default select-none"
              >
                <Lock className="h-2.5 w-2.5" />
                {action}
              </span>
            )
          }

          // Toggleable - resource-level only
          return (
            <button
              key={action}
              onClick={() => onToggle(action)}
              disabled={isLoading}
              className={cn(
                "h-6 px-2.5 text-[11px] font-medium rounded-md border transition-colors select-none",
                fromResource
                  ? "bg-primary/15 text-primary border-primary/30 hover:bg-primary/25"
                  : "bg-transparent text-muted-foreground/35 border-border/30 hover:text-muted-foreground/70 hover:border-border/60",
                isLoading && "opacity-40 cursor-wait pointer-events-none"
              )}
            >
              {action}
            </button>
          )
        })}
      </div>
    </div>
    {reach && <MeshReachLine reach={reach} pending={reachPending} onChange={onReach} />}
    </div>
  )
}
