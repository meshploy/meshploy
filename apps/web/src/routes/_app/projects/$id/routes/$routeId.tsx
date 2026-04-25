import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, ExternalLink, Globe, Loader2, RefreshCw, ServerCrash, Trash2 } from "lucide-react"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { routes as routesApi, services as servicesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, Field } from "@/components/services/form-primitives"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

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

  const [targetMode, setTargetMode] = useState<"service" | "manual">("manual")
  const [selectedServiceId, setSelectedServiceId] = useState("")
  const [targetIP, setTargetIP] = useState("")
  const [targetPort, setTargetPort] = useState(80)

  const { data: route, isLoading, isError } = useQuery({
    queryKey: ["route", orgId, projectId, routeId],
    queryFn: () => routesApi.get(orgId!, projectId, routeId, token),
    enabled: !!orgId,
  })

  const { data: allServices = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const serviceList = allServices.filter((s) => s.type === "application")

  useEffect(() => {
    if (!route) return
    if (route.service_id) {
      setTargetMode("service")
      setSelectedServiceId(route.service_id)
    } else {
      setTargetMode("manual")
    }
    setTargetIP(route.target_ip)
    setTargetPort(route.target_port)
  }, [route])

  const updateMutation = useMutation({
    mutationFn: () => {
      const body =
        targetMode === "service"
          ? { service_id: selectedServiceId || null, target_ip: targetIP, target_port: targetPort }
          : { service_id: null as string | null, target_ip: targetIP, target_port: targetPort }
      return routesApi.update(orgId!, projectId, routeId, body, token)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["route", orgId, projectId, routeId] }),
  })

  const syncMutation = useMutation({
    mutationFn: () => routesApi.syncIP(orgId!, projectId, routeId, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["route", orgId, projectId, routeId] }),
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
            <a
              href={`https://${route.hostname}`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-muted-foreground/50 hover:text-muted-foreground transition-colors"
            >
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 font-mono">
            → {route.target_ip}:{route.target_port}
          </p>
        </div>
        {route.service_id && (
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 h-7 text-xs shrink-0"
            onClick={() => syncMutation.mutate()}
            disabled={syncMutation.isPending}
            title="Re-resolve target IP from current service node"
          >
            {syncMutation.isPending
              ? <Loader2 className="h-3 w-3 animate-spin" />
              : <RefreshCw className="h-3 w-3" />}
            Sync IP
          </Button>
        )}
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

      {/* Target */}
      <Section title="Target" subtitle="Change where this route forwards traffic.">
        <div className="space-y-4">
          <div className="flex items-center gap-1 p-1 rounded-md bg-muted/30 border border-border/40 w-fit">
            <button
              onClick={() => setTargetMode("service")}
              className={`px-3 py-1 text-xs rounded transition-colors ${
                targetMode === "service"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              Service
            </button>
            <button
              onClick={() => setTargetMode("manual")}
              className={`px-3 py-1 text-xs rounded transition-colors ${
                targetMode === "manual"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              Manual
            </button>
          </div>

          {targetMode === "service" ? (() => {
            const selectedSvc = serviceList.find((x) => x.id === selectedServiceId)
            const selectedLabel = selectedSvc ? `${selectedSvc.name} :${selectedSvc.port}` : "— none —"
            return (
            <Field label="Service">
              <Select value={selectedServiceId} onValueChange={(v) => setSelectedServiceId(v ?? "")}>
                <SelectTrigger className="w-full h-9 text-sm">
                  <SelectValue placeholder="— none —">{selectedLabel}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">— none —</SelectItem>
                  {serviceList.map((svc) => (
                    <SelectItem key={svc.id} value={svc.id}>
                      {svc.name} :{svc.port}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            )
          })() : (
            <div className="grid grid-cols-2 gap-4">
              <Field label="Target IP">
                <Input
                  type="text"
                  value={targetIP}
                  onChange={(e) => setTargetIP(e.target.value)}
                  placeholder="10.0.0.1"
                  className="h-9 text-sm"
                />
              </Field>
              <Field label="Target Port">
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={targetPort}
                  onChange={(e) => setTargetPort(parseInt(e.target.value) || 80)}
                  className="h-9 text-sm"
                />
              </Field>
            </div>
          )}

          {updateMutation.isError && (
            <p className="text-xs text-destructive">
              {(updateMutation.error as Error)?.message ?? "Failed to update route"}
            </p>
          )}

          <Button
            size="sm"
            className="gap-1.5"
            onClick={() => updateMutation.mutate()}
            disabled={updateMutation.isPending}
          >
            {updateMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Save changes
          </Button>
        </div>
      </Section>

      {/* Danger zone */}
      <Section title="Danger zone" subtitle="Permanent actions that cannot be undone." danger>
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium">Delete route</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Removes this routing rule. Traffic to <span className="font-mono">{route.hostname}</span> will stop being proxied.
            </p>
          </div>
          {!confirmDelete ? (
            <Button variant="destructive" size="sm" className="shrink-0 gap-1.5" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="h-3.5 w-3.5" />Delete
            </Button>
          ) : (
            <div className="flex items-center gap-2 shrink-0">
              <Button variant="outline" size="sm" onClick={() => setConfirmDelete(false)} disabled={deleteMutation.isPending}>
                Cancel
              </Button>
              <Button variant="destructive" size="sm" className="gap-1.5" onClick={() => deleteMutation.mutate()} disabled={deleteMutation.isPending}>
                {deleteMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                Confirm delete
              </Button>
            </div>
          )}
        </div>
      </Section>
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
