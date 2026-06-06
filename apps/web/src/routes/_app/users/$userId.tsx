import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, ChevronDown, ChevronRight, Loader2, User } from "lucide-react"
import { useEffect, useState } from "react"
import {
  orgs as orgsApi,
  projects as projectsApi,
  permissions as permissionsApi,
  type ApiOrgMember,
  type ApiPermission,
  type ApiProject,
  RESOURCE_ACTIONS,
  type ResourceAction,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useOrgRole } from "@/store/org-store"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/users/$userId")({
  component: UserDetailPage,
})

const ACTION_LABELS: Record<ResourceAction, string> = {
  view: "view",
  create: "create",
  deploy: "deploy",
  update: "update",
  delete: "delete",
}

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

  const { data: projectList = [], isLoading: projectsLoading } = useQuery({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const { data: memberPerms = [], isLoading: permsLoading } = useQuery({
    queryKey: ["member-permissions", orgId, userId],
    queryFn: () => permissionsApi.listForMember(orgId, userId, token),
    enabled: !!orgId && !!userId,
  })

  const member = members.find((m) => m.user_id === userId)

  if (!member && members.length > 0) {
    return (
      <div className="p-6 text-sm text-muted-foreground">Member not found.</div>
    )
  }

  const isLoading = projectsLoading || permsLoading || members.length === 0

  // Project-level permissions keyed by projectId → Set of actions
  const projectGrantMap = buildProjectGrantMap(memberPerms)

  // Resource-level (non-project) permissions
  const resourcePerms = memberPerms.filter((p) => p.resource_type !== "project")

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

      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : (
        <ProjectPermissionsSection
          orgId={orgId}
          userId={userId}
          token={token}
          projects={projectList}
          projectGrantMap={projectGrantMap}
          resourcePerms={resourcePerms}
        />
      )}
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

// ---------------------------------------------------------------------------
// Project permissions section
// ---------------------------------------------------------------------------

function ProjectPermissionsSection({
  orgId, userId, token, projects, projectGrantMap, resourcePerms,
}: {
  orgId: string
  userId: string
  token: string
  projects: ApiProject[]
  projectGrantMap: Map<string, Set<ResourceAction>>
  resourcePerms: ApiPermission[]
}) {
  const qc = useQueryClient()
  // Track which (projectId, action) pairs are pending
  const [pending, setPending] = useState<Set<string>>(new Set())

  const { mutate } = useMutation({
    mutationFn: ({ projectId, action, granted }: { projectId: string; action: ResourceAction; granted: boolean }) => {
      const body = { resource_type: "project" as const, resource_id: projectId, action }
      return granted
        ? permissionsApi.revoke(orgId, userId, body, token)
        : permissionsApi.grant(orgId, userId, body, token)
    },
    onMutate: ({ projectId, action }) => {
      setPending((s) => new Set(s).add(`${projectId}-${action}`))
    },
    onSettled: (_, __, { projectId, action }) => {
      setPending((s) => { const n = new Set(s); n.delete(`${projectId}-${action}`); return n })
      qc.invalidateQueries({ queryKey: ["member-permissions", orgId, userId] })
    },
  })

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-medium">Project Permissions</h2>
        <p className="text-xs text-muted-foreground">Click an action to grant or revoke access</p>
      </div>

      <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
        {projects.map((project) => {
          const granted = projectGrantMap.get(project.id) ?? new Set<ResourceAction>()
          const overrides = resourcePerms.filter((p) =>
            // Will be cross-referenced in Phase 2 — shown as count for now
            p.resource_id !== project.id
          )
          // Count overrides that conceptually belong to this project — approximation until resource lookup is added
          const overrideCount = 0 // Phase 2: resolve resource → project mapping

          return (
            <ProjectRow
              key={project.id}
              project={project}
              granted={granted}
              overrideCount={overrideCount}
              pending={pending}
              onToggle={(action) => mutate({ projectId: project.id, action, granted: granted.has(action) })}
            />
          )
        })}
      </div>

      {resourcePerms.length > 0 && (
        <p className="text-xs text-muted-foreground/60 pt-1 px-1">
          {resourcePerms.length} resource-level override{resourcePerms.length !== 1 ? "s" : ""} — visible in service / stack / job settings
        </p>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Project row with pill toggles + collapsible overrides
// ---------------------------------------------------------------------------

function ProjectRow({
  project, granted, overrideCount, pending, onToggle,
}: {
  project: ApiProject
  granted: Set<ResourceAction>
  overrideCount: number
  pending: Set<string>
  onToggle: (action: ResourceAction) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const hasOverrides = overrideCount > 0

  return (
    <div>
      <div className="flex items-center gap-3 px-4 py-3">
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium">{project.name}</p>
          <p className="text-xs text-muted-foreground/50 font-mono">{project.slug}</p>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {RESOURCE_ACTIONS.map((action) => {
            const isGranted = granted.has(action)
            const isLoading = pending.has(`${project.id}-${action}`)
            return (
              <ActionPill
                key={action}
                action={action}
                granted={isGranted}
                loading={isLoading}
                onToggle={() => onToggle(action)}
              />
            )
          })}
        </div>

        {hasOverrides && (
          <button
            onClick={() => setExpanded((v) => !v)}
            className="flex items-center gap-1 text-[10px] text-muted-foreground/50 hover:text-muted-foreground transition-colors shrink-0 ml-1"
          >
            {overrideCount} override{overrideCount !== 1 ? "s" : ""}
            {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          </button>
        )}
      </div>

      {/* Phase 2: override rows rendered here when expanded */}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Action pill toggle
// ---------------------------------------------------------------------------

function ActionPill({ action, granted, loading, onToggle }: {
  action: ResourceAction
  granted: boolean
  loading: boolean
  onToggle: () => void
}) {
  return (
    <button
      onClick={onToggle}
      disabled={loading}
      className={cn(
        "h-6 px-2.5 text-[11px] font-medium rounded-md border transition-colors select-none",
        granted
          ? "bg-primary/15 text-primary border-primary/30 hover:bg-primary/25"
          : "bg-transparent text-muted-foreground/35 border-border/30 hover:text-muted-foreground/70 hover:border-border/60",
        loading && "opacity-40 cursor-wait pointer-events-none"
      )}
    >
      {loading ? "…" : ACTION_LABELS[action]}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function buildProjectGrantMap(perms: ApiPermission[]): Map<string, Set<ResourceAction>> {
  const map = new Map<string, Set<ResourceAction>>()
  for (const p of perms) {
    if (p.resource_type !== "project") continue
    if (!map.has(p.resource_id)) map.set(p.resource_id, new Set())
    map.get(p.resource_id)!.add(p.action)
  }
  return map
}
