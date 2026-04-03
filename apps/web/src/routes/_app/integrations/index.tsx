import { createFileRoute, useSearch, useNavigate } from "@tanstack/react-router"
import React, { useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Bell, Box, GitBranch, HardDrive, Loader2, Plus, Settings2, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  gitIntegrations as gitApi,
  gitHubApp,
  type ApiGitIntegration,
} from "@/lib/api"
import { mockRegistries, mockStorage, mockNotifications } from "@/lib/mock-data"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { NotificationChannel } from "@/types"

const PROVIDER_LABELS: Record<string, string> = {
  ghcr: "GitHub Container Registry", dockerhub: "Docker Hub", ecr: "Amazon ECR", generic: "Private Registry",
  s3: "Amazon S3", r2: "Cloudflare R2", minio: "MinIO",
  slack: "Slack", webhook: "Webhook", email: "Email",
  github: "GitHub", gitlab: "GitLab", gitea: "Gitea",
}

export const Route = createFileRoute("/_app/integrations/")({
  component: IntegrationsPage,
})

function IntegrationsPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [actionError, setActionError] = React.useState<string | null>(null)
  const [actioning, setActioning] = React.useState(false)

  const search = useSearch({ strict: false }) as Record<string, string>
  useEffect(() => {
    if (search.app_setup === "done") {
      qc.invalidateQueries({ queryKey: ["github-app-status"] })
      navigate({ to: "/integrations", replace: true })
    } else if (search.github === "connected") {
      qc.invalidateQueries({ queryKey: ["git-integrations", orgId] })
      navigate({ to: "/integrations", replace: true })
    }
  }, [search.app_setup, search.github])

  const { data: appStatus } = useQuery({
    queryKey: ["github-app-status"],
    queryFn: () => gitHubApp.status(),
  })

  const { data: gitList = [], isLoading: gitLoading } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token),
    enabled: !!orgId && appStatus?.configured === true,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => gitApi.delete(orgId, id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["git-integrations", orgId] }),
  })

  async function handleSetupGitHubApp() {
    setActionError(null)
    setActioning(true)
    try {
      const { github_url, manifest } = await gitHubApp.manifestSetup()
      const form = document.createElement("form")
      form.method = "POST"
      form.action = github_url
      const input = document.createElement("input")
      input.type = "hidden"
      input.name = "manifest"
      input.value = manifest
      form.appendChild(input)
      document.body.appendChild(form)
      form.submit()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to start GitHub App setup"
      setActionError(msg)
      setActioning(false)
    }
  }

  async function handleConnectGitHub() {
    setActionError(null)
    setActioning(true)
    try {
      const { url } = await gitApi.installUrl(orgId, token)
      window.location.href = url
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to get install URL"
      setActionError(msg)
      setActioning(false)
    }
  }

  const appConfigured = appStatus?.configured === true

  return (
    <div className="p-6 space-y-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Integrations</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Connect external services for source code, images, backups, and alerts</p>
      </div>

      {/* Git Sources */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="text-muted-foreground"><GitBranch className="h-4 w-4" /></div>
            <div>
              <h2 className="text-sm font-medium text-foreground">Git Sources</h2>
              <p className="text-xs text-muted-foreground">
                {appConfigured
                  ? "Connect GitHub organizations to deploy from repositories"
                  : "Set up the Meshploy GitHub App to enable git-based deployments"}
              </p>
            </div>
          </div>
          {appConfigured ? (
            <button
              onClick={handleConnectGitHub}
              disabled={actioning}
              className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {actioning ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
              {actioning ? "Redirecting…" : "Connect GitHub"}
            </button>
          ) : (
            <button
              onClick={handleSetupGitHubApp}
              disabled={actioning}
              className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {actioning ? <Loader2 className="h-3 w-3 animate-spin" /> : <Settings2 className="h-3 w-3" />}
              {actioning ? "Opening GitHub…" : "Setup GitHub App"}
            </button>
          )}
        </div>
        {actionError && (
          <p className="text-xs text-destructive">{actionError}</p>
        )}

        {!appConfigured ? (
          <div className="rounded-lg border border-dashed border-border/60 py-8 flex flex-col items-center gap-3">
            <GitBranch className="h-7 w-7 text-muted-foreground/40" />
            <div className="text-center">
              <p className="text-sm text-muted-foreground">GitHub App not set up</p>
              <p className="text-xs text-muted-foreground/60 mt-0.5">
                Click "Setup GitHub App" to register Meshploy as a GitHub App on your account
              </p>
            </div>
            <button
              onClick={handleSetupGitHubApp}
              disabled={actioning}
              className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {actioning ? <Loader2 className="h-3 w-3 animate-spin" /> : <Settings2 className="h-3 w-3" />}
              {actioning ? "Opening GitHub…" : "Setup GitHub App"}
            </button>
          </div>
        ) : gitLoading ? (
          <div className="flex items-center gap-2 py-6 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span className="text-sm">Loading…</span>
          </div>
        ) : gitList.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border/60 py-8 flex flex-col items-center gap-3">
            <GitBranch className="h-7 w-7 text-muted-foreground/40" />
            <div className="text-center">
              <p className="text-sm text-muted-foreground">No GitHub organizations connected</p>
              <p className="text-xs text-muted-foreground/60 mt-0.5">Install the Meshploy App on your GitHub organization</p>
            </div>
            <button
              onClick={handleConnectGitHub}
              disabled={actioning}
              className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {actioning ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
              {actioning ? "Redirecting…" : "Connect GitHub"}
            </button>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {gitList.map((g) => (
              <GitIntegrationCard
                key={g.id}
                integration={g}
                orgId={orgId}
                token={token}
                onDelete={() => deleteMutation.mutate(g.id)}
                isDeleting={deleteMutation.isPending}
              />
            ))}
          </div>
        )}
      </section>

      <Section icon={<Box className="h-4 w-4" />} title="Container Registries" description="Pull images from private and public registries" action="Add registry">
        {mockRegistries.map((reg) => (
          <IntegrationCard key={reg.id} name={reg.name} providerKey={reg.provider} meta={reg.endpoint ?? `docker.io/${reg.username}`} />
        ))}
      </Section>

      <Section icon={<HardDrive className="h-4 w-4" />} title="Object Storage" description="Store database backups and build artifacts" action="Add storage">
        {mockStorage.map((sto) => (
          <IntegrationCard key={sto.id} name={sto.name} providerKey={sto.provider} meta={`${sto.bucket}${sto.region ? ` · ${sto.region}` : ""}`} />
        ))}
      </Section>

      <Section icon={<Bell className="h-4 w-4" />} title="Notification Channels" description="Get alerted on deployments, node status changes, and failures" action="Add channel">
        {mockNotifications.map((ntf) => <NotificationCard key={ntf.id} channel={ntf} />)}
      </Section>
    </div>
  )
}

// ─── GitIntegrationCard ───────────────────────────────────────────────────────

function GitIntegrationCard({
  integration, orgId, token, onDelete, isDeleting,
}: {
  integration: ApiGitIntegration
  orgId: string
  token: string
  onDelete: () => void
  isDeleting: boolean
}) {
  const { data: repos } = useQuery({
    queryKey: ["git-repos", orgId, integration.id],
    queryFn: () => import("@/lib/api").then(({ gitIntegrations }) =>
      gitIntegrations.repos(orgId, integration.id, token)
    ),
    staleTime: 5 * 60 * 1000,
  })

  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted shrink-0 text-xs font-bold text-muted-foreground uppercase">
        {integration.provider.slice(0, 2)}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium text-foreground truncate">{integration.name}</p>
          <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">
            connected
          </Badge>
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">
          {PROVIDER_LABELS[integration.provider] ?? integration.provider}
        </p>
        {repos !== undefined && (
          <p className="text-[11px] font-mono text-muted-foreground/60 mt-1">
            {repos.length} {repos.length === 1 ? "repo" : "repos"} accessible
          </p>
        )}
      </div>
      <button
        type="button"
        onClick={onDelete}
        disabled={isDeleting}
        className="shrink-0 p-1 text-muted-foreground/40 hover:text-destructive transition-colors disabled:opacity-30"
        title="Disconnect"
      >
        {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}

// ─── Shared section / card components ────────────────────────────────────────

function Section({ icon, title, description, action, children }: {
  icon: React.ReactNode; title: string; description: string; action: string; children: React.ReactNode
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className="text-muted-foreground">{icon}</div>
          <div>
            <h2 className="text-sm font-medium text-foreground">{title}</h2>
            <p className="text-xs text-muted-foreground">{description}</p>
          </div>
        </div>
        <button className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors">
          <Plus className="h-3 w-3" />{action}
        </button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{children}</div>
    </section>
  )
}

function IntegrationCard({ name, providerKey, meta }: { name: string; providerKey: string; meta: string }) {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted shrink-0 text-xs font-bold text-muted-foreground uppercase">
        {providerKey.slice(0, 2)}
      </div>
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

function NotificationCard({ channel }: { channel: NotificationChannel }) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center gap-3">
        <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted shrink-0 text-xs font-bold text-muted-foreground uppercase">
          {channel.type.slice(0, 2)}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium text-foreground truncate">{channel.name}</p>
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">active</Badge>
          </div>
          <p className="text-xs text-muted-foreground">{PROVIDER_LABELS[channel.type]}</p>
        </div>
      </div>
      <div className="flex flex-wrap gap-1">
        {channel.events.map((ev) => (
          <code key={ev} className="text-[10px] font-mono bg-muted/60 px-1.5 py-0.5 rounded text-muted-foreground">{ev}</code>
        ))}
      </div>
    </div>
  )
}
