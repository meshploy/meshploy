import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { Loader2, Crown, Shield, User, ChevronRight } from "lucide-react"
import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { orgs as orgsApi, type ApiOrgMember } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useOrgRole } from "@/store/org-store"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type { OrgRole } from "@/types"

export const Route = createFileRoute("/_app/users/")({
  component: UsersPage,
})

function UsersPage() {
  const role = useOrgRole()
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const { data: members = [], isLoading } = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId, token),
    enabled: !!orgId,
  })

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Users</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Manage member access and resource permissions</p>
      </div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {members.map((member) => (
            <UserRow key={member.id} member={member} />
          ))}
        </div>
      )}
    </div>
  )
}

function UserRow({ member }: { member: ApiOrgMember }) {
  const initials = member.user_name.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()
  const canManage = member.role === "member"

  const inner = (
    <>
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <span className="text-xs font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{member.user_name}</p>
        <p className="text-xs text-muted-foreground">{member.user_email}</p>
      </div>
      <RoleBadge role={member.role as OrgRole} />
      {canManage && <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0 ml-1" />}
    </>
  )

  if (canManage) {
    return (
      <Link
        to="/users/$userId"
        params={{ userId: member.user_id }}
        className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/20 transition-colors"
      >
        {inner}
      </Link>
    )
  }

  return (
    <div className="flex items-center gap-3 px-4 py-3.5">
      {inner}
    </div>
  )
}

function RoleBadge({ role }: { role: OrgRole }) {
  if (role === "owner") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-500/10">
      <Crown className="h-2.5 w-2.5" />owner
    </Badge>
  )
  if (role === "admin") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
      <Shield className="h-2.5 w-2.5" />admin
    </Badge>
  )
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}
