import { createFileRoute, useSearch, useNavigate, Link } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Bell, Box, GitBranch, HardDrive, Loader2, Plus, Trash2, Download, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  gitIntegrations as gitApi,
  registries as registriesApi,
  storage as storageApi,
  notifications as notificationsApi,
  type ApiGitIntegration,
  type ApiRegistryIntegration,
  type ApiStorageIntegration,
  type ApiNotificationChannel,
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

  const gitDeleteMutation = useMutation({
    mutationFn: (id: string) => gitApi.delete(orgId, id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["git-integrations", orgId] }),
  })

  return (
    <div className="p-6 space-y-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Integrations</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Connect external services for source code, images, backups, and alerts
        </p>
      </div>

      {/* ── Git Sources ────────────────────────────────────────────────────── */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <SectionHeader
            icon={<GitBranch className="h-4 w-4" />}
            title="Git Sources"
            description="Connect GitHub, GitLab, or Gitea to deploy from repositories"
          />
          <Link
            to="/integrations/new"
            search={{ category: "git" }}
            className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors"
          >
            <Plus className="h-3 w-3" />Add source
          </Link>
        </div>

        {gitLoading ? (
          <LoadingRow />
        ) : gitList.length === 0 ? (
          <EmptyState icon={<GitBranch className="h-7 w-7" />} title="No git sources connected" description="Connect GitHub, GitLab, or Gitea to enable repository-based deployments">
            <Link
              to="/integrations/new"
              search={{ category: "git" }}
              className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1"
            >
              <Plus className="h-3 w-3" />Add source
            </Link>
          </EmptyState>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {gitList.map((g) => (
              <GitIntegrationCard
                key={g.id}
                integration={g}
                orgId={orgId}
                token={token}
                onDelete={() => gitDeleteMutation.mutate(g.id)}
                isDeleting={gitDeleteMutation.isPending && gitDeleteMutation.variables === g.id}
              />
            ))}
          </div>
        )}
      </section>

      {/* ── Container Registries ───────────────────────────────────────────── */}
      <RegistrySection orgId={orgId} token={token} />

      {/* ── Object Storage ─────────────────────────────────────────────────── */}
      <StorageSection orgId={orgId} token={token} />

      {/* ── Notification Channels ──────────────────────────────────────────── */}
      <NotificationsSection orgId={orgId} token={token} />
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
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<Box className="h-4 w-4" />}
          title="Container Registries"
          description="Pull and push images from private and public registries"
        />
        <Link
          to="/integrations/new"
          search={{ category: "registry" }}
          className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors"
        >
          <Plus className="h-3 w-3" />Add registry
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
            className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1"
          >
            <Plus className="h-3 w-3" />Add registry
          </Link>
        </EmptyState>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {list.map((reg) => (
            <RegistryCard
              key={reg.id}
              registry={reg}
              onDelete={() => deleteMutation.mutate(reg.id)}
              isDeleting={deleteMutation.isPending}
            />
          ))}
        </div>
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
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<HardDrive className="h-4 w-4" />}
          title="Object Storage"
          description="Store database backups and build artifacts"
        />
        <Link
          to="/integrations/new"
          search={{ category: "storage" }}
          className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors"
        >
          <Plus className="h-3 w-3" />Add storage
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
            className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1"
          >
            <Plus className="h-3 w-3" />Add storage
          </Link>
        </EmptyState>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {list.map((sto) => (
            <StorageCard
              key={sto.id}
              integration={sto}
              onDelete={() => deleteMutation.mutate(sto.id)}
              isDeleting={deleteMutation.isPending && deleteMutation.variables === sto.id}
            />
          ))}
        </div>
      )}
    </section>
  )
}

// ─── Storage card ─────────────────────────────────────────────────────────────

function StorageCard({ integration, onDelete, isDeleting }: {
  integration: ApiStorageIntegration
  onDelete: () => void
  isDeleting: boolean
}) {
  const meta = [integration.bucket, integration.region].filter(Boolean).join(" · ")
  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-card p-4">
      <ProviderIcon provider={integration.provider} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium text-foreground truncate">{integration.name}</p>
          <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">
            connected
          </Badge>
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">{PROVIDER_LABELS[integration.provider] ?? integration.provider}</p>
        {meta && <p className="text-[11px] font-mono text-muted-foreground/60 mt-1 truncate">{meta}</p>}
      </div>
      <button
        type="button"
        onClick={onDelete}
        disabled={isDeleting}
        className="shrink-0 p-1 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-30"
        title="Remove"
      >
        {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}

// ─── Registry card ────────────────────────────────────────────────────────────

function RegistryCard({ registry, onDelete, isDeleting }: {
  registry: ApiRegistryIntegration
  onDelete: () => void
  isDeleting: boolean
}) {
  const label = PROVIDER_LABELS[registry.provider] ?? registry.provider
  const meta = registry.namespace || registry.endpoint || ""

  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-card p-4">
      <ProviderIcon provider={registry.provider} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium text-foreground truncate">{registry.name}</p>
          {registry.provider === "builtin" ? (
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-blue-500/10 text-blue-400 border-0">
              built-in
            </Badge>
          ) : (
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">
              connected
            </Badge>
          )}
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">{label}</p>
        {meta && <p className="text-[11px] font-mono text-muted-foreground/60 mt-1 truncate">{meta}</p>}
      </div>
      {registry.provider !== "builtin" && (
        <button
          type="button"
          onClick={onDelete}
          disabled={isDeleting}
          className="shrink-0 p-1 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-30"
          title="Remove"
        >
          {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
        </button>
      )}
    </div>
  )
}

// ─── Git integration card ─────────────────────────────────────────────────────

function GitIntegrationCard({ integration, orgId, token, onDelete, isDeleting }: {
  integration: ApiGitIntegration
  orgId: string
  token: string
  onDelete: () => void
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

  return (
    <div className={`flex items-start gap-3 rounded-lg border bg-card p-4 ${isPending || needsReconnect ? "border-amber-500/30" : "border-border/60"}`}>
      <ProviderIcon provider={integration.provider} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <p className="text-sm font-medium text-foreground truncate">
            {integration.provider === "github" ? (integration.gh_app_slug || integration.name) : integration.name}
          </p>
          {isPending ? (
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-amber-500/10 text-amber-400 border-amber-500/20">
              action required
            </Badge>
          ) : (
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">
              connected
            </Badge>
          )}
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">
          {PROVIDER_LABELS[integration.provider] ?? integration.provider}
          {isPending && isGHApp && " · awaiting installation"}
          {isPending && isOAuth && " · authorization incomplete"}
          {needsReconnect && " · token expired"}
        </p>
        {integration.connected && repos !== undefined && (
          <p className="text-[11px] font-mono text-muted-foreground/60 mt-1">
            {repos.length} {repos.length === 1 ? "repo" : "repos"} accessible
          </p>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-0.5 shrink-0">
        {isPending && isGHApp && (
          <button
            type="button"
            onClick={handleInstall}
            disabled={installing || !integration.gh_app_slug}
            className="p-1 text-muted-foreground/60 hover:text-foreground transition-colors disabled:opacity-30"
            title={integration.gh_app_slug ? "Install GitHub App" : "Setup not yet complete"}
          >
            {installing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
          </button>
        )}
        {(isPending && isOAuth || needsReconnect) && (
          <button
            type="button"
            onClick={handleReconnect}
            disabled={reconnecting}
            className="p-1 text-muted-foreground/60 hover:text-foreground transition-colors disabled:opacity-30"
            title={needsReconnect ? "Token expired — re-authorize" : "Re-authorize"}
          >
            {reconnecting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
          </button>
        )}
        <button
          type="button"
          onClick={onDelete}
          disabled={isDeleting}
          className="p-1 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-30"
          title="Disconnect"
        >
          {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
        </button>
      </div>
    </div>
  )
}

// ─── Mock section cards ───────────────────────────────────────────────────────

function IntegrationCard({ name, providerKey, meta }: { name: string; providerKey: string; meta: string }) {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-card p-4">
      <ProviderIcon provider={providerKey} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium text-foreground truncate">{name}</p>
          <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">connected</Badge>
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">{PROVIDER_LABELS[providerKey] ?? providerKey}</p>
        <p className="text-[11px] font-mono text-muted-foreground/60 mt-1 truncate">{meta}</p>
      </div>
    </div>
  )
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
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <SectionHeader
          icon={<Bell className="h-4 w-4" />}
          title="Notification Channels"
          description="Get alerted on deployments, node status changes, and backup results"
        />
        {isLoading
          ? <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          : (
            <button
              onClick={() => navigate({ to: "/integrations/new", search: { category: "notifications" } })}
              className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground border border-border/60 hover:border-border px-2.5 py-1.5 rounded-md transition-colors"
            >
              <Plus className="h-3 w-3" />Add channel
            </button>
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
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {list.map((ch) => (
            <NotificationCard
              key={ch.id}
              channel={ch}
              onDelete={() => deleteMutation.mutate(ch.id)}
              isDeleting={deleteMutation.isPending && deleteMutation.variables === ch.id}
              onToggle={(enabled) => toggleMutation.mutate({ id: ch.id, enabled })}
              isToggling={toggleMutation.isPending && (toggleMutation.variables as any)?.id === ch.id}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function NotificationCard({ channel, onDelete, isDeleting, onToggle, isToggling }: {
  channel: ApiNotificationChannel
  onDelete: () => void
  isDeleting: boolean
  onToggle: (enabled: boolean) => void
  isToggling: boolean
}) {
  const destination = channel.type === "email"
    ? channel.config.address
    : channel.config.url

  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center gap-3">
        <ProviderIcon provider={channel.type} />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium text-foreground truncate">{channel.name}</p>
            <Badge
              variant="secondary"
              className={`text-[10px] px-1.5 py-0 h-4 shrink-0 border-0 ${
                channel.enabled
                  ? "bg-emerald-500/10 text-emerald-400"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {channel.enabled ? "active" : "paused"}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground truncate">{PROVIDER_LABELS[channel.type]}{destination ? ` · ${destination}` : ""}</p>
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
          <button
            onClick={() => onToggle(!channel.enabled)}
            disabled={isToggling}
            title={channel.enabled ? "Pause" : "Resume"}
            className="p-1.5 text-muted-foreground/40 hover:text-foreground transition-colors disabled:opacity-30"
          >
            {isToggling
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : channel.enabled
                ? <span className="text-[10px] font-mono">II</span>
                : <span className="text-[10px] font-mono">▶</span>
            }
          </button>
          <button
            onClick={onDelete}
            disabled={isDeleting}
            title="Delete"
            className="p-1.5 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-30"
          >
            {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
          </button>
        </div>
      </div>
      {channel.events.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {channel.events.map((ev) => (
            <code key={ev} className="text-[10px] font-mono bg-muted/60 px-1.5 py-0.5 rounded text-muted-foreground">{ev}</code>
          ))}
        </div>
      )}
    </div>
  )
}

// ─── Shared primitives ────────────────────────────────────────────────────────

function ProviderIcon({ provider }: { provider: string }) {
  return (
    <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted shrink-0 text-xs font-bold text-muted-foreground uppercase">
      {provider.slice(0, 2)}
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
