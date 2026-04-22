import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, Globe, Loader2, ServerCrash, Trash2 } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { routes as routesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id/routes/$routeId")({
  component: RouteDetailPage,
})

const ZONE_STYLES: Record<string, string> = {
  public:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  internal: "bg-blue-500/10 text-blue-400 border-blue-500/20",
  preview:  "bg-violet-500/10 text-violet-400 border-violet-500/20",
}

function RouteDetailPage() {
  const { id: projectId, routeId } = useParams({ from: "/_app/projects/$id/routes/$routeId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)

  const { data: route, isLoading, isError } = useQuery({
    queryKey: ["route", orgId, projectId, routeId],
    queryFn: () => routesApi.get(orgId!, projectId, routeId, token),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: () => routesApi.delete(orgId!, projectId, routeId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
      navigate({ to: "/projects/$id/routes", params: { id: projectId } })
    },
  })

  const goBack = () => navigate({ to: "/projects/$id/routes", params: { id: projectId } })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-32">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (isError || !route) {
    return (
      <div className="flex flex-col items-center justify-center h-32 gap-2 text-muted-foreground">
        <ServerCrash className="h-6 w-6 text-destructive/60" />
        <p className="text-xs">Route not found</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" className="gap-1.5 -ml-1 h-7 text-muted-foreground" onClick={goBack}>
          <ArrowLeft className="h-3.5 w-3.5" />
          Routes
        </Button>
      </div>

      {/* Header */}
      <div className="flex items-start gap-3">
        <div className="h-9 w-9 rounded-md bg-muted/40 border border-border/40 flex items-center justify-center shrink-0">
          <Globe className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium font-mono">{route.hostname}</p>
            <Badge
              className={`text-[10px] px-1.5 py-0 h-4 border shrink-0 ${ZONE_STYLES[route.zone] ?? ""}`}
            >
              {route.zone}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 font-mono">
            → {route.target_ip}:{route.target_port}
          </p>
        </div>
      </div>

      {/* Details */}
      <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
        <DetailRow label="Hostname" value={route.hostname} mono />
        <DetailRow label="Zone" value={route.zone} />
        {route.subdomain && <DetailRow label="Subdomain" value={route.subdomain} mono />}
        <DetailRow label="Target IP" value={route.target_ip} mono />
        <DetailRow label="Target Port" value={String(route.target_port)} />
        {route.service_id && <DetailRow label="Service ID" value={route.service_id} mono />}
        <DetailRow label="Created" value={new Date(route.created_at).toLocaleString()} />
      </div>

      {/* Danger zone */}
      <div className="rounded-lg border border-destructive/30 overflow-hidden">
        <div className="px-4 py-3 bg-destructive/5">
          <p className="text-xs font-medium text-destructive">Danger Zone</p>
        </div>
        <div className="px-4 py-4 flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium">Delete route</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Removes this routing rule. Traffic to <span className="font-mono">{route.hostname}</span> will stop being proxied.
            </p>
          </div>
          {!confirmDelete ? (
            <Button
              variant="destructive"
              size="sm"
              className="shrink-0 gap-1.5"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete
            </Button>
          ) : (
            <div className="flex items-center gap-2 shrink-0">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setConfirmDelete(false)}
                disabled={deleteMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                className="gap-1.5"
                onClick={() => deleteMutation.mutate()}
                disabled={deleteMutation.isPending}
              >
                {deleteMutation.isPending ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Trash2 className="h-3.5 w-3.5" />
                )}
                Confirm delete
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function DetailRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <span className="text-xs text-muted-foreground w-28 shrink-0">{label}</span>
      <span className={`text-xs text-foreground flex-1 ${mono ? "font-mono" : ""}`}>{value}</span>
    </div>
  )
}
