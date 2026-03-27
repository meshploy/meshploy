import { createFileRoute } from "@tanstack/react-router"
import { Crown, Shield, User } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { mockOrgs, mockMembers } from "@/lib/mock-data"
import type { OrgRole } from "@/types"

const currentOrg = mockOrgs[0]

export const Route = createFileRoute("/_app/settings/")({
  component: SettingsPage,
})

function SettingsPage() {
  return (
    <div className="p-6 space-y-8 max-w-3xl">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Manage your organization settings and members</p>
      </div>

      <section className="space-y-4">
        <h2 className="text-sm font-medium text-foreground border-b border-border/40 pb-2">General</h2>
        <div className="grid gap-4">
          <Field label="Organization name" value={currentOrg.name} />
          <Field label="Slug" value={currentOrg.slug} mono />
          <Field label="Organization ID" value={currentOrg.id} mono muted />
        </div>
      </section>

      <section className="space-y-4">
        <div className="flex items-center justify-between border-b border-border/40 pb-2">
          <h2 className="text-sm font-medium text-foreground">Members</h2>
          <span className="text-xs text-muted-foreground">{mockMembers.length} members</span>
        </div>
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {mockMembers.map((member) => (
            <div key={member.id} className="flex items-center gap-3 px-4 py-3.5">
              <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
                <span className="text-xs font-semibold text-primary">
                  {member.name.split(" ").map((p) => p[0]).join("").slice(0, 2)}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-foreground">{member.name}</p>
                <p className="text-xs text-muted-foreground">{member.email}</p>
              </div>
              <RoleBadge role={member.role} />
            </div>
          ))}
        </div>
      </section>

      <section className="space-y-4">
        <h2 className="text-sm font-medium text-destructive border-b border-border/40 pb-2">Danger zone</h2>
        <div className="rounded-lg border border-destructive/30 p-4 flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">Delete organization</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Permanently delete this organization and all its projects, services, and nodes.
            </p>
          </div>
          <button className="shrink-0 text-xs text-destructive border border-destructive/40 px-3 py-1.5 rounded-md hover:bg-destructive/10 transition-colors">
            Delete org
          </button>
        </div>
      </section>
    </div>
  )
}

function Field({ label, value, mono, muted }: { label: string; value: string; mono?: boolean; muted?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-muted-foreground">{label}</label>
      <div className="flex items-center h-9 px-3 rounded-md border border-border/60 bg-muted/20">
        <span className={`text-sm ${mono ? "font-mono" : ""} ${muted ? "text-muted-foreground" : "text-foreground"}`}>{value}</span>
      </div>
    </div>
  )
}

function RoleBadge({ role }: { role: OrgRole }) {
  if (role === "owner") {
    return (
      <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-500/10">
        <Crown className="h-2.5 w-2.5" />owner
      </Badge>
    )
  }
  if (role === "admin") {
    return (
      <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
        <Shield className="h-2.5 w-2.5" />admin
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}
