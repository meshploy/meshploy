import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Crown, Loader2, Shield, User } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { orgs as orgsApi, type ApiOrgMember } from "@/lib/api"
import type { OrgRole } from "@/types"

/** A role as the Users list and a member's page show it. */
export function RoleBadge({ role }: { role: OrgRole }) {
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

/**
 * A member's role, changed on their page where its consequences are shown.
 * The change asks first: admin is every resource and every machine on the
 * mesh, member only what was granted. The owner's role is not changed here.
 */
export function MemberRole({ member, orgId, token, canEdit }: { member: ApiOrgMember; orgId: string; token: string; canEdit: boolean }) {
  const qc = useQueryClient()
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

  if (!canEdit || member.role === "owner") return <RoleBadge role={member.role as OrgRole} />
  return (
    <>
      <Select value={member.role} onValueChange={(v) => { if (v && v !== member.role) setAsking(v as "admin" | "member") }} disabled={isPending}>
        <SelectTrigger size="sm" aria-label={`Role of ${member.user_name}`} className="w-28! text-xs bg-muted/20 border-border/50 px-2 gap-1">
          {isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <SelectValue>{member.role}</SelectValue>}
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="admin">admin</SelectItem>
          <SelectItem value="member">member</SelectItem>
        </SelectContent>
      </Select>
      <Dialog open={asking !== null} onOpenChange={(o) => { if (!o) setAsking(null) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Make {member.user_name} {asking === "admin" ? "an admin" : "a member"}?</DialogTitle>
            <DialogDescription>
              {asking === "admin"
                ? "An admin can manage every project and resource in this organisation, and their machines reach every machine on the mesh. Their own grants stop mattering while they are an admin."
                : "A member can use only what they are granted, in the console and from their machines on the mesh. Grant them what they need on this page or the Access page."}
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
      </Dialog>
    </>
  )
}
