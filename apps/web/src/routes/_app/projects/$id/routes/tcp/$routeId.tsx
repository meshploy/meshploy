import { useEffect, useMemo, useState } from "react"
import { createFileRoute, Link, useNavigate, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, Copy, Loader2, Network, ServerCrash, Trash2, Zap } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DetailPageHeader } from "@/components/layout/detail-page-header"
import { ResourcePanel, ResourceFact } from "@/components/layout/resource-workbench"
import { ConfigSaveBar, useConfigDraft, useConfigSave } from "@/components/layout/config-save-bar"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { PublishStateBadge, PublishToggle } from "@/components/routes/publish-toggle"
import { FirewallHint } from "@/components/routes/firewall-hint"
import {
  tcpRoutes as tcpRoutesApi,
  services as servicesApi,
  nodes as nodesApi,
  orgs as orgsApi,
  domains as domainsApi,
  type ApiDatabaseConfig,
  type ApiTCPRoute,
} from "@/lib/api"
import { cn, formatRelativeTime } from "@/lib/utils"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/projects/$id/routes/tcp/$routeId")({
  component: TCPRouteDetailPage,
})

const STATE: Record<string, { label: string; cls: string }> = {
  open:    { label: "listening", cls: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20" },
  pending: { label: "opening…",  cls: "bg-muted text-muted-foreground border-border" },
  failed:  { label: "failed",    cls: "bg-destructive/10 text-destructive border-destructive/20" },
  paused:  { label: "closed",    cls: "bg-muted text-muted-foreground border-border" },
}

function TCPRouteDetailPage() {
  const { id: projectId, routeId } = useParams({ from: "/_app/projects/$id/routes/tcp/$routeId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)

  const { data: route, isLoading, isError } = useQuery({
    queryKey: ["tcp-route", orgId, projectId, routeId],
    queryFn: () => tcpRoutesApi.get(orgId, projectId, routeId, token),
    enabled: !!orgId,
    // The gateway reports back within 30 seconds of a change.
    refetchInterval: (q) => {
      const r = q.state.data
      return r && (r.status === "pending" || (!r.published && r.status !== "paused")) ? 5000 : false
    },
  })
  const { data: nodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId, token),
    enabled: !!orgId,
  })
  const { data: services = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId && !!route?.service_id,
  })
  const { data: members = [] } = useQuery({
    queryKey: ["org-members", orgId],
    queryFn: () => orgsApi.listMembers(orgId, token),
    enabled: !!orgId && !!route?.published_changed_by,
  })

  const deleteMutation = useMutation({
    mutationFn: () => tcpRoutesApi.remove(orgId, projectId, routeId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["tcp-routes", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["tcp-port-usage", orgId] })
      navigate({ to: "/projects/$id/routes", params: { id: projectId } })
    },
  })

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
        <p className="text-xs">TCP route not found</p>
      </div>
    )
  }

  const gateway = nodes.find((n) => n.k3s_role === "server")
  const service = services.find((s) => s.id === route.service_id)
  const node = nodes.find((n) => n.id === route.node_id)
  const target = route.service_id
    ? service?.name ?? "a service"
    : `${node?.name ?? route.target_ip}:${route.target_port}`
  const changedBy = members.find((m) => m.user_id === route.published_changed_by)
  const state = !route.published && route.status !== "paused"
    ? { label: "closing…", cls: STATE.pending.cls }
    : STATE[route.status] ?? STATE.pending

  return (
    <div className="flex flex-col min-h-full">
      <DetailPageHeader
        backTo="/projects/$id/routes"
        backLabel="Back to routes"
        backParams={{ id: projectId }}
        icon={<Network className="h-4 w-4 text-muted-foreground" />}
        name={`:${route.gateway_port}`}
        nameClassName="font-mono"
        badge={
          <>
            <PublishStateBadge published={route.published} className="h-4 shrink-0" />
            <Badge className={cn("text-[11px] px-1.5 py-0 h-4 border shrink-0", state.cls)}>{state.label}</Badge>
          </>
        }
        subtitle={`Forwards to ${target}`}
        actions={<PublishToggle kind="tcp" routeId={route.id} projectId={projectId} published={route.published} label={`:${route.gateway_port}`} />}
      />
      <ConfigSaveBar key={route.id}>
        <div className="console-page space-y-6 pb-24">
          <div className="resource-overview-columns">
            <div className="min-w-0 space-y-6">
              <ConnectSection route={route} gatewayIP={gateway?.public_ip} projectId={projectId} />
              <SettingsSection route={route} projectId={projectId} />
            </div>
            <aside className="space-y-6">
              <ResourcePanel title="Route details">
                <ResourceFact label="Gateway port"><code>{route.gateway_port}</code></ResourceFact>
                <ResourceFact label="Target">
                  {route.service_id ? (
                    <Link to="/projects/$id/services/$serviceId/config" params={{ id: projectId, serviceId: route.service_id }} className="hover:text-primary">
                      {target}
                    </Link>
                  ) : target}
                </ResourceFact>
                <ResourceFact label="Forwards to"><code>{route.target_ip}:{route.target_port}</code></ResourceFact>
                <ResourceFact label="Created">{new Date(route.created_at).toLocaleDateString()}</ResourceFact>
                {route.published_changed_at && (
                  <ResourceFact label={route.published ? "Published" : "Paused"}>
                    {formatRelativeTime(new Date(route.published_changed_at))}
                    {changedBy && ` by ${changedBy.user_name || changedBy.user_email}`}
                  </ResourceFact>
                )}
              </ResourcePanel>
            </aside>
          </div>

          <Section title="Danger zone" subtitle="Permanent actions that cannot be undone." danger>
            <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium">Delete TCP route</p>
                <p className="text-xs text-muted-foreground mt-0.5">
                  The gateway stops listening on <span className="font-mono">:{route.gateway_port}</span> and the port
                  becomes free. To close it for now and keep the route, pause it instead.
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
            {deleteMutation.isError && <p className="text-xs text-destructive">{(deleteMutation.error as Error).message}</p>}
          </Section>
        </div>
      </ConfigSaveBar>
    </div>
  )
}

/**
 * A URL a driver or GUI client accepts. The password goes in the copied text
 * and is masked on screen.
 */
function connectionString(dc: ApiDatabaseConfig, host: string, port: number, masked: boolean) {
  const secret = !dc.db_password ? "" : `:${masked ? "••••••••" : encodeURIComponent(dc.db_password)}`
  const auth = `${encodeURIComponent(dc.db_user)}${secret}@${host}:${port}`
  switch (dc.engine) {
    case "postgres":   return `postgresql://${auth}/${dc.db_name}`
    case "mysql":      return `mysql://${auth}/${dc.db_name}`
    case "redis":
    case "dragonfly":  return `redis://${auth}`
    case "mongodb":    return `mongodb://${auth}/${dc.db_name}?authSource=admin`
    case "clickhouse": return `clickhouse://${auth}/${dc.db_name}`
  }
}

/** Where to point a client, and for a database the command and URL that do it. */
function ConnectSection({ route, gatewayIP, projectId }: { route: ApiTCPRoute; gatewayIP?: string; projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const { data: services = [] } = useQuery({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId && !!route.service_id,
  })
  const isDatabase = services.find((s) => s.id === route.service_id)?.type === "database"
  const { data: dc } = useQuery({
    queryKey: ["database-config", orgId, projectId, route.service_id],
    queryFn: () => servicesApi.getDatabaseConfig(orgId, projectId, route.service_id!, token),
    enabled: !!orgId && isDatabase,
  })
  // A name outlives the gateway's address. The port does the routing, so the
  // base domain itself is enough: install points it, and everything under it,
  // at the gateway.
  const { data: domain } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
    select: (list) => list.find((d) => d.verified)?.base_domain,
  })

  const host = domain || gatewayIP || "the gateway"
  const port = route.gateway_port
  const address = `${host}:${port}`
  // The password is left for the client to ask: a command pasted into a shell
  // lands in its history.
  const command = !dc ? null : {
    postgres: `psql -h ${host} -p ${port} -U ${dc.db_user} ${dc.db_name}`,
    mysql: `mysql -h ${host} -P ${port} -u ${dc.db_user} -p ${dc.db_name}`,
    redis: `redis-cli -h ${host} -p ${port} --askpass`,
    dragonfly: `redis-cli -h ${host} -p ${port} --askpass`,
    mongodb: `mongosh "mongodb://${dc.db_user}@${address}/${dc.db_name}?authSource=admin"`,
    clickhouse: `clickhouse-client --host ${host} --port ${port} --user ${dc.db_user} --ask-password`,
  }[dc.engine]

  return (
    <Section title="Connect" subtitle="Where clients reach this port.">
      <div className="space-y-3">
        <CopyRow label="Address" value={address} />
        {dc && (
          <CopyRow
            label="URL"
            value={connectionString(dc, host, port, false)}
            display={connectionString(dc, host, port, true)}
            copyLabel="Copy with the password"
          />
        )}
        {command && <CopyRow label="Command" value={command} />}
        {domain && gatewayIP && (
          <p className="text-xs text-muted-foreground">
            Or by address, <code className="font-mono">{gatewayIP}:{port}</code>, for a client that cannot resolve {domain}.
          </p>
        )}
        {!route.published ? (
          <p className="text-xs text-muted-foreground">Paused: the gateway refuses connections until the route is published.</p>
        ) : route.status === "failed" ? (
          <p className="text-xs text-destructive">The gateway could not open the port: {route.last_error}</p>
        ) : route.status === "pending" ? (
          <p className="text-xs text-muted-foreground">The gateway opens the port within 30 seconds.</p>
        ) : route.allowed_cidrs.length === 0 ? (
          <p className="text-xs text-amber-400">Open to anyone who can reach the gateway. Limit it under Allow from.</p>
        ) : null}
        {route.published && route.status !== "failed" && <FirewallHint port={port} verdict={route.host_firewall} />}
      </div>
    </Section>
  )
}

function CopyRow({ label, value, display, copyLabel }: {
  label: string
  value: string
  /** What is shown, when it differs from what is copied. */
  display?: string
  copyLabel?: string
}) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-3">
      <span className="w-20 shrink-0 text-xs text-muted-foreground">{label}</span>
      <code className="flex-1 min-w-0 truncate rounded-md border border-border/60 bg-muted/30 px-2.5 py-1.5 text-xs font-mono">{display ?? value}</code>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={copyLabel ?? `Copy ${label.toLowerCase()}`}
        title={copyLabel}
        onClick={() => {
          navigator.clipboard?.writeText(value).then(() => {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          })
        }}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </Button>
    </div>
  )
}

/** The gateway port and who may connect, saved through the page's save bar. */
function SettingsSection({ route, projectId }: { route: ApiTCPRoute; projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const draft = useConfigDraft({ gatewayPort: String(route.gateway_port), allowFrom: route.allowed_cidrs.join(", ") })
  const { value, setValue } = draft

  useEffect(() => {
    draft.sync({ gatewayPort: String(route.gateway_port), allowFrom: route.allowed_cidrs.join(", ") })
  }, [route.gateway_port, route.allowed_cidrs.join(",")]) // eslint-disable-line react-hooks/exhaustive-deps

  // Across the whole org: a port another project holds is just as taken.
  const { data: usage } = useQuery({
    queryKey: ["tcp-port-usage", orgId],
    queryFn: () => tcpRoutesApi.usage(orgId, token),
    enabled: !!orgId,
  })
  const inUse = useMemo(() => {
    const taken = new Map<number, string>()
    for (const p of usage?.reserved ?? []) taken.set(p, "the gateway uses it itself")
    for (const r of usage?.routes ?? []) {
      if (r.id !== route.id) taken.set(r.gateway_port, r.project_id === projectId ? "another route in this project has it" : "another project has it")
    }
    return taken
  }, [usage, route.id, projectId])

  const port = Number(value.gatewayPort)
  const portError = (() => {
    if (!value.gatewayPort) return "A gateway port is required."
    if (!(port > 0 && port < 65536)) return "A port is between 1 and 65535."
    const why = inUse.get(port)
    return why ? `Port ${port} is not free: ${why}.` : null
  })()

  const findFreePort = () => {
    for (let p = 10000; p < 65536; p++) {
      if (!inUse.has(p)) return setValue((v) => ({ ...v, gatewayPort: String(p) }))
    }
  }

  const save = useMutation({
    mutationFn: async () => {
      if (portError) throw new Error(portError)
      return tcpRoutesApi.update(orgId, projectId, route.id, {
        gateway_port: port,
        allowed_cidrs: value.allowFrom.split(/[\s,]+/).filter(Boolean),
      }, token)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["tcp-route", orgId, projectId, route.id] })
      qc.invalidateQueries({ queryKey: ["tcp-routes", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["tcp-port-usage", orgId] })
    },
  })
  useConfigSave("TCP route", draft, () => save.mutateAsync())

  return (
    <Section title="Settings" subtitle="Changing the port moves the listener; clients have to use the new one.">
      <Field label="Gateway port">
        <div className="flex items-center gap-2">
          <input
            className={cn(inputCls, "flex-1 min-w-0")}
            value={value.gatewayPort}
            inputMode="numeric"
            onChange={(e) => setValue((v) => ({ ...v, gatewayPort: e.target.value.replace(/[^0-9]/g, "") }))}
          />
          <Button variant="outline" className="shrink-0 gap-1.5" onClick={findFreePort}>
            <Zap className="h-3.5 w-3.5" />
            Find a free port
          </Button>
        </div>
        {draft.dirty && portError ? (
          <p className="text-xs text-destructive mt-1.5">{portError}</p>
        ) : (
          <p className="text-xs text-muted-foreground mt-1.5">
            It cannot be one the gateway uses for itself, and the host firewall must allow it too.
          </p>
        )}
      </Field>
      <Field label="Allow from">
        <input
          className={inputCls}
          value={value.allowFrom}
          placeholder="Anyone. e.g. 203.0.113.7, 10.0.0.0/8"
          onChange={(e) => setValue((v) => ({ ...v, allowFrom: e.target.value }))}
        />
        <p className="text-xs text-muted-foreground mt-1.5">
          Addresses or ranges, separated by commas. Left empty the port is open to the internet.
        </p>
      </Field>
    </Section>
  )
}
