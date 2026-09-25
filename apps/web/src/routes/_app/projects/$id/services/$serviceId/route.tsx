import { StatusPill } from "@/components/layout/resource-workbench"
import { createFileRoute, Link, Outlet, useParams, useNavigate } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Box, ChevronDown, Database, ExternalLink, Globe, Loader2, Play, ServerCrash, Square, Terminal, Plus } from "lucide-react"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"
import { services as servicesApi, deployments, stacks as stacksApi, routes as routesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { DetailPageHeader, tabLinkCls } from "@/components/layout/detail-page-header"
import { useIsAdmin } from "@/store/org-store"
import { livePoll } from "@/lib/live-poll"
import { OriginStrip } from "@/components/services/deployment-origin"

export const Route = createFileRoute("/_app/projects/$id/services/$serviceId")({
  component: ServiceLayout,
})

const APP_TABS = [
  { label: "Overview",    to: "/projects/$id/services/$serviceId/overview"    },
  { label: "Deployments", to: "/projects/$id/services/$serviceId/deployments" },
  { label: "Configuration", to: "/projects/$id/services/$serviceId/config"    },
  { label: "Pods",        to: "/projects/$id/services/$serviceId/pods"        },
  { label: "Logs",        to: "/projects/$id/services/$serviceId/logs"        },
  { label: "Settings",    to: "/projects/$id/services/$serviceId/settings"    },
]

const DB_TABS = [
  { label: "Overview",    to: "/projects/$id/services/$serviceId/overview"    },
  { label: "Deployments", to: "/projects/$id/services/$serviceId/deployments" },
  { label: "Configuration", to: "/projects/$id/services/$serviceId/config"    },
  { label: "Pods",        to: "/projects/$id/services/$serviceId/pods"        },
  { label: "Backups",     to: "/projects/$id/services/$serviceId/backups"     },
  { label: "Logs",        to: "/projects/$id/services/$serviceId/logs"        },
  { label: "Settings",    to: "/projects/$id/services/$serviceId/settings"    },
]


function ServiceLayout() {
  const { id: projectId, serviceId } = useParams({ from: "/_app/projects/$id/services/$serviceId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const isAdmin = useIsAdmin()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const deployMutation = useMutation({ mutationFn: () => deployments.trigger(orgId!, projectId, serviceId, token), onSuccess: (deployment) => { queryClient.invalidateQueries({ queryKey: ["deployments", orgId, projectId, serviceId] }); queryClient.invalidateQueries({ queryKey: ["service", orgId, projectId, serviceId] }); navigate({ to: "/projects/$id/services/$serviceId/deployments/$deploymentId", params: { id: projectId, serviceId, deploymentId: deployment.id } }) } })

  const queryKey = ["service", orgId, projectId, serviceId]

  const { data: service, isLoading, isError } = useQuery({
    queryKey,
    queryFn: () => servicesApi.get(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
    refetchInterval: livePoll<{ status: string }>((d) => d.status === "deploying"),
  })

  // Named so the subtitle can link to it: "Managed by a stack" is a dead end
  // when the stack is one click away.
  const { data: stack } = useQuery({
    queryKey: ["stack", orgId, projectId, service?.stack_id],
    queryFn: () => stacksApi.get(orgId!, projectId, service!.stack_id!, token),
    enabled: !!orgId && !!service?.stack_id,
  })

  // What runs here now, and where it came from: a branch built here, or an
  // image moved in from another level. Shares the overview's query.
  const { data: history } = useQuery({
    queryKey: ["deployments", orgId, projectId, serviceId],
    queryFn: () => deployments.list(orgId!, projectId, serviceId, token),
    enabled: !!orgId,
  })
  const current = history?.find((d) => d.status === "success")

  // The addresses a browser can open: the service's routes, internal ones
  // aside. Shares the overview's query.
  const { data: projectRoutes } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const addresses = (projectRoutes ?? []).filter((r) => r.zone !== "internal" && r.targets.some((t) => t.service_id === serviceId))

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
        subtitle={
          service.type === "database"
            ? "Database · Persistent data service"
            : service.stack_id
              ? <>
                  Application ·{" "}
                  <Link
                    to="/projects/$id/stacks/$stackId"
                    params={{ id: projectId, stackId: service.stack_id }}
                    className="text-primary hover:text-primary/80 transition-colors"
                  >
                    Managed by {stack?.name ?? "a stack"}
                  </Link>
                </>
              : "Application · Standalone service"
        }
        badge={<StatusPill status={service.status} />}
        highlight={current && <OriginStrip origin={current} image={current.image} />}
        actions={
          <>
            {(service.status === "stopped" || service.status === "failed") && !!service.image && !!service.deployed_at && (
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
            {service.type !== "database" && projectRoutes && (
              addresses.length === 0 ? (
                <Button variant="outline" size="sm" render={<Link to="/projects/$id/new" params={{ id: projectId }} search={{ type: "route", service: serviceId }} />}><Globe className="size-4" />Add route</Button>
              ) : addresses.length === 1 ? (
                <Button variant="outline" size="sm" render={<a href={`https://${addresses[0].hostname}`} target="_blank" rel="noopener noreferrer" title={addresses[0].hostname} />}><ExternalLink className="size-4" />Open</Button>
              ) : (
                <DropdownMenu>
                  <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}><ExternalLink className="size-4" />Open<ChevronDown className="size-3.5" /></DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="min-w-[240px]">
                    {addresses.map((r) => (
                      <DropdownMenuItem key={r.id} render={<a href={`https://${r.hostname}`} target="_blank" rel="noopener noreferrer" />}>{r.hostname}</DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              )
            )}
            <Button variant="outline" size="sm" render={<Link to="/projects/$id/services/$serviceId/logs" params={{ id: projectId, serviceId }} />}><Terminal className="size-4" />Logs</Button>
            <Button size="sm" onClick={() => deployMutation.mutate()} disabled={deployMutation.isPending || service.status === "deploying"}><Plus className="size-4" />Deploy</Button>
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

      {(deployMutation.error || startMutation.error || stopMutation.error) && <p role="alert" className="mx-8 mt-4 text-sm text-destructive">{(deployMutation.error || startMutation.error || stopMutation.error)?.message}</p>}
      <div className="flex-1">
        <Outlet />
      </div>
    </div>
  )
}
