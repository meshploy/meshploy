import { createFileRoute, Link, Outlet, useParams, useRouterState } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ArrowLeft, Layers } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { stacks as stacksApi, type ApiStack } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

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
  const pathname = useRouterState({ select: (s) => s.location.pathname })

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
    { label: "Services", seg: "services", to: "/projects/$id/stacks/$stackId/services" as const },
    { label: "Editor",   seg: "editor",   to: "/projects/$id/stacks/$stackId/editor"   as const },
  ]

  const activeSegment = pathname.split(`/stacks/${stackId}/`)[1]?.split("/")[0] ?? ""

  return (
    <div className="flex flex-col min-h-full">
      {/* Header */}
      <div className="border-b border-border/60 bg-background">
        <div className="px-6 pt-5 pb-0">
          <div className="flex items-center gap-3 mb-4">
            <Link
              to="/projects/$id/stacks"
              params={{ id: projectId }}
              className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Back to stacks
            </Link>
            <span className="text-border/60 text-xs">·</span>
            <div className="flex items-center gap-2.5">
              <div className="flex items-center justify-center w-7 h-7 rounded-md bg-muted border border-border/60 shrink-0">
                <Layers className="h-3 w-3 text-muted-foreground" />
              </div>
              {isLoading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
              ) : stack ? (
                <div className="flex items-center gap-2">
                  <span className="text-sm font-semibold">{stack.name}</span>
                  <Badge className={`text-[10px] px-1.5 py-0 h-4 border shrink-0 ${STATUS_STYLES[stack.status]}`}>
                    {stack.status}
                  </Badge>
                </div>
              ) : null}
            </div>
          </div>

          <nav className="flex items-center gap-0 -mb-px">
            {tabs.map(({ label, seg, to }) => {
              const isActive = activeSegment === seg || (activeSegment === "" && seg === "services")
              return (
                <Link
                  key={label}
                  to={to}
                  params={{ id: projectId, stackId }}
                  className={cn(
                    "px-4 py-2.5 text-sm border-b-2 transition-colors whitespace-nowrap",
                    isActive
                      ? "text-foreground border-foreground/25"
                      : "text-muted-foreground border-transparent hover:text-foreground hover:border-border/60"
                  )}
                >
                  {label}
                </Link>
              )
            })}
          </nav>
        </div>
      </div>

      <div className="flex-1">
        <Outlet />
      </div>
    </div>
  )
}
