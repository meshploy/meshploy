import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Plus, UserX } from "lucide-react"
import {
  orgs as orgsApi,
  permissions as permissionsApi,
  type ApiOrgMember,
  type PermissionsWithUserDTO,
  RESOURCE_ACTIONS,
  type ResourceAction,
  type ResourceType,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"

interface Props {
  orgId: string
  resourceType: "service" | "stack" | "job"
  resourceId: string
  token: string
}

interface UserGrant {
  userId: string
  userName: string
  userEmail: string
  actions: Set<ResourceAction>
}

function groupByUser(rows: PermissionsWithUserDTO[]): UserGrant[] {
  const map = new Map<string, UserGrant>()
  for (const row of rows) {
    const existing = map.get(row.user_id)
    if (existing) {
      existing.actions.add(row.action)
    } else {
      map.set(row.user_id, {
        userId: row.user_id,
        userName: row.user_name,
        userEmail: row.user_email,
        actions: new Set([row.action]),
      })
    }
  }
  return Array.from(map.values())
}

export function ResourcePermissionsSection({ orgId, resourceType, resourceId, token }: Props) {
  const qc = useQueryClient()
  const queryKey = ["resource-permissions", orgId, resourceType, resourceId]

  const { data: rows = [], isLoading } = useQuery({
    queryKey,
    queryFn: () => permissionsApi.listForResource(orgId, resourceType, resourceId, token),
    enabled: !!orgId && !!resourceId,
  })

  const { data: members = [] } = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId, token),
    enabled: !!orgId,
  })

  const [pending, setPending] = useState<Set<string>>(new Set())
  const [addingUserId, setAddingUserId] = useState<string | null>(null)

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
      qc.invalidateQueries({ queryKey })
    },
  })

  const { mutate: addMember, isPending: adding } = useMutation({
    mutationFn: (userId: string) =>
      permissionsApi.grant(orgId, userId, { resource_type: resourceType as ResourceType, resource_id: resourceId, action: "view" }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey })
      setAddingUserId(null)
    },
  })

  const userGrants = groupByUser(rows)
  const grantedUserIds = new Set(userGrants.map((u) => u.userId))

  // Only member-role users not already in the list
  const addableMembers = members.filter(
    (m: ApiOrgMember) => m.role === "member" && !grantedUserIds.has(m.user_id)
  )

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-muted-foreground text-sm">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>Loading…</span>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-medium">Member Permissions</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Grant specific members access to this {resourceType}
          </p>
        </div>
        {addableMembers.length > 0 && (
          <Select
            value={addingUserId ?? ""}
            onValueChange={(v) => { if (v) { setAddingUserId(v); addMember(v) } }}
            disabled={adding}
          >
            <SelectTrigger className="w-auto h-7 text-xs gap-1.5 px-2.5 border-border/60 bg-muted/20">
              {adding
                ? <Loader2 className="h-3 w-3 animate-spin" />
                : <Plus className="h-3 w-3" />
              }
              <span>Add member</span>
            </SelectTrigger>
            <SelectContent>
              {addableMembers.map((m: ApiOrgMember) => (
                <SelectItem key={m.user_id} value={m.user_id}>
                  {m.user_name}
                  <span className="ml-1.5 text-muted-foreground/60 text-[10px]">{m.user_email}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>

      {userGrants.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/50 py-8 flex flex-col items-center gap-2 text-muted-foreground">
          <UserX className="h-5 w-5 text-muted-foreground/30" />
          <p className="text-xs">No member-level overrides — access is controlled by project permissions</p>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {userGrants.map((user) => (
            <UserGrantRow
              key={user.userId}
              user={user}
              pending={pending}
              onToggle={(action) => mutate({ userId: user.userId, action, granted: user.actions.has(action) })}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function UserGrantRow({ user, pending, onToggle }: {
  user: UserGrant
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
          const granted = user.actions.has(action)
          const isLoading = pending.has(`${user.userId}-${action}`)
          return (
            <button
              key={action}
              onClick={() => onToggle(action)}
              disabled={isLoading}
              className={cn(
                "h-6 px-2.5 text-[11px] font-medium rounded-md border transition-colors select-none",
                granted
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
