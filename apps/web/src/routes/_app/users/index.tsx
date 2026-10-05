import { OptionSelect } from "@/components/layout/option-select"
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, Clock, Copy, Crown, Loader2, Plus, Shield, User } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  oauth as oauthApi,
  orgs as orgsApi,
  type ApiOrgInvitation,
  type ApiOrgMember,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin, useOrgRole } from "@/store/org-store"
import { HelpButton } from "@/help/help-button"
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

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const [search, setSearch] = useState("")
  const [roleFilter, setRoleFilter] = useState("all")
  const [showInvite, setShowInvite] = useState(false)

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
  const { data: sessions = {} } = useQuery({
    queryKey: ["session-counts", orgId],
    queryFn: () => oauthApi.sessionCounts(orgId, token),
    enabled: !!orgId && canEditRoles,
  })

  const total = members.length + invitations.length

  return (
    <div className="console-page space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight flex items-center gap-2" aria-labelledby="page-title"><span id="page-title">Users</span><HelpButton topic="users" label="How users and access work" /></h1>
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
            onClick={() => setShowInvite(true)}
          >
            <Plus className="h-3.5 w-3.5" />
            Invite
          </Button>
        )}
      </div>

      <div className="flex flex-wrap gap-3"><Input aria-label="Search users" placeholder="Search by name or email…" value={search} onChange={e=>setSearch(e.target.value)} className="h-10 max-w-md"/><OptionSelect label="Filter users by role" value={roleFilter} onChange={setRoleFilter} options={[{"value": "all", "label": "All roles"}, {"value": "owner", "label": "Owner"}, {"value": "admin", "label": "Admin"}, {"value": "member", "label": "Member"}]} /></div>
      {/* Member list */}
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : (
        <div className="console-data-table quiet-surface rounded-xl border border-border overflow-x-auto"><table className="w-full min-w-[680px] text-left"><thead><tr className="border-b border-border text-xs text-muted-foreground"><th className="p-4 font-medium">Member</th><th className="p-4 font-medium">Status</th><th className="p-4 font-medium">Role</th><th className="p-4 font-medium">Sessions</th><th className="p-4 font-medium">Access</th></tr></thead><tbody>
          {members.filter(m => `${m.user_name} ${m.user_email}`.toLowerCase().includes(search.toLowerCase()) && (roleFilter === "all" || m.role === roleFilter)).map((member) => (
            <MemberRow
              key={member.id}
              member={member}
              canEdit={canEditRoles && member.role !== "owner"}
              sessions={sessions[member.user_id] ?? 0}
              orgId={orgId}
              token={token}
            />
          ))}
          {invitations.filter(i => i.email.toLowerCase().includes(search.toLowerCase()) && (roleFilter === "all" || i.role === roleFilter)).map((inv) => (
            <PendingInviteRow key={inv.id} invitation={inv} />
          ))}
        {!members.some(m => `${m.user_name} ${m.user_email}`.toLowerCase().includes(search.toLowerCase()) && (roleFilter === "all" || m.role === roleFilter)) && !invitations.some(i => i.email.toLowerCase().includes(search.toLowerCase()) && (roleFilter === "all" || i.role === roleFilter)) && <tr><td colSpan={5} className="p-8 text-center text-sm text-muted-foreground">No matching users or invitations.</td></tr>}</tbody></table></div>
      )}

      <InviteDialog open={showInvite} onOpenChange={setShowInvite} orgId={orgId} token={token} />
    </div>
  )
}

// ---------------------------------------------------------------------------

/**
 * Inviting follows the same shape as creating an agent: a dialog with the
 * fields, then the one thing the server hands back once. It was an inline strip
 * above the member list, which read as a different kind of action than it is.
 */
function InviteDialog({ open, onOpenChange, orgId, token }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const [email, setEmail] = useState("")
  const [role, setRole] = useState<"admin" | "member">("member")
  const [link, setLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (open) {
      setEmail(""); setRole("member"); setLink(null); setCopied(false)
    }
  }, [open])

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => orgsApi.createInvitation(orgId, email.trim(), role, token),
    onSuccess: (inv) => {
      qc.invalidateQueries({ queryKey: ["org-invitations", orgId] })
      setLink(`${window.location.origin}/register?token=${inv.token}`)
    },
  })

  function copyLink() {
    if (!link) return
    navigator.clipboard.writeText(link)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{link ? "Invitation link" : "Invite a member"}</DialogTitle>
          <DialogDescription>
            {link
              ? "Share this link. It expires in 7 days and can be used once."
              : "Generate a link for someone to join this organization."}
          </DialogDescription>
        </DialogHeader>

        {link ? (
          <div className="flex items-center gap-2">
            <div className="flex-1 h-9 flex items-center px-3 rounded-md border border-border/60 bg-muted/20 font-mono text-xs text-muted-foreground overflow-hidden">
              <span className="truncate">{link}</span>
            </div>
            <Button variant="outline" className="shrink-0 gap-1.5" onClick={copyLink}>
              {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        ) : (
          <div className="space-y-5">
            <div className="flex flex-col gap-3">
              <label className="text-xs font-medium text-muted-foreground">Email address</label>
              <Input
                type="email"
                placeholder="person@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && email.trim()) mutate() }}
                className="h-9 text-sm"
                autoFocus
              />
            </div>

            <div className="flex flex-col gap-3">
              <label className="text-xs font-medium text-muted-foreground">Role</label>
              <Select value={role} onValueChange={(v) => v && setRole(v as "admin" | "member")}>
                <SelectTrigger className="w-full h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue>{role}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">member</SelectItem>
                  <SelectItem value="admin">admin</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {error && (
              <p className="text-xs text-destructive">{(error as Error).message}</p>
            )}
          </div>
        )}

        <DialogFooter>
          {link ? (
            <Button onClick={() => onOpenChange(false)}>Done</Button>
          ) : (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
              <Button onClick={() => mutate()} disabled={isPending || !email.trim()} className="gap-1.5">
                {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                Generate link
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------------------

function MemberRow({ member, canEdit, sessions, orgId, token }: {
  member: ApiOrgMember
  canEdit: boolean
  /** Connected sessions: CLIs signed in as them, and assistants they connected here. */
  sessions: number
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const initials = member.user_name.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase()
  // Every member has a page: a member's grants are managed there, and an
  // owner's or admin's page shows the machines that reach everything as them.
  const canManagePermissions = member.role === "member"

  // A role change asks first: admin is every resource and every machine on
  // the mesh, member only what was granted.
  const [asking, setAsking] = useState<"admin" | "member" | null>(null)
  const { mutate: changeRole, isPending, error } = useMutation({
    mutationFn: (role: "admin" | "member") => orgsApi.updateMember(orgId, member.user_id, role, token),
    onSuccess: () => {
      setAsking(null)
      qc.invalidateQueries({ queryKey: ["org-members", orgId] })
      qc.invalidateQueries({ queryKey: ["mesh-access", orgId] })
      qc.invalidateQueries({ queryKey: ["access-rules", orgId] })
    },
  })

  const avatarAndName = (
    <>
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <span className="text-xs font-semibold text-primary">{initials || "?"}</span>
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{member.user_name}</p>
        <p className="text-xs text-muted-foreground">{member.user_email}</p>
      </div>
    </>
  )

  const roleControl = canEdit ? (
    <Select
      value={member.role}
      onValueChange={(v) => { if (v && v !== member.role) setAsking(v as "admin" | "member") }}
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
  )

  return <tr className="border-b border-border last:border-0 hover:bg-muted/20"><td className="p-4"><Link to="/users/$userId" params={{userId:member.user_id}} className="flex items-center gap-3">{avatarAndName}</Link></td><td className="p-4 text-xs text-muted-foreground">Member</td><td className="p-4">{roleControl}<Dialog open={asking !== null} onOpenChange={(o) => { if (!o) setAsking(null) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Make {member.user_name} {asking === "admin" ? "an admin" : "a member"}?</DialogTitle>
            <DialogDescription>
              {asking === "admin"
                ? "An admin can manage every project and resource in this organisation, and their machines reach every machine on the mesh. Their own grants stop mattering while they are an admin."
                : "A member can use only what they are granted, in the console and from their machines on the mesh. Grant them what they need on the Access page or their own page."}
            </DialogDescription>
          </DialogHeader>
          {error && <p role="alert" className="text-xs text-destructive">{error.message}</p>}
          <DialogFooter>
            <Button variant="outline" onClick={() => setAsking(null)}>Cancel</Button>
            <Button disabled={isPending} onClick={() => asking && changeRole(asking)}>
              {isPending && <Loader2 className="size-3.5 animate-spin" />}
              {asking === "admin" ? "Make admin" : "Make member"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog></td><td className="p-4 text-xs text-muted-foreground">{sessions > 0 ? <Link to="/users/$userId" params={{userId:member.user_id}} className="text-foreground hover:underline">{sessions} signed in</Link> : "None"}</td><td className="p-4">{canManagePermissions ? <Link to="/users/$userId" params={{userId:member.user_id}} aria-label={`Permissions for ${member.user_name}`} className="text-xs text-primary">Manage access</Link> : <Link to="/users/$userId" params={{userId:member.user_id}} aria-label={`Details for ${member.user_name}`} className="text-xs text-muted-foreground hover:text-foreground">Organization-wide</Link>}</td></tr>
}

function PendingInviteRow({ invitation }: { invitation: ApiOrgInvitation }) {
  const [copied, setCopied] = useState(false)

  function copyLink() {
    navigator.clipboard.writeText(`${window.location.origin}/register?token=${invitation.token}`)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return <tr className="border-b border-border last:border-0"><td className="p-4 text-sm">{invitation.email}</td><td className="p-4"><span className="text-xs text-muted-foreground inline-flex items-center gap-2"><Clock className="size-3"/>Invite pending</span></td><td className="p-4"><RoleBadge role={invitation.role as OrgRole}/></td><td className="p-4" /><td className="p-4"><Button size="sm" variant="ghost" onClick={copyLink} title="Copy invite link">{copied ? <Check className="size-3"/> : <Copy className="size-3"/>}{copied ? "Copied" : "Copy link"}</Button></td></tr>
}

function RoleBadge({ role }: { role: OrgRole }) {
  if (role === "owner") return (
    <Badge className="gap-1 text-[11px] px-1.5 py-0 h-5 bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-500/10">
      <Crown className="h-2.5 w-2.5" />owner
    </Badge>
  )
  if (role === "admin") return (
    <Badge className="gap-1 text-[11px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
      <Shield className="h-2.5 w-2.5" />admin
    </Badge>
  )
  return (
    <Badge variant="secondary" className="gap-1 text-[11px] px-1.5 py-0 h-5">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}
