import { createFileRoute, Link, Outlet, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Box, Database, Loader2, Play, ServerCrash, Square } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { services as servicesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { DetailPageHeader, tabLinkCls } from "@/components/layout/detail-page-header"
import { useIsAdmin } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id/services/$serviceId")({
  component: ServiceLayout,
})

const APP_TABS = [
  { label: "Overview",    to: "/projects/$id/services/$serviceId/overview"    },
  { label: "Deployments", to: "/projects/$id/services/$serviceId/deployments" },
  { label: "Config",      to: "/projects/$id/services/$serviceId/config"      },
  { label: "Pods",        to: "/projects/$id/services/$serviceId/pods"        },
  { label: "Logs",        to: "/projects/$id/services/$serviceId/logs"        },
  { label: "Settings",    to: "/projects/$id/services/$serviceId/settings"    },
]

const DB_TABS = [
  { label: "Overview",    to: "/projects/$id/services/$serviceId/overview"    },
  { label: "Deployments", to: "/projects/$id/services/$serviceId/deployments" },
  { label: "Pods",        to: "/projects/$id/services/$serviceId/pods"        },
  { label: "Backups",     to: "/projects/$id/services/$serviceId/backups"     },
  { label: "Logs",        to: "/projects/$id/services/$serviceId/logs"        },
  { label: "Settings",    to: "/projects/$id/services/$serviceId/settings"    },
]

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
  const isAdmin = useIsAdmin()
  const queryClient = useQueryClient()

  const queryKey = ["service", orgId, projectId, serviceId]

  const { data: service, isLoading, isError } = useQuery({
    queryKey,
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
    refetchInterval: (query) => query.state.data?.status === "deploying" ? 3000 : false,
  })

  const startMutation = useMutation({
    mutationFn: () => servicesApi.start(orgId!, projectId, serviceId, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })

  const stopMutation = useMutation({
    mutationFn: () => servicesApi.stop(orgId!, projectId, serviceId, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
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
      <DetailPageHeader
        backTo={service.type === "database" ? "/projects/$id/databases" : "/projects/$id/services"}
        backLabel={service.type === "database" ? "Back to databases" : "Back to services"}
        backParams={{ id: projectId }}
        icon={service.type === "database"
          ? <Database className="h-4 w-4 text-muted-foreground" />
          : <Box className="h-4 w-4 text-muted-foreground" />
        }
        name={service.name}
        badge={
          <Badge className={`text-[10px] px-1.5 py-0 h-4 border ${STATUS_STYLES[service.status] ?? STATUS_STYLES.stopped}`}>
            {service.status}
          </Badge>
        }
        actions={
          <>
            {(service.status === "stopped" || service.status === "failed") && !!service.image && (
              <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs"
                onClick={() => startMutation.mutate()} disabled={startMutation.isPending}>
                {startMutation.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
                Start
              </Button>
            )}
            {(service.status === "running" || service.status === "deploying") && (
              <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs"
                onClick={() => stopMutation.mutate()} disabled={stopMutation.isPending}>
                {stopMutation.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Square className="h-3 w-3" />}
                Stop
              </Button>
            )}
          </>
        }
      >
        {[...(service.type === "database" ? DB_TABS : APP_TABS),
          ...(isAdmin ? [{ label: "Permissions", to: "/projects/$id/services/$serviceId/permissions" as const }] : [])
        ].map(({ label, to }) => (
          <Link
            key={to}
            to={to}
            params={{ id: projectId, serviceId }}
            className={tabLinkCls}
            activeOptions={{ exact: false }}
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
