import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CornerDownRight, ExternalLink, Globe, Loader2, Pencil, Plus, ServerCrash, Trash2, X } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  routes as routesApi,
  services as servicesApi,
  nodes as nodesApi,
  type ApiRouteTarget,
  type TargetBody,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, inputCls } from "@/components/services/form-primitives"
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

type TargetMode = "service" | "node" | "redirect"

interface TargetFormState {
  mode: TargetMode
  serviceId: string
  servicePortId: string  // "" = primary port (auto)
  nodeId: string
  port: string
  path: string
  stripPath: boolean
  redirectRouteId: string
  redirectCode: number
}

const BLANK_FORM: TargetFormState = {
  mode: "service",
  serviceId: "",
  servicePortId: "",
  nodeId: "",
  port: "",
  path: "/",
  stripPath: false,
  redirectRouteId: "",
  redirectCode: 301,
}

interface AddForm extends TargetFormState {
  open: boolean
}

const INITIAL_ADD: AddForm = { open: false, ...BLANK_FORM }

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

  const { data: allRoutes = [] } = useQuery({
    queryKey: ["routes", orgId, projectId],
    queryFn: () => routesApi.list(orgId!, projectId, token),
    enabled: !!orgId,
  })
  // Other public/preview routes in same project — valid redirect targets
  const redirectableRoutes = allRoutes.filter(
    (r) => r.id !== routeId && r.zone !== "internal"
  )

  const serviceMap = Object.fromEntries(allServices.map((s) => [s.id, s.name]))
  const nodeMap    = Object.fromEntries(rawNodes.map((n) => [n.id, n.name]))
  const routeMap   = Object.fromEntries(allRoutes.map((r) => [r.id, r.hostname]))

  const invalidateRoute = () =>
    queryClient.invalidateQueries({ queryKey: ["route", orgId, projectId, routeId] })

  const addTargetMutation = useMutation({
    mutationFn: () => {
      const body: TargetBody = { path: add.path || "/", strip_path: add.stripPath }
      if (add.mode === "service") {
        body.service_id = add.serviceId
        if (add.servicePortId) body.service_port_id = add.servicePortId
      } else if (add.mode === "node") {
        body.node_id = add.nodeId; body.port = parseInt(add.port, 10)
      } else if (add.mode === "redirect") {
        body.redirect_route_id = add.redirectRouteId; body.redirect_code = add.redirectCode
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
    add.path.trim().startsWith("/") && (
      add.mode === "service"  ? add.serviceId.length > 0 :
      add.mode === "node"     ? add.nodeId.length > 0 && add.port.trim().length > 0 :
      /* redirect */            add.redirectRouteId.length > 0
    )

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
                zone={route.zone}
                serviceMap={serviceMap}
                nodeMap={nodeMap}
                routeMap={routeMap}
                serviceList={serviceList}
                nodeList={nodeList}
                redirectableRoutes={redirectableRoutes}
                orgId={orgId!}
                projectId={projectId}
                routeId={routeId}
                token={token}
                deletePending={deleteTargetMutation.isPending}
                onDelete={() => deleteTargetMutation.mutate(target.id)}
                onUpdated={invalidateRoute}
              />
            ))}

            {add.open && (
              <TargetForm
                form={add}
                zone={route.zone}
                serviceList={serviceList}
                nodeList={nodeList}
                redirectableRoutes={redirectableRoutes}
                onChange={patchAdd}
                submitLabel="Add target"
                isPending={addTargetMutation.isPending}
                error={(addTargetMutation.error as Error | null)?.message}
                onSubmit={() => addTargetMutation.mutate()}
                onCancel={() => setAdd(INITIAL_ADD)}
                cancelIcon="trash"
              />
            )}

            {!add.open && (
              <Button
                variant="ghost"
                onClick={() => patchAdd({ open: true })}
                className="w-full flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground hover:text-foreground border border-dashed border-border/60 rounded-md hover:border-border transition-colors"
              >
                <Plus className="h-3 w-3" />
                Add another target
              </Button>
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

// ─── Shared target form ───────────────────────────────────────────────────────

function TargetForm({
  form,
  zone,
  serviceList,
  nodeList,
  redirectableRoutes,
  onChange,
  submitLabel,
  isPending,
  error,
  onSubmit,
  onCancel,
  cancelIcon = "x",
}: {
  form: TargetFormState
  zone: string
  serviceList: { id: string; name: string; ports?: { id: string; name: string; port: number; is_http: boolean; is_primary: boolean; is_public: boolean }[] }[]
  nodeList: { id: string; name: string; tailscale_ip?: string }[]
  redirectableRoutes: { id: string; hostname: string; zone: string }[]
  onChange: (patch: Partial<TargetFormState>) => void
  submitLabel: string
  isPending: boolean
  error?: string
  onSubmit: () => void
  onCancel: () => void
  cancelIcon?: "trash" | "x"
}) {
  const valid =
    form.path.trim().startsWith("/") && (
      form.mode === "service"  ? form.serviceId.length > 0 :
      form.mode === "node"     ? form.nodeId.length > 0 && form.port.trim().length > 0 :
      /* redirect */             form.redirectRouteId.length > 0
    )

  return (
    <div className="rounded-md border border-border/60 bg-muted/10 p-3 space-y-3">
      <div className="flex items-center gap-2">
        <SegmentedControl
          value={form.mode}
          onValueChange={(v) => onChange({ mode: v as TargetMode, serviceId: "", nodeId: "", port: "", redirectRouteId: "" })}
          options={[
            { value: "service",  label: "Service" },
            { value: "node",     label: "Node + port" },
            ...(zone !== "internal" ? [{ value: "redirect", label: "Redirect" }] : []),
          ]}
          className="text-xs shrink-0"
        />
        <input
          value={form.path}
          onChange={(e) => onChange({ path: e.target.value })}
          placeholder="/path"
          className={cn(inputCls, "font-mono text-xs w-28 shrink-0")}
        />
        {form.mode !== "redirect" && (
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground select-none cursor-pointer ml-auto shrink-0">
            <input
              type="checkbox"
              checked={form.stripPath}
              onChange={(e) => onChange({ stripPath: e.target.checked })}
              className="accent-primary"
            />
            Strip path
          </label>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onCancel}
          className="text-muted-foreground hover:text-destructive transition-colors shrink-0 ml-auto"
        >
          {cancelIcon === "trash" ? <Trash2 className="h-3.5 w-3.5" /> : <X className="h-3.5 w-3.5" />}
        </Button>
      </div>

      {form.mode === "service" ? (
        <div className="space-y-2">
          <Select
            value={form.serviceId}
            onValueChange={(v) => onChange({ serviceId: v ?? "", servicePortId: "" })}
          >
            <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
              <SelectValue placeholder={serviceList.length === 0 ? "No services in this project" : "Select a service…"}>
                {serviceList.find((s) => s.id === form.serviceId)?.name}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {serviceList.map((s) => (
                <SelectItem key={s.id} value={s.id}>
                  {s.name}
                  {(s.ports?.find((p) => p.is_primary) ?? s.ports?.[0])?.port && (
                    <span className="ml-2 text-muted-foreground text-xs">:{(s.ports?.find((p) => p.is_primary) ?? s.ports?.[0])?.port}</span>
                  )}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {/* Port selector — only shown when the selected service has multiple public HTTP ports */}
          {(() => {
            const selected = serviceList.find((s) => s.id === form.serviceId)
            const publicHTTP = selected?.ports?.filter((p) => p.is_public && p.is_http) ?? []
            if (publicHTTP.length <= 1) return null
            return (
              <Select
                value={form.servicePortId || "__primary__"}
                onValueChange={(v) => onChange({ servicePortId: !v || v === "__primary__" ? "" : v })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
                  <SelectValue placeholder="Port (primary)">
                    {form.servicePortId
                      ? (() => { const p = publicHTTP.find((p) => p.id === form.servicePortId); return p ? `${p.name} :${p.port}` : "Port (primary)" })()
                      : "Port (primary)"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__primary__">
                    <span className="text-muted-foreground">Primary port</span>
                  </SelectItem>
                  {publicHTTP.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                      <span className="ml-2 text-muted-foreground text-xs">:{p.port}</span>
                      {p.is_primary && <span className="ml-1 text-[10px] text-primary">primary</span>}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )
          })()}
        </div>
      ) : form.mode === "node" ? (
        <div className="grid grid-cols-[1fr_100px] gap-2">
          <Select value={form.nodeId} onValueChange={(v) => onChange({ nodeId: v ?? "" })}>
            <SelectTrigger className="w-full! h-9 text-sm bg-background border-border/60">
              <SelectValue placeholder={nodeList.length === 0 ? "No online nodes" : "Select a node…"}>
                {nodeList.find((n) => n.id === form.nodeId)?.name}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {nodeList.map((n) => (
                <SelectItem key={n.id} value={n.id}>
                  {n.name}
                  {n.tailscale_ip && <span className="ml-2 text-muted-foreground text-xs">{n.tailscale_ip}</span>}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            type="number"
            min={1}
            max={65535}
            value={form.port}
            onChange={(e) => onChange({ port: e.target.value })}
            placeholder="8080"
          />
        </div>
      ) : (
        <div className="flex items-center gap-2">
          <Select value={form.redirectRouteId} onValueChange={(v) => onChange({ redirectRouteId: v ?? "" })}>
            <SelectTrigger className="flex-1 h-9 text-sm bg-background border-border/60">
              <SelectValue placeholder={redirectableRoutes.length === 0 ? "No other routes available" : "Select target route…"}>
                {redirectableRoutes.find((r) => r.id === form.redirectRouteId)?.hostname}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {redirectableRoutes.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  <span className="font-mono text-xs">{r.hostname}</span>
                  <span className="ml-2 text-muted-foreground text-xs">{r.zone}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <SegmentedControl
            value={String(form.redirectCode)}
            onValueChange={(v) => onChange({ redirectCode: Number(v) })}
            options={[
              { value: "301", label: "301" },
              { value: "302", label: "302" },
            ]}
            className="text-xs shrink-0"
          />
        </div>
      )}

      {error && <p className="text-xs text-destructive">{error}</p>}

      <Button
        size="sm"
        className="gap-1.5"
        onClick={onSubmit}
        disabled={!valid || isPending}
      >
        {isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
        {submitLabel}
      </Button>
    </div>
  )
}

// ─── Target item (view + inline edit) ────────────────────────────────────────

function targetToForm(target: ApiRouteTarget): TargetFormState {
  const mode: TargetMode = target.redirect_route_id ? "redirect" : target.node_id ? "node" : "service"
  return {
    mode,
    serviceId: target.service_id ?? "",
    servicePortId: "",  // not stored on the target; user can re-select on edit
    nodeId: target.node_id ?? "",
    port: target.target_port ? String(target.target_port) : "",
    path: target.path,
    stripPath: target.strip_path ?? false,
    redirectRouteId: target.redirect_route_id ?? "",
    redirectCode: target.redirect_code ?? 301,
  }
}

function TargetItem({
  target,
  hostname,
  zone,
  serviceMap,
  nodeMap,
  routeMap,
  serviceList,
  nodeList,
  redirectableRoutes,
  orgId,
  projectId,
  routeId,
  token,
  deletePending,
  onDelete,
  onUpdated,
}: {
  target: ApiRouteTarget
  hostname: string
  zone: string
  serviceMap: Record<string, string>
  nodeMap: Record<string, string>
  routeMap: Record<string, string>
  serviceList: { id: string; name: string; ports?: { id: string; name: string; port: number; is_http: boolean; is_primary: boolean; is_public: boolean }[] }[]
  nodeList: { id: string; name: string; tailscale_ip?: string }[]
  redirectableRoutes: { id: string; hostname: string; zone: string }[]
  orgId: string
  projectId: string
  routeId: string
  token: string
  deletePending: boolean
  onDelete: () => void
  onUpdated: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [editForm, setEditForm] = useState<TargetFormState>(() => targetToForm(target))
  const patchEdit = (p: Partial<TargetFormState>) => setEditForm((s) => ({ ...s, ...p }))

  const updateMutation = useMutation({
    mutationFn: () => {
      const body: TargetBody = { path: editForm.path || "/", strip_path: editForm.stripPath }
      if (editForm.mode === "service") {
        body.service_id = editForm.serviceId
        if (editForm.servicePortId) body.service_port_id = editForm.servicePortId
      } else if (editForm.mode === "node") {
        body.node_id = editForm.nodeId; body.port = parseInt(editForm.port, 10)
      } else if (editForm.mode === "redirect") {
        body.redirect_route_id = editForm.redirectRouteId; body.redirect_code = editForm.redirectCode
      }
      return routesApi.updateTarget(orgId, projectId, routeId, target.id, body, token)
    },
    onSuccess: () => { setEditing(false); onUpdated() },
  })

  const isRedirect = !!target.redirect_route_id
  const targetLabel = isRedirect
    ? routeMap[target.redirect_route_id!] ?? "Unknown route"
    : target.service_id
    ? serviceMap[target.service_id] ?? "Unknown service"
    : target.node_id
    ? `${nodeMap[target.node_id] ?? "Unknown node"} :${target.target_port}`
    : `${target.target_ip}:${target.target_port}`

  const openHref = `https://${hostname}${target.path === "/" ? "" : target.path}`

  if (editing) {
    return (
      <TargetForm
        form={editForm}
        zone={zone}
        serviceList={serviceList}
        nodeList={nodeList}
        redirectableRoutes={redirectableRoutes}
        onChange={patchEdit}
        submitLabel="Save"
        isPending={updateMutation.isPending}
        error={(updateMutation.error as Error | null)?.message}
        onSubmit={() => updateMutation.mutate()}
        onCancel={() => { setEditing(false); setEditForm(targetToForm(target)) }}
      />
    )
  }

  return (
    <div className="rounded-md border border-border/60 bg-muted/5 px-3 py-2.5 space-y-1.5">
      <div className="flex items-center gap-3">
        <code className="text-xs font-mono bg-muted/50 border border-border/40 px-1.5 py-0.5 rounded text-muted-foreground shrink-0">
          {target.path}
        </code>
        <span className="text-muted-foreground text-xs shrink-0">→</span>
        {isRedirect && (
          <CornerDownRight className="h-3 w-3 text-amber-400/70 shrink-0" />
        )}
        <span className="text-xs text-foreground flex-1 truncate font-mono">{targetLabel}</span>
        {isRedirect && (
          <Badge className="text-[9px] px-1 py-0 h-4 border bg-amber-500/10 text-amber-400 border-amber-500/20 shrink-0">
            {target.redirect_code}
          </Badge>
        )}
        {target.strip_path && !isRedirect && (
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
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => setEditing(true)}
          title="Edit target"
          className="text-muted-foreground/50 hover:text-foreground transition-colors shrink-0"
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDelete}
          disabled={deletePending}
          className="text-muted-foreground/50 hover:text-destructive transition-colors shrink-0 disabled:opacity-40"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
      {!isRedirect && target.target_ip && (
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
