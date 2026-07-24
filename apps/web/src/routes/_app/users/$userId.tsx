import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, User } from "lucide-react"
import { useEffect } from "react"
import {
  orgs as orgsApi,
  type ApiOrgMember,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useOrgRole } from "@/store/org-store"
import { Badge } from "@/components/ui/badge"
import { PrincipalPermissions } from "@/components/permissions/principal-permissions"

export const Route = createFileRoute("/_app/users/$userId")({
  component: UserDetailPage,
})

function UserDetailPage() {
  const { userId } = Route.useParams()
  const role = useOrgRole()
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const { data: members = [] } = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId, token),
    enabled: !!orgId,
  })

  const member = members.find((m) => m.user_id === userId)

  if (!member && members.length > 0) {
    return (
      <div className="p-6 text-sm text-muted-foreground">Member not found.</div>
    )
  }

  return (
    <div className="p-6 max-w-2xl space-y-6">
      {/* Header */}
      <div className="space-y-4">
        <Link
          to="/users"
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          Users
        </Link>

        {member && <MemberHeader member={member} />}
      </div>

      <PrincipalPermissions orgId={orgId} principalId={userId} token={token} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

function MemberHeader({ member }: { member: ApiOrgMember }) {
  const initials = member.user_name.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()

  return (
    <div className="flex items-center gap-3">
      <div className="flex items-center justify-center w-10 h-10 rounded-full bg-primary/10 shrink-0">
        <span className="text-sm font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-base font-semibold">{member.user_name}</p>
          <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5">
            <User className="h-2.5 w-2.5" />member
          </Badge>
        </div>
        <p className="text-sm text-muted-foreground">{member.user_email}</p>
      </div>
    </div>
  )
}
