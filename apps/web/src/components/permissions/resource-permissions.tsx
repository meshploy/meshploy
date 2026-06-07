import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Lock, Loader2 } from "lucide-react"
import {
  permissions as permissionsApi,
  type PermissionsWithUserDTO,
  RESOURCE_ACTIONS,
  type ResourceAction,
  type ResourceType,
} from "@/lib/api"
import { cn } from "@/lib/utils"

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
}

function buildCombinedGrants(
  projectRows: PermissionsWithUserDTO[],
  resourceRows: PermissionsWithUserDTO[],
): Map<string, CombinedUserGrant> {
  const map = new Map<string, CombinedUserGrant>()

  for (const row of projectRows) {
    const existing = map.get(row.user_id)
    if (existing) {
      existing.projectActions.add(row.action)
    } else {
      map.set(row.user_id, {
        userId: row.user_id,
        userName: row.user_name,
        userEmail: row.user_email,
        projectActions: new Set([row.action]),
        resourceActions: new Set(),
      })
    }
  }

  for (const row of resourceRows) {
    const existing = map.get(row.user_id)
    if (existing) {
      existing.resourceActions.add(row.action)
    } else {
      map.set(row.user_id, {
        userId: row.user_id,
        userName: row.user_name,
        userEmail: row.user_email,
        projectActions: new Set(),
        resourceActions: new Set([row.action]),
      })
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
      <div>
        <h2 className="text-sm font-medium">Member Access</h2>
        <p className="text-xs text-muted-foreground mt-0.5">
          <Lock className="inline h-3 w-3 mr-0.5 mb-0.5" />
          locked = from project · toggleable = resource-level override
        </p>
      </div>

      {userGrants.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/50 py-8 flex flex-col items-center gap-2 text-muted-foreground">
          <p className="text-xs">No members have access to this {resourceType} yet</p>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {userGrants.map((user) => (
            <UserGrantRow
              key={user.userId}
              user={user}
              pending={pending}
              onToggle={(action) =>
                mutate({ userId: user.userId, action, granted: user.resourceActions.has(action) })
              }
            />
          ))}
        </div>
      )}
    </div>
  )
}

function UserGrantRow({ user, pending, onToggle }: {
  user: CombinedUserGrant
  pending: Set<string>
  onToggle: (action: ResourceAction) => void
}) {
  const initials = user.userName.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <span className="text-xs font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{user.userName}</p>
        <p className="text-xs text-muted-foreground">{user.userEmail}</p>
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        {RESOURCE_ACTIONS.map((action) => {
          const fromProject = user.projectActions.has(action)
          const fromResource = user.resourceActions.has(action)
          const isLoading = pending.has(`${user.userId}-${action}`)

          if (fromProject) {
            // Locked — comes from project grant
            return (
              <span
                key={action}
                title="Granted via project"
                className="h-6 px-2.5 text-[11px] font-medium rounded-md border border-border/30 bg-muted/30 text-muted-foreground/50 flex items-center gap-1 cursor-default select-none"
              >
                <Lock className="h-2.5 w-2.5" />
                {action}
              </span>
            )
          }

          // Toggleable — resource-level only
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
  )
}
