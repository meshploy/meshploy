import { createFileRoute, Link, Outlet, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Layers } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { stacks as stacksApi, type ApiStack } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { DetailPageHeader, tabLinkCls } from "@/components/layout/detail-page-header"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId")({
  component: StackLayout,
})

const STATUS_STYLES: Record<ApiStack["status"], string> = {
  idle:     "bg-muted text-muted-foreground border-border",
  applying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:   "bg-destructive/10 text-destructive border-destructive/20",
}

function StackLayout() {
  const { id: projectId, stackId } = useParams({ from: "/_app/projects/$id/stacks/$stackId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const { data: stack, isLoading } = useQuery({
    queryKey: ["stack", orgId, projectId, stackId],
    queryFn: () => stacksApi.get(orgId!, projectId, stackId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const d = query.state.data as ApiStack | undefined
      return d?.status === "applying" ? 3000 : false
    },
  })

  const tabs = [
    { label: "Services",  to: "/projects/$id/stacks/$stackId/services"   as const },
    { label: "Variables", to: "/projects/$id/stacks/$stackId/variables"  as const },
    { label: "Editor",    to: "/projects/$id/stacks/$stackId/editor"     as const },
  ]

  return (
    <div className="flex flex-col min-h-full">
      <DetailPageHeader
        backTo="/projects/$id/stacks"
        backLabel="Back to stacks"
        backParams={{ id: projectId }}
        icon={<Layers className="h-4 w-4 text-muted-foreground" />}
        name={isLoading ? "…" : (stack?.name ?? "")}
        badge={stack && (
          <Badge className={`text-[10px] px-1.5 py-0 h-4 border shrink-0 ${STATUS_STYLES[stack.status]}`}>
            {stack.status}
          </Badge>
        )}
      >
        {tabs.map(({ label, to }) => (
          <Link
            key={label}
            to={to}
            params={{ id: projectId, stackId }}
            className={tabLinkCls}
          >
            {label}
          </Link>
        ))}
      </DetailPageHeader>

      <div className="flex-1">
        <Outlet />
      </div>
    </div>
  )
}
