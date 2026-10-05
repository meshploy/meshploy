import { ResourcePanel, ResourceFact, ResourceIntro } from "@/components/layout/resource-workbench"
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, Crown, Shield, User } from "lucide-react"
import { useEffect } from "react"
import {
  access as accessApi,
  nodes as nodesApi,
  orgs as orgsApi,
  type ApiOrgMember,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useOrgRole } from "@/store/org-store"
import { Badge } from "@/components/ui/badge"
import { PrincipalPermissions } from "@/components/permissions/principal-permissions"
import { MemberSessions } from "@/components/auth/connected-sessions"

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
      <div className="console-page p-6 text-sm text-muted-foreground">Member not found.</div>
    )
  }

  return (
    <div className="console-page space-y-6">
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

      <ResourceIntro title="Workspace access" description={member && member.role !== "member" ? "What this member can do, and the machines that act as them." : "Review the resources this member can access and manage explicit permissions."} />
      <div className="resource-overview-columns"><div className="min-w-0">{member && member.role !== "member" ? <OrganizationWide member={member} /> : <PrincipalPermissions orgId={orgId} principalId={userId} token={token} />}</div><aside className="space-y-6"><ResourcePanel title="Member details"><ResourceFact label="Name">{member?.user_name || "Loading…"}</ResourceFact><ResourceFact label="Email">{member?.user_email || "-"}</ResourceFact>{member?.role === "member" && <p className="mt-5 text-sm text-muted-foreground leading-relaxed">These grants are the same ones on the <Link to="/access" className="text-primary hover:underline">Access page</Link>, where each also shows what it opens on the mesh.</p>}</ResourcePanel>{member && <OnTheMesh orgId={orgId} member={member} token={token} />}{member && <ResourcePanel title="Connected sessions" description="CLIs signed in as this member, and AI assistants they connected here."><MemberSessions orgId={orgId} userId={userId} token={token} /></ResourcePanel>}</aside></div>
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
          <h1>{member.user_name}</h1>
          {/* The role they hold, as the Users list shows it. */}
          {member.role === "owner" ? (
            <Badge className="gap-1 text-[11px] px-1.5 py-0 h-5 bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-500/10">
              <Crown className="h-2.5 w-2.5" />owner
            </Badge>
          ) : member.role === "admin" ? (
            <Badge className="gap-1 text-[11px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
              <Shield className="h-2.5 w-2.5" />admin
            </Badge>
          ) : (
            <Badge variant="secondary" className="gap-1 text-[11px] px-1.5 py-0 h-5">
              <User className="h-2.5 w-2.5" />member
            </Badge>
          )}
        </div>
        <p className="text-sm text-muted-foreground">{member.user_email}</p>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// On the mesh
// ---------------------------------------------------------------------------

/**
 * The member's side of the Access page: the machines that act as them on the
 * mesh, and the rules that name them, a click from the full list.
 */
function OnTheMesh({ orgId, member, token }: { orgId: string; member: ApiOrgMember; token: string }) {
  const { data: machines = [] } = useQuery({ queryKey: ["nodes", orgId], queryFn: () => nodesApi.list(orgId, token) })
  const { data: rules = [] } = useQuery({ queryKey: ["access-rules", orgId], queryFn: () => accessApi.rules(orgId, token) })
  const mine = machines.filter((n) => n.owner_id === member.user_id && n.mesh_role === "mesh")
  const named = rules.filter((r) => r.from.id === member.user_id).length
  const everything = member.role === "owner" || member.role === "admin"
  return (
    <ResourcePanel title="On the mesh" description="The machines that act as this member, and what they may reach.">
      <div className="space-y-3 text-sm">
        {mine.length === 0 ? (
          <p className="text-muted-foreground">No machine of theirs is on the mesh.</p>
        ) : (
          <ul className="space-y-1">
            {mine.map((n) => (
              <li key={n.id}>
                <Link to="/nodes/$id" params={{ id: n.id }} className="hover:underline">{n.name}</Link>
                <span className="ml-2 font-mono text-xs text-muted-foreground">{n.tailscale_ip}</span>
              </li>
            ))}
          </ul>
        )}
        <p className="text-muted-foreground">
          {everything
            ? `As ${member.role === "owner" ? "an owner" : "an admin"}, their machines reach everything.`
            : named === 0 ? "No rule names them yet." : `${named} ${named === 1 ? "rule names" : "rules name"} them.`}
        </p>
        {!everything && (
          <Link to="/access" search={{ person: member.user_id }} className="inline-block text-sm text-primary hover:underline">
            See their rules on Access
          </Link>
        )}
      </div>
    </ResourcePanel>
  )
}

/**
 * An owner or admin in place of the grants editor: per-project grants do not
 * apply to them, which the page says rather than showing switches that do
 * nothing.
 */
function OrganizationWide({ member }: { member: ApiOrgMember }) {
  const owner = member.role === "owner"
  return (
    <ResourcePanel title="Organization-wide" description={owner ? "The organisation's owner." : "An admin of the organisation."}>
      <div className="space-y-2 text-sm text-muted-foreground leading-relaxed">
        <p>
          {member.user_name} can manage every project, service and resource here{owner ? ", its members and its settings, and hand the organisation on" : ", and its members"}.
          Their machines reach every machine on the mesh.
        </p>
        <p>Per-project grants do not apply while they are {owner ? "the owner" : "an admin"}; any they had are kept, and count again if they are made a member.</p>
        {!owner && <p>Change their role on the <Link to="/users" className="text-primary hover:underline">Users</Link> page.</p>}
      </div>
    </ResourcePanel>
  )
}
