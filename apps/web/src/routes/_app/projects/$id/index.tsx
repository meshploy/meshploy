import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Globe, Loader2, Plus, Server, ServerCrash } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { NewRouteModal } from "@/components/routes/new-route-modal"
import { projects as projectsApi, services as servicesApi, routes as routesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { ServiceStatus } from "@/types"

const STATUS_STYLES: Record<ServiceStatus, string> = {
  running: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  deploying: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  failed: "bg-destructive/10 text-destructive border-destructive/20",
  stopped: "bg-muted text-muted-foreground border-border",
}

export const Route = createFileRoute("/_app/projects/$id/")({
  component: ProjectDetailPage,
})

function ProjectDetailPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const [routeModalOpen, setRouteModalOpen] = useState(false)

  const projectQuery = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const servicesQuery = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  const routesQuery = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })

  if (projectQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading project…</span>
      </div>
    )
  }

  if (projectQuery.isError || !projectQuery.data) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Project not found</p>
      </div>
    )
  }

  const project = projectQuery.data
  const serviceList = servicesQuery.data ?? []
  const routeList = routesQuery.data ?? []

  return (
    <>
    {orgId && (
      <NewRouteModal
        open={routeModalOpen}
        onOpenChange={setRouteModalOpen}
        orgId={orgId}
        projectId={projectId}
      />
    )}
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-xl font-semibold tracking-tight">{project.name}</h1>
            <code className="text-xs font-mono text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">
              ns/{project.slug}
            </code>
          </div>
          <p className="text-sm text-muted-foreground mt-0.5">
            {serviceList.length} service{serviceList.length !== 1 ? "s" : ""} · {routeList.length} route{routeList.length !== 1 ? "s" : ""}
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setRouteModalOpen(true)}>
            <Plus className="h-3.5 w-3.5" />
            New Route
          </Button>
          <Button
            size="sm"
            className="gap-1.5"
            render={<Link to="/projects/$id/new-service" params={{ id: projectId }} />}
          >
            <Plus className="h-3.5 w-3.5" />
            New Service
          </Button>
        </div>
      </div>

      {/* Services */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Server className="h-3.5 w-3.5 text-muted-foreground" />
          <h2 className="text-sm font-medium">Services</h2>
          {servicesQuery.isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
        </div>
        {serviceList.length === 0 && !servicesQuery.isLoading ? (
          <div className="rounded-lg border border-dashed border-border/60 py-10 flex flex-col items-center gap-3">
            <Server className="h-7 w-7 text-muted-foreground/40" />
            <div className="text-center">
              <p className="text-sm text-muted-foreground">No services yet</p>
              <p className="text-xs text-muted-foreground/60 mt-0.5">Deploy your first service to get started</p>
            </div>
            <Button
              size="sm"
              className="gap-1.5 mt-1"
              render={<Link to="/projects/$id/new-service" params={{ id: projectId }} />}
            >
              <Plus className="h-3.5 w-3.5" />
              New Service
            </Button>
          </div>
        ) : (
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {serviceList.map((svc) => (
              <div
                key={svc.id}
                className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/20 transition-colors cursor-pointer"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-medium text-foreground">{svc.name}</p>
                    <Badge className={`text-[10px] px-1.5 py-0 h-4.5 border ${STATUS_STYLES[svc.status]}`}>
                      {svc.status}
                    </Badge>
                    <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-4.5">
                      {svc.type}
                    </Badge>
                  </div>
                  <code className="text-[11px] font-mono text-muted-foreground/70 mt-0.5 block truncate">
                    {svc.image}
                  </code>
                </div>
                <span className="text-xs text-muted-foreground shrink-0">
                  {svc.type === "database" ? "1 replica" : `×${svc.replicas}`}
                </span>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Routes */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Globe className="h-3.5 w-3.5 text-muted-foreground" />
          <h2 className="text-sm font-medium">Routes</h2>
          {routesQuery.isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
        </div>
        {routeList.length === 0 && !routesQuery.isLoading ? (
          <div className="rounded-lg border border-dashed border-border/60 py-8 flex flex-col items-center gap-3">
            <Globe className="h-7 w-7 text-muted-foreground/40" />
            <p className="text-sm text-muted-foreground">No routes configured</p>
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5 mt-1"
              onClick={() => setRouteModalOpen(true)}
            >
              <Plus className="h-3.5 w-3.5" />
              New Route
            </Button>
          </div>
        ) : (
          <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
            {routeList.map((route) => (
              <div
                key={route.id}
                className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/20 transition-colors cursor-pointer"
              >
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-foreground font-mono">{route.hostname}</p>
                  <code className="text-[11px] font-mono text-muted-foreground/70 mt-0.5 block truncate">
                    → {route.target_ip}:{route.target_port}
                  </code>
                </div>
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4.5">
                  active
                </Badge>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
    </>
  )
}
