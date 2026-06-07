import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, ChevronRight, Clock, Copy, Crown, Loader2, Plus, Shield, User } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  orgs as orgsApi,
  type ApiOrgInvitation,
  type ApiOrgMember,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin, useOrgRole } from "@/store/org-store"
import { cn } from "@/lib/utils"
import type { OrgRole } from "@/types"

export const Route = createFileRoute("/_app/users/")({
  component: UsersPage,
})

function UsersPage() {
  const role = useOrgRole()
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const userId = useAuthStore((s) => s.userId)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const isAdmin = useIsAdmin()
  const qc = useQueryClient()

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const [inviteEmail, setInviteEmail] = useState("")
  const [inviteRole, setInviteRole] = useState<"admin" | "member">("member")
  const [showInvite, setShowInvite] = useState(false)
  const [inviteLink, setInviteLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const { data: members = [], isLoading } = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId, token),
    enabled: !!orgId,
  })

  const { data: invitations = [] } = useQuery({
    queryKey: ["org-invitations", orgId],
    queryFn: () => orgsApi.listInvitations(orgId, token),
    enabled: !!orgId,
  })

  const callerRole = members.find((m) => m.user_id === userId)?.role ?? "member"
  const canEditRoles = callerRole === "owner" || callerRole === "admin"

  const { mutate: createInvite, isPending: inviting, error: inviteError } = useMutation({
    mutationFn: () => orgsApi.createInvitation(orgId, inviteEmail, inviteRole, token),
    onSuccess: (inv) => {
      qc.invalidateQueries({ queryKey: ["org-invitations", orgId] })
      setInviteLink(`${window.location.origin}/register?token=${inv.token}`)
      setInviteEmail("")
    },
  })

  function copyLink() {
    if (!inviteLink) return
    navigator.clipboard.writeText(inviteLink)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  function closeInviteForm() {
    setShowInvite(false)
    setInviteLink(null)
    setInviteEmail("")
    setInviteRole("member")
  }

  const total = members.length + invitations.length

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Users</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {total} {total === 1 ? "member" : "members"}
            {invitations.length > 0 && ` · ${invitations.length} pending`}
          </p>
        </div>
        {isAdmin && (
          <Button
            size="sm"
            variant="outline"
            className="gap-1.5 h-7 text-xs shrink-0"
            onClick={() => { setShowInvite((v) => !v); setInviteLink(null) }}
          >
            <Plus className="h-3.5 w-3.5" />
            Invite
          </Button>
        )}
      </div>

      {/* Invite form */}
      {showInvite && (
        <div className="rounded-lg border border-border/60 bg-muted/10 px-4 py-3 space-y-2">
          {inviteLink ? (
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">Share this link — expires in 7 days, single use.</p>
              <div className="flex items-center gap-2">
                <div className="flex-1 h-8 flex items-center px-3 rounded-md border border-border/60 bg-muted/20 font-mono text-xs text-muted-foreground overflow-hidden">
                  <span className="truncate">{inviteLink}</span>
                </div>
                <Button size="sm" variant="outline" className="h-8 shrink-0 gap-1.5" onClick={copyLink}>
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                  {copied ? "Copied" : "Copy"}
                </Button>
                <Button size="sm" variant="ghost" className="h-8 shrink-0" onClick={closeInviteForm}>Done</Button>
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-center gap-2">
                <Input
                  placeholder="Email address"
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  className="h-8 text-sm"
                  onKeyDown={(e) => { if (e.key === "Enter" && inviteEmail) createInvite() }}
                  autoFocus
                />
                <Select value={inviteRole} onValueChange={(v) => v && setInviteRole(v as "admin" | "member")}>
                  <SelectTrigger className="w-28! h-8 text-xs bg-muted/20 border-border/60 shrink-0">
                    <SelectValue>{inviteRole}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">member</SelectItem>
                    <SelectItem value="admin">admin</SelectItem>
                  </SelectContent>
                </Select>
                <Button size="sm" className="h-8 shrink-0" onClick={() => createInvite()} disabled={inviting || !inviteEmail}>
                  {inviting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : "Generate link"}
                </Button>
                <Button size="sm" variant="ghost" className="h-8 shrink-0" onClick={closeInviteForm}>Cancel</Button>
              </div>
              {inviteError && (
                <p className="text-xs text-destructive">{String((inviteError as Error).message)}</p>
              )}
            </>
          )}
        </div>
      )}

      {/* Member list */}
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {members.map((member) => (
            <MemberRow
              key={member.id}
              member={member}
              canEdit={canEditRoles && member.role !== "owner"}
              orgId={orgId}
              token={token}
            />
          ))}
          {invitations.map((inv) => (
            <PendingInviteRow key={inv.id} invitation={inv} />
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------

function MemberRow({ member, canEdit, orgId, token }: {
  member: ApiOrgMember
  canEdit: boolean
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const initials = member.user_name.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()
  const canManagePermissions = member.role === "member"

  const { mutate: changeRole, isPending } = useMutation({
    mutationFn: (role: "admin" | "member") => orgsApi.updateMember(orgId, member.user_id, role, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["org-members", orgId] }),
  })

  const inner = (
    <>
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <span className="text-xs font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{member.user_name}</p>
        <p className="text-xs text-muted-foreground">{member.user_email}</p>
      </div>
      {canEdit ? (
        <Select
          value={member.role}
          onValueChange={(v) => v && changeRole(v as "admin" | "member")}
          disabled={isPending}
        >
          <SelectTrigger className="w-24! h-6 text-[11px] bg-muted/20 border-border/50 px-2 gap-1 shrink-0">
            {isPending
              ? <Loader2 className="h-3 w-3 animate-spin" />
              : <SelectValue>{member.role}</SelectValue>
            }
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="admin">admin</SelectItem>
            <SelectItem value="member">member</SelectItem>
          </SelectContent>
        </Select>
      ) : (
        <RoleBadge role={member.role as OrgRole} />
      )}
      {canManagePermissions
        ? <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0 ml-1" />
        : <span className="w-5 shrink-0" />
      }
    </>
  )

  if (canManagePermissions) {
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

  return <div className="flex items-center gap-3 px-4 py-3.5">{inner}</div>
}

function PendingInviteRow({ invitation }: { invitation: ApiOrgInvitation }) {
  const [copied, setCopied] = useState(false)

  function copyLink() {
    navigator.clipboard.writeText(`${window.location.origin}/register?token=${invitation.token}`)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="flex items-center gap-3 px-4 py-3.5">
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-muted/40 shrink-0">
        <Clock className="h-3.5 w-3.5 text-muted-foreground" />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-muted-foreground">{invitation.email}</p>
        <p className="text-xs text-muted-foreground/60">Invite pending</p>
      </div>
      <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5 shrink-0">
        <Clock className="h-2.5 w-2.5" />{invitation.role}
      </Badge>
      <Button size="sm" variant="ghost" className="h-7 w-7 p-0 shrink-0" onClick={copyLink} title="Copy invite link">
        {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5 text-muted-foreground" />}
      </Button>
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
