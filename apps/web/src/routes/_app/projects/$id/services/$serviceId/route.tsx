import { createFileRoute, Link, Outlet, useParams } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ServerCrash } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { services as servicesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id/services/$serviceId")({
  component: ServiceLayout,
})

const SERVICE_TABS = [
  { label: "Deployments", to: "/projects/$id/services/$serviceId/deployments" },
  { label: "Config",      to: "/projects/$id/services/$serviceId/config"      },
  { label: "Logs",        to: "/projects/$id/services/$serviceId/logs"        },
  { label: "Settings",    to: "/projects/$id/services/$serviceId/settings"    },
] as const

const STATUS_STYLES: Record<string, string> = {
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
  stopped:   "bg-muted text-muted-foreground border-border",
}

function ServiceLayout() {
  const { id: projectId, serviceId } = useParams({ from: "/_app/projects/$id/services/$serviceId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: service, isLoading, isError } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-32">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (isError || !service) {
    return (
      <div className="flex flex-col items-center justify-center h-32 gap-2 text-muted-foreground">
        <ServerCrash className="h-6 w-6 text-destructive/60" />
        <p className="text-xs">Service not found</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col min-h-full">
      {/* Service sub-header */}
      <div className="border-b border-border/40 bg-muted/10">
        <div className="px-6 pt-3.5 pb-0">
          <div className="flex items-center gap-2 mb-2.5">
            <span className="text-sm font-medium">{service.name}</span>
            <Badge
              className={`text-[10px] px-1.5 py-0 h-4 border ${STATUS_STYLES[service.status] ?? STATUS_STYLES.stopped}`}
            >
              {service.status}
            </Badge>
          </div>

          <nav className="flex items-center -mb-px">
            {SERVICE_TABS.map(({ label, to }) => (
              <Link
                key={to}
                to={to}
                params={{ id: projectId, serviceId }}
                className={cn(
                  "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap",
                  "text-muted-foreground hover:text-foreground border-transparent hover:border-border/60"
                )}
                activeProps={{
                  className: cn(
                    "px-3.5 py-2 text-xs border-b-2 transition-colors whitespace-nowrap",
                    "text-foreground border-primary font-medium"
                  ),
                }}
                activeOptions={{ exact: false }}
              >
                {label}
              </Link>
            ))}
          </nav>
        </div>
      </div>

      <div className="flex-1">
        <Outlet />
      </div>
    </div>
  )
}
