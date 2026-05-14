import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ExternalLink, Globe, Loader2, Plus, ServerCrash, Trash2 } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  routes as routesApi,
  services as servicesApi,
  nodes as nodesApi,
  type ApiDbRoute,
  type ApiRouteTarget,
  type TargetBody,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DetailPageHeader } from "@/components/layout/detail-page-header"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/routes/$routeId")({
  component: RouteDetailPage,
})

const ZONE_STYLES: Record<string, string> = {
  public:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  internal: "bg-blue-500/10 text-blue-400 border-blue-500/20",
  preview:  "bg-violet-500/10 text-violet-400 border-violet-500/20",
}

type TargetMode = "service" | "node"

interface AddForm {
  open: boolean
  mode: TargetMode
  serviceId: string
  nodeId: string
  port: string
  path: string
  stripPath: boolean
}

const INITIAL_ADD: AddForm = {
  open: false,
  mode: "service",
  serviceId: "",
  nodeId: "",
  port: "",
  path: "/",
  stripPath: false,
}

function RouteDetailPage() {
  const { id: projectId, routeId } = useParams({ from: "/_app/projects/$id/routes/$routeId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [add, setAdd] = useState<AddForm>(INITIAL_ADD)
  const patchAdd = (p: Partial<AddForm>) => setAdd((s) => ({ ...s, ...p }))

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

  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })
  const nodeList = rawNodes.filter((n) => n.status === "online")

  const serviceMap = Object.fromEntries(allServices.map((s) => [s.id, s.name]))
  const nodeMap = Object.fromEntries(rawNodes.map((n) => [n.id, n.name]))

  const invalidateRoute = () =>
    queryClient.invalidateQueries({ queryKey: ["route", orgId, projectId, routeId] })

  const addTargetMutation = useMutation({
    mutationFn: () => {
      const body: TargetBody = {
        path: add.path || "/",
        strip_path: add.stripPath,
        ...(add.mode === "service"
          ? { service_id: add.serviceId }
          : { node_id: add.nodeId, port: parseInt(add.port, 10) }),
      }
      return routesApi.addTarget(orgId!, projectId, routeId, body, token)
    },
    onSuccess: () => {
      setAdd(INITIAL_ADD)
      invalidateRoute()
      queryClient.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
    },
  })

  const deleteTargetMutation = useMutation({
    mutationFn: (targetId: string) =>
      routesApi.deleteTarget(orgId!, projectId, routeId, targetId, token),
    onSuccess: () => {
      invalidateRoute()
      queryClient.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => routesApi.delete(orgId!, projectId, routeId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["routes", orgId, projectId] })
      navigate({ to: "/projects/$id/routes", params: { id: projectId } })
    },
  })

  const addValid =
    add.path.trim().startsWith("/") &&
    (add.mode === "service" ? add.serviceId.length > 0 : add.nodeId.length > 0 && add.port.trim().length > 0)

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

  const targetCount = route.targets.length

  return (
    <div className="flex flex-col min-h-full">
      <DetailPageHeader
        backTo="/projects/$id/routes"
        backLabel="Back to routes"
        backParams={{ id: projectId }}
        icon={<Globe className="h-4 w-4 text-muted-foreground" />}
        name={route.hostname}
        nameClassName="font-mono"
        badge={
          <>
            <Badge className={`text-[10px] px-1.5 py-0 h-4 border shrink-0 ${ZONE_STYLES[route.zone] ?? ""}`}>
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
          </>
        }
        subtitle={`${targetCount} path rule${targetCount !== 1 ? "s" : ""}`}
      />
      <div className="p-6 space-y-6 max-w-2xl">

        {/* Details */}
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          <DetailRow label="Hostname" value={route.hostname} mono />
          <DetailRow label="Zone" value={route.zone} />
          {route.subdomain && <DetailRow label="Subdomain" value={route.subdomain} mono />}
          <DetailRow label="Created" value={new Date(route.created_at).toLocaleString()} />
        </div>

        {/* Targets */}
        <Section title="Targets" subtitle="Path rules for this hostname, matched longest-first.">
          <div className="space-y-2">
            {route.targets.length === 0 && !add.open && (
              <p className="text-xs text-muted-foreground/60 py-2">No targets configured.</p>
            )}
            {route.targets.map((target) => (
              <TargetItem
                key={target.id}
                target={target}
                hostname={route.hostname}
                serviceMap={serviceMap}
                nodeMap={nodeMap}
                deletePending={deleteTargetMutation.isPending}
                onDelete={() => deleteTargetMutation.mutate(target.id)}
              />
            ))}

            {add.open && (
              <div className="rounded-md border border-border/60 bg-muted/10 p-3 space-y-3">
                <div className="flex items-center gap-2">
                  <SegmentedControl
                    value={add.mode}
                    onValueChange={(v) => patchAdd({ mode: v as TargetMode, serviceId: "", nodeId: "", port: "" })}
                    options={[
                      { value: "service", label: "Service" },
                      { value: "node", label: "Node + port" },
                    ]}
                    className="text-xs shrink-0"
                  />
                  <input
                    value={add.path}
                    onChange={(e) => patchAdd({ path: e.target.value })}
                    placeholder="/path"
                    className={cn(inputCls, "font-mono text-xs w-28 shrink-0")}
                  />
                  <label className="flex items-center gap-1.5 text-xs text-muted-foreground select-none cursor-pointer ml-auto shrink-0">
                    <input
                      type="checkbox"
                      checked={add.stripPath}
                      onChange={(e) => patchAdd({ stripPath: e.target.checked })}
                      className="accent-primary"
                    />
                    Strip path
                  </label>
                  <button
                    type="button"
                    onClick={() => setAdd(INITIAL_ADD)}
                    className="text-muted-foreground hover:text-destructive transition-colors shrink-0"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>

                {add.mode === "service" ? (
                  <Select value={add.serviceId} onValueChange={(v) => patchAdd({ serviceId: v ?? "" })}>
                    <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
                      <SelectValue placeholder={serviceList.length === 0 ? "No services in this project" : "Select a service…"}>
                        {serviceList.find((s) => s.id === add.serviceId)?.name}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {serviceList.map((s) => (
                        <SelectItem key={s.id} value={s.id}>
                          {s.name}
                          <span className="ml-2 text-muted-foreground text-xs">:{s.port}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <div className="grid grid-cols-[1fr_100px] gap-2">
                    <Select value={add.nodeId} onValueChange={(v) => patchAdd({ nodeId: v ?? "" })}>
                      <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
                        <SelectValue placeholder={nodeList.length === 0 ? "No online nodes" : "Select a node…"}>
                          {nodeList.find((n) => n.id === add.nodeId)?.name}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {nodeList.map((n) => (
                          <SelectItem key={n.id} value={n.id}>
                            {n.name}
                            <span className="ml-2 text-muted-foreground text-xs">{n.tailscale_ip}</span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Input
                      type="number"
                      min={1}
                      max={65535}
                      value={add.port}
                      onChange={(e) => patchAdd({ port: e.target.value })}
                      placeholder="8080"
                    />
                  </div>
                )}

                {addTargetMutation.isError && (
                  <p className="text-xs text-destructive">
                    {(addTargetMutation.error as Error)?.message ?? "Failed to add target"}
                  </p>
                )}

                <Button
                  size="sm"
                  className="gap-1.5"
                  onClick={() => addTargetMutation.mutate()}
                  disabled={!addValid || addTargetMutation.isPending}
                >
                  {addTargetMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  Add target
                </Button>
              </div>
            )}

            {!add.open && (
              <button
                type="button"
                onClick={() => patchAdd({ open: true })}
                className="w-full flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground hover:text-foreground border border-dashed border-border/60 rounded-md hover:border-border transition-colors"
              >
                <Plus className="h-3 w-3" />
                Add another target
              </button>
            )}
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
    </div>
  )
}

function TargetItem({
  target,
  hostname,
  serviceMap,
  nodeMap,
  deletePending,
  onDelete,
}: {
  target: ApiRouteTarget
  hostname: string
  serviceMap: Record<string, string>
  nodeMap: Record<string, string>
  deletePending: boolean
  onDelete: () => void
}) {
  const targetLabel = target.service_id
    ? serviceMap[target.service_id] ?? "Unknown service"
    : target.node_id
    ? `${nodeMap[target.node_id] ?? "Unknown node"} :${target.target_port}`
    : `${target.target_ip}:${target.target_port}`

  const openHref = `https://${hostname}${target.path === "/" ? "" : target.path}`

  return (
    <div className="rounded-md border border-border/60 bg-muted/5 px-3 py-2.5 space-y-1.5">
      <div className="flex items-center gap-3">
        <code className="text-xs font-mono bg-muted/50 border border-border/40 px-1.5 py-0.5 rounded text-muted-foreground shrink-0">
          {target.path}
        </code>
        <span className="text-muted-foreground text-xs shrink-0">→</span>
        <span className="text-xs text-foreground flex-1 truncate">{targetLabel}</span>
        {target.strip_path && (
          <Badge className="text-[9px] px-1 py-0 h-4 border bg-muted/50 text-muted-foreground border-border/40 shrink-0">
            strip
          </Badge>
        )}
        <a
          href={openHref}
          target="_blank"
          rel="noopener noreferrer"
          title="Open in new tab"
          className="text-muted-foreground/50 hover:text-muted-foreground transition-colors shrink-0"
        >
          <ExternalLink className="h-3.5 w-3.5" />
        </a>
        <button
          type="button"
          onClick={onDelete}
          disabled={deletePending}
          className="text-muted-foreground/50 hover:text-destructive transition-colors shrink-0 disabled:opacity-40"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
      {target.target_ip && (
        <div className="flex items-center gap-1.5 pl-0.5">
          <code className="text-[10px] font-mono text-muted-foreground/50">
            {target.target_ip}:{target.target_port}
          </code>
        </div>
      )}
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
