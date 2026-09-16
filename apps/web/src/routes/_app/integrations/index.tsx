import type { ReactNode } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { createFileRoute, useSearch, useNavigate, Link } from "@tanstack/react-router"
import { createContext, useContext, useEffect, useState } from "react"
import { useQuery, useQueries, useMutation, useQueryClient } from "@tanstack/react-query"
import { Bell, Box, GitBranch, HardDrive, Loader2, Mail, Pencil, Plus, Trash2, Download, RefreshCw } from "lucide-react"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { EventPicker } from "@/components/notifications/event-picker"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import {
  gitIntegrations as gitApi,
  registries as registriesApi,
  storage as storageApi,
  notifications as notificationsApi,
  emailConfig as emailConfigApi,
  type ApiGitIntegration,
  type ApiRegistryIntegration,
  type ApiStorageIntegration,
  type ApiNotificationChannel,
  type ApiOrgEmailConfig,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

const PROVIDER_LABELS: Record<string, string> = {
  ghcr: "GitHub Container Registry",
  dockerhub: "Docker Hub",
  ecr: "Amazon ECR",
  gcr: "Google Container Registry",
  custom: "Private Registry",
  builtin: "Built-in Registry",
  s3: "Amazon S3",
  r2: "Cloudflare R2",
  minio: "MinIO",
  slack: "Slack",
  discord: "Discord",
  webhook: "Webhook",
  email: "Email",
  github: "GitHub",
  gitlab: "GitLab",
  gitea: "Gitea",
}

export const Route = createFileRoute("/_app/integrations/")({
  component: IntegrationsPage,
})

function IntegrationsPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const navigate = useNavigate()

  const search = useSearch({ strict: false }) as Record<string, string>
  useEffect(() => {
    if (search.github_setup === "done" || search.github === "connected" || search.gitlab === "connected" || search.gitea === "connected") {
      qc.invalidateQueries({ queryKey: ["git-integrations", orgId] })
      navigate({ to: "/integrations", replace: true })
    }
  }, [search.github_setup, search.github, search.gitlab, search.gitea])

  const { data: gitList = [], isLoading: gitLoading } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId,
  })

  const categories = [["git", "Git sources"], ["registries", "Registries"], ["storage", "Object storage"], ["notifications", "Notifications"], ["email", "Email provider"]]
  const countQueries = useQueries({ queries: [
    { queryKey: ["registry-integrations", orgId], queryFn: () => registriesApi.list(orgId, token), enabled: !!orgId },
    { queryKey: ["storage-integrations", orgId], queryFn: () => storageApi.list(orgId, token), enabled: !!orgId },
    { queryKey: ["notification-channels", orgId], queryFn: () => notificationsApi.list(orgId, token), enabled: !!orgId },
  ] })
  const emailCount = useQuery({ queryKey: ["email-config", orgId], queryFn: () => emailConfigApi.get(orgId, token), enabled: !!orgId })
  const counts = [gitLoading ? "…" : gitList.length, ...countQueries.map(q => q.isPending ? "…" : q.isError ? "-" : (q.data?.length ?? 0)), emailCount.isPending ? "…" : emailCount.data ? 1 : 0]

  const gitDeleteMutation = useMutation({
    mutationFn: (id: string) => gitApi.delete(orgId, id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["git-integrations", orgId] }),
  })

  return (
    <div className="console-page p-6 space-y-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Integrations</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Connect external services for source code, images, backups, and alerts
        </p>
      </div>

      <Tabs defaultValue="git" className="gap-6 min-w-0">
        <div className="overflow-x-auto border-b border-border">
          <TabsList variant="line" aria-label="Integration categories" className="detail-tabs console-panel-tabs">
            {categories.map(([id, label], index) => <TabsTrigger key={id} value={id} className="console-detail-tab">{label}<span className="text-xs text-muted-foreground tabular-nums">{counts[index]}</span></TabsTrigger>)}
          </TabsList>
        </div>
        <TabsContent value="git">
      {/* ── Git Sources ────────────────────────────────────────────────────── */}
      <section id="integration-git" className="integration-category space-y-4">
        <div className="flex items-center justify-between">
          <SectionHeader
            icon={<GitBranch className="h-4 w-4" />}
            title="Git Sources"
            description="Connect GitHub, GitLab, or Gitea to deploy from repositories"
          />
          <Link
            to="/integrations/new"
            search={{ category: "git" }}
            className={buttonVariants({ variant: "default", className: "h-9 px-3" })}
          >
            <Plus className="size-4" />Add source
          </Link>
        </div>

        {gitLoading ? (
          <LoadingRow />
        ) : gitList.length === 0 ? (
          <EmptyState icon={<GitBranch className="h-7 w-7" />} title="No git sources connected" description="Connect GitHub, GitLab, or Gitea to enable repository-based deployments">
            <Link
              to="/integrations/new"
              search={{ category: "git" }}
              className={buttonVariants({ variant: "default", className: "h-9 px-3" })}
            >
              <Plus className="size-4" />Add source
            </Link>
          </EmptyState>
        ) : (
          <IntegrationTable>
            {gitList.map((g) => (
              <GitIntegrationCard
                key={g.id}
                integration={g}
                orgId={orgId}
                token={token}
                onDelete={() => gitDeleteMutation.mutateAsync(g.id)}
                isDeleting={gitDeleteMutation.isPending && gitDeleteMutation.variables === g.id}
              />
            ))}
          </IntegrationTable>
        )}
      </section>

      </TabsContent>
      <TabsContent value="registries"><RegistrySection orgId={orgId} token={token} /></TabsContent>
      <TabsContent value="storage"><StorageSection orgId={orgId} token={token} /></TabsContent>
      <TabsContent value="notifications"><NotificationsSection orgId={orgId} token={token} /></TabsContent>
      <TabsContent value="email"><EmailProviderSection orgId={orgId} token={token} /></TabsContent>
      </Tabs>
    </div>
  )
}

// ─── Registry section ─────────────────────────────────────────────────────────

function RegistrySection({ orgId, token }: { orgId: string; token: string }) {
  const qc = useQueryClient()

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["registry-integrations", orgId],
    queryFn: () => registriesApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => registriesApi.delete(orgId, id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["registry-integrations", orgId] }),
  })

  return (
    <section id="integration-registries" className="integration-category space-y-4">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<Box className="h-4 w-4" />}
          title="Container Registries"
          description="Pull and push images from private and public registries"
        />
        <Link
          to="/integrations/new"
          search={{ category: "registry" }}
          className={buttonVariants({ variant: "default", className: "h-9 px-3" })}
        >
          <Plus className="size-4" />Add registry
        </Link>
      </div>

      {isLoading ? (
        <LoadingRow />
      ) : list.length === 0 ? (
        <EmptyState
          icon={<Box className="h-7 w-7" />}
          title="No registries connected"
          description="Add a registry to pull private images during deployments"
        >
          <Link
            to="/integrations/new"
            search={{ category: "registry" }}
            className={buttonVariants({ variant: "default", className: "h-9 px-3" })}
          >
            <Plus className="size-4" />Add registry
          </Link>
        </EmptyState>
      ) : (
        <IntegrationTable>
          {list.map((reg) => (
            <RegistryCard
              key={reg.id}
              registry={reg}
              onDelete={() => deleteMutation.mutateAsync(reg.id)}
              isDeleting={deleteMutation.isPending}
            />
          ))}
        </IntegrationTable>
      )}
    </section>
  )
}

// ─── Storage section ──────────────────────────────────────────────────────────

function StorageSection({ orgId, token }: { orgId: string; token: string }) {
  const qc = useQueryClient()

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["storage-integrations", orgId],
    queryFn: () => storageApi.list(orgId, token).then((r) => r ?? []),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => storageApi.delete(orgId, id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["storage-integrations", orgId] }),
  })

  return (
    <section id="integration-storage" className="integration-category space-y-4">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<HardDrive className="h-4 w-4" />}
          title="Object Storage"
          description="Store database backups and build artifacts"
        />
        <Link
          to="/integrations/new"
          search={{ category: "storage" }}
          className={buttonVariants({ variant: "default", className: "h-9 px-3" })}
        >
          <Plus className="size-4" />Add storage
        </Link>
      </div>

      {isLoading ? (
        <LoadingRow />
      ) : list.length === 0 ? (
        <EmptyState
          icon={<HardDrive className="h-7 w-7" />}
          title="No storage configured"
          description="Connect S3, R2, or MinIO to enable database backups"
        >
          <Link
            to="/integrations/new"
            search={{ category: "storage" }}
            className={buttonVariants({ variant: "default", className: "h-9 px-3" })}
          >
            <Plus className="size-4" />Add storage
          </Link>
        </EmptyState>
      ) : (
        <IntegrationTable>
          {list.map((sto) => (
            <StorageCard
              key={sto.id}
              integration={sto}
              onDelete={() => deleteMutation.mutateAsync(sto.id)}
              isDeleting={deleteMutation.isPending && deleteMutation.variables === sto.id}
            />
          ))}
        </IntegrationTable>
      )}
    </section>
  )
}

// ─── Storage card ─────────────────────────────────────────────────────────────

function StorageCard({ integration, onDelete, isDeleting }: {
  integration: ApiStorageIntegration
  onDelete: () => Promise<unknown>
  isDeleting: boolean
}) {
  const meta = [integration.bucket, integration.region].filter(Boolean).join(" · ")
  return <IntegrationRow name={integration.name} provider={integration.provider} status="Configured" details={meta} actions={<DisconnectButton name={integration.name} onDelete={onDelete} pending={isDeleting} />} />
}

// ─── Registry card ────────────────────────────────────────────────────────────

function RegistryCard({ registry, onDelete, isDeleting }: {
  registry: ApiRegistryIntegration
  onDelete: () => Promise<unknown>
  isDeleting: boolean
}) {
  const label = PROVIDER_LABELS[registry.provider] ?? registry.provider
  const meta = registry.namespace || registry.endpoint || ""

  return <IntegrationRow name={registry.name} provider={registry.provider} status={registry.provider === "builtin" ? "Built-in" : "Configured"} details={meta || label} actions={registry.provider !== "builtin" ? <DisconnectButton name={registry.name} onDelete={onDelete} pending={isDeleting} /> : <span className="text-xs text-muted-foreground">Managed by Meshploy</span>} />
}

// ─── Git integration card ─────────────────────────────────────────────────────

function GitIntegrationCard({ integration, orgId, token, onDelete, isDeleting }: {
  integration: ApiGitIntegration
  orgId: string
  token: string
  onDelete: () => Promise<unknown>
  isDeleting: boolean
}) {
  const [installing, setInstalling] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)

  const { data: repos, error: reposError } = useQuery({
    queryKey: ["git-repos", orgId, integration.id],
    queryFn: () => import("@/lib/api").then(({ gitIntegrations }) =>
      gitIntegrations.repos(orgId, integration.id, token)
    ),
    staleTime: 5 * 60 * 1000,
    enabled: integration.connected,
    retry: false,
  })

  // OAuth token expired and no refresh token available — user must re-authorize.
  const needsReconnect = integration.connected && integration.auth_method === "oauth"
    && (reposError as { status?: number } | null)?.status === 401

  async function handleInstall() {
    setInstalling(true)
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted) { setInstalling(false); window.removeEventListener("pageshow", onPageShow) }
    }
    window.addEventListener("pageshow", onPageShow)
    try {
      const { url } = await gitApi.installUrl(orgId, integration.id, token)
      window.location.href = url
    } catch {
      window.removeEventListener("pageshow", onPageShow)
      setInstalling(false)
    }
  }

  async function handleReconnect() {
    setReconnecting(true)
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted) { setReconnecting(false); window.removeEventListener("pageshow", onPageShow) }
    }
    window.addEventListener("pageshow", onPageShow)
    try {
      const { auth_url } = await gitApi.oauthReconnect(orgId, integration.id, token)
      window.location.href = auth_url
    } catch {
      window.removeEventListener("pageshow", onPageShow)
      setReconnecting(false)
    }
  }

  const isPending = !integration.connected
  const isGHApp = integration.auth_method === "app"
  const isOAuth = integration.auth_method === "oauth"

  return <IntegrationRow name={integration.name} provider={integration.provider} status={isPending || needsReconnect ? "Action required" : "Connected"}
    details={<>{needsReconnect ? "Token expired" : isPending ? "Authorization incomplete" : repos !== undefined ? `${repos.length} repositories accessible` : reposError ? "Repository access unavailable" : "Checking repository access…"}</>}
    actions={<>
      {isPending && isGHApp && <Button variant="outline" size="sm" onClick={handleInstall} disabled={installing || !integration.gh_app_slug}>{installing ? <Loader2 className="size-3 animate-spin" /> : <Download className="size-3" />}Install app</Button>}
      {(isPending && isOAuth || needsReconnect) && <Button variant="outline" size="sm" onClick={handleReconnect} disabled={reconnecting}><RefreshCw className="size-3" />Re-authorize</Button>}
      <DisconnectButton name={integration.name} onDelete={onDelete} pending={isDeleting} />
    </>} />
}

// ─── Notifications section ────────────────────────────────────────────────────

function NotificationsSection({ orgId, token }: { orgId: string; token: string }) {
  const qc = useQueryClient()
  const navigate = useNavigate()

  const { data: list = [], isLoading } = useQuery({
    queryKey: ["notification-channels", orgId],
    queryFn: () => notificationsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => notificationsApi.delete(orgId, id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-channels", orgId] }),
  })

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      notificationsApi.update(orgId, id, { enabled }, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-channels", orgId] }),
  })

  return (
    <section id="integration-notifications" className="integration-category space-y-4">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<Bell className="h-4 w-4" />}
          title="Notification Channels"
          description="Get alerted on deployments, node status changes, and backup results"
        />
        {isLoading
          ? <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          : (
            <Button
              variant="default"
              onClick={() => navigate({ to: "/integrations/new", search: { category: "notifications" } })}
              className="h-9 px-3"
            >
              <Plus className="size-4" />Add channel
            </Button>
          )
        }
      </div>
      {!isLoading && list.length === 0 ? (
        <EmptyState
          icon={<Bell className="h-7 w-7" />}
          title="No notification channels"
          description="Add an email address or webhook to get notified on deployments and failures"
        />
      ) : (
        <IntegrationTable>
          {list.map((ch) => (
            <NotificationCard
              key={ch.id}
              channel={ch}
              onDelete={() => deleteMutation.mutateAsync(ch.id)}
              isDeleting={deleteMutation.isPending && deleteMutation.variables === ch.id}
              onToggle={(enabled) => toggleMutation.mutate({ id: ch.id, enabled })}
              isToggling={toggleMutation.isPending && (toggleMutation.variables as any)?.id === ch.id}
            />
          ))}
        </IntegrationTable>
      )}
    </section>
  )
}

function NotificationCard({ channel, onDelete, isDeleting, onToggle, isToggling }: {
  channel: ApiNotificationChannel
  onDelete: () => Promise<unknown>
  isDeleting: boolean
  onToggle: (enabled: boolean) => void
  isToggling: boolean
}) {
  const [editing, setEditing] = useState(false)
  const destination =
    channel.type === "email" ? channel.config.address
    : channel.type === "slack" || channel.type === "discord" ? channel.config.webhook_url
    : channel.config.url

  return <>
    <IntegrationRow name={channel.name} provider={channel.type} status={channel.enabled ? "Enabled" : "Paused"} details={<><span className="block max-w-sm truncate">{destination}</span><span className="text-xs">{channel.events.length} subscribed events</span></>} actions={<>
      <Button variant="outline" size="sm" onClick={() => setEditing(true)}>Events</Button>
      <Button variant="outline" size="sm" onClick={() => onToggle(!channel.enabled)} disabled={isToggling}>{channel.enabled ? "Pause" : "Resume"}</Button>
      <DisconnectButton name={channel.name} onDelete={onDelete} pending={isDeleting} />
    </>} />
    <EditEventsDialog channel={channel} open={editing} onOpenChange={setEditing} />
  </>
}

/**
 * Changing what an existing channel listens for.
 *
 * Without this a channel's events are fixed at creation, so the events added in
 * a release are unreachable for every channel made before it -- the same shape
 * of problem as an event nothing could subscribe to.
 */
function EditEventsDialog({ channel, open, onOpenChange }: {
  channel: ApiNotificationChannel
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [events, setEvents] = useState<string[]>(channel.events)

  useEffect(() => { if (open) setEvents(channel.events) }, [open, channel.events])

  const save = useMutation({
    mutationFn: () => notificationsApi.update(orgId, channel.id, { events }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["notification-channels", orgId] })
      onOpenChange(false)
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Events for {channel.name}</DialogTitle>
          <DialogDescription>Choose what this channel is told about.</DialogDescription>
        </DialogHeader>
        <EventPicker events={events} onChange={setEvents} />
        {save.isError && <p className="text-xs text-destructive">{(save.error as Error).message}</p>}
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => save.mutate()} disabled={save.isPending || events.length === 0}>
            {save.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Save
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// ─── Email provider section ───────────────────────────────────────────────────

function EmailProviderSection({ orgId, token }: { orgId: string; token: string }) {
  const qc       = useQueryClient()
  const navigate = useNavigate()

  const { data: cfg, isLoading } = useQuery({
    queryKey: ["email-config", orgId],
    queryFn: () => emailConfigApi.get(orgId, token).catch(() => null),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: () => emailConfigApi.delete(orgId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["email-config", orgId] }),
  })

  return (
    <section id="integration-email" className="integration-category space-y-4">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<Mail className="h-4 w-4" />}
          title="Email Provider"
          description="Outbound SMTP for email notification channels"
        />
        {!isLoading && !cfg && (
          <Button
            variant="default"
            onClick={() => navigate({ to: "/integrations/new", search: { category: "email" } })}
            className="h-9 px-3"
          >
            <Plus className="size-4" />Configure
          </Button>
        )}
      </div>

      {isLoading ? (
        <LoadingRow />
      ) : !cfg ? (
        <EmptyState
          icon={<Mail className="h-7 w-7" />}
          title="No email provider configured"
          description="Set up SMTP so email notification channels can send alerts"
        >
          <Button
            variant="default"
            onClick={() => navigate({ to: "/integrations/new", search: { category: "email" } })}
            className="h-9 px-3"
          >
            <Plus className="size-4" />Configure
          </Button>
        </EmptyState>
      ) : (
        <EmailProviderCard
          cfg={cfg}
          onEdit={() => navigate({ to: "/integrations/new", search: { category: "email" } })}
          onDelete={() => deleteMutation.mutateAsync()}
          isDeleting={deleteMutation.isPending}
        />
      )}
    </section>
  )
}

function EmailProviderCard({ cfg, onEdit, onDelete, isDeleting }: {
  cfg: ApiOrgEmailConfig
  onEdit: () => void
  onDelete: () => Promise<unknown>
  isDeleting: boolean
}) {
  return <IntegrationTable><IntegrationRow name={cfg.host} provider="email" status="Configured" details={<>{cfg.from_address}<span className="block text-xs">Port {cfg.port} · {cfg.use_tls ? "TLS" : "No TLS"}</span></>} actions={<>
    <Button variant="outline" size="sm" onClick={onEdit}><Pencil className="size-3" />Edit</Button>
    <DisconnectButton name={cfg.host} onDelete={onDelete} pending={isDeleting} />
  </>} /></IntegrationTable>
}

// ─── Shared primitives ────────────────────────────────────────────────────────

function ProviderIcon({ provider }: { provider: string }) {
  return (
    <div className="flex items-center justify-center accent-icon-tile shrink-0 text-xs font-bold uppercase">
      {({ github: "GH", gitlab: "GL", gitea: "GT" } as Record<string, string>)[provider] || provider.slice(0, 2)}
    </div>
  )
}

function SectionHeader({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="flex items-center gap-2">
      <div className="text-muted-foreground">{icon}</div>
      <div>
        <h2 className="text-sm font-medium text-foreground">{title}</h2>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
    </div>
  )
}

function EmptyState({ icon, title, description, children }: {
  icon: React.ReactNode
  title: string
  description: string
  children?: React.ReactNode
}) {
  return (
    <div className="rounded-lg border border-dashed border-border/60 py-8 flex flex-col items-center gap-3">
      <div className="text-muted-foreground/40">{icon}</div>
      <div className="text-center">
        <p className="text-sm text-muted-foreground">{title}</p>
        <p className="text-xs text-muted-foreground/60 mt-0.5">{description}</p>
      </div>
      {children}
    </div>
  )
}

function LoadingRow() {
  return (
    <div className="flex items-center gap-2 text-muted-foreground text-sm py-2">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
      <span>Loading…</span>
    </div>
  )
}

function IntegrationTable({ children }: { children: ReactNode }) {
  const [query, setQuery] = useState("")
  // Filter the fetched connections locally.
  return <div className="space-y-4 integration-table-area" data-search={query.trim().toLowerCase()}>
    <Input aria-label="Search integrations" placeholder="Search by name, provider, or status…" value={query} onChange={e => setQuery(e.target.value)} className="h-10 max-w-md" />
    <IntegrationSearchContext.Provider value={query.trim().toLowerCase()}>
      <div className="console-data-table overflow-x-auto"><table className="min-w-[760px]" aria-label="Integrations"><thead><tr><th>Name</th><th>Provider</th><th>Status</th><th>Details</th><th aria-label="Actions">Actions</th></tr></thead><tbody>{children}<tr className="integration-no-results"><td colSpan={5} className="text-center text-muted-foreground">No matching integrations. Try another search.</td></tr></tbody></table></div>
      {query && <p className="text-xs text-muted-foreground">Showing matches for “{query}”. <Button variant="link" size="sm" onClick={() => setQuery("")}>Clear search</Button></p>}
    </IntegrationSearchContext.Provider>
  </div>
}

const IntegrationSearchContext = createContext("")
function IntegrationRow({name, provider, status, details, actions}: {name: string; provider: string; status: string; details: ReactNode; actions: ReactNode}) {
  const query = useContext(IntegrationSearchContext)
  if (query && !`${name} ${PROVIDER_LABELS[provider] || provider} ${status} ${typeof details === "string" ? details : ""}`.toLowerCase().includes(query)) return null
  return <tr><td><div className="flex items-center gap-3"><ProviderIcon provider={provider}/><span className="font-medium">{name}</span></div></td><td className="text-muted-foreground">{PROVIDER_LABELS[provider] || provider}</td><td><Badge variant="secondary" className={status === "Action required" ? "text-amber-400" : ""}>{status}</Badge></td><td className="text-muted-foreground">{details}</td><td><div className="flex items-center justify-end gap-2">{actions}</div></td></tr>
}

function DisconnectButton({name, onDelete, pending}: {name: string; onDelete: () => Promise<unknown>; pending: boolean}) {
  const [open, setOpen] = useState(false)
  const [error, setError] = useState("")
  return <><Button variant="ghost" size="sm" aria-label={`Disconnect ${name}`} onClick={() => { setError(""); setOpen(true) }} className="text-muted-foreground hover:text-destructive"><Trash2 className="size-3.5" />Disconnect</Button>
    <Dialog open={open} onOpenChange={value => { if (!pending) setOpen(value) }}><DialogContent><DialogHeader><DialogTitle>Disconnect {name}?</DialogTitle><DialogDescription>This removes the connection from your workspace. Resources using it may need another integration for future operations.</DialogDescription></DialogHeader>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <div className="flex justify-end gap-2"><Button variant="outline" disabled={pending} onClick={() => setOpen(false)}>Cancel</Button><Button variant="destructive" disabled={pending} onClick={async () => { try { await onDelete(); setOpen(false) } catch (e) { setError(e instanceof Error ? e.message : "Could not disconnect. Try again.") } }}>{pending && <Loader2 className="size-3 animate-spin" />}Disconnect</Button></div>
    </DialogContent></Dialog></>
}
