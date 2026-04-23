import { createFileRoute, useSearch, useNavigate } from "@tanstack/react-router"
import React, { useEffect, useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Bell, Box, GitBranch, HardDrive, Loader2, Plus, Settings2, Trash2, AlertCircle, Eye, EyeOff, Download, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  gitIntegrations as gitApi,
  registries as registriesApi,
  type ApiGitIntegration,
  type ApiRegistryIntegration,
  type RegistryProvider,
  type CreateRegistryBody,
} from "@/lib/api"
import { mockStorage, mockNotifications } from "@/lib/mock-data"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Field } from "@/components/services/form-primitives"
import type { NotificationChannel } from "@/types"

type GitProvider = "github" | "gitlab" | "gitea"

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
  const [addGitOpen, setAddGitOpen] = useState(false)

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
          <button
            onClick={() => setAddGitOpen(true)}
            className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors"
          >
            <Plus className="h-3 w-3" />Add source
          </button>
        </div>

        {gitLoading ? (
          <LoadingRow />
        ) : gitList.length === 0 ? (
          <EmptyState icon={<GitBranch className="h-7 w-7" />} title="No git sources connected" description="Connect GitHub, GitLab, or Gitea to enable repository-based deployments">
            <button
              onClick={() => setAddGitOpen(true)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1"
            >
              <Plus className="h-3 w-3" />Add source
            </button>
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

        <AddGitSourceDialog
          open={addGitOpen}
          onClose={() => setAddGitOpen(false)}
          orgId={orgId}
          token={token}
          onSuccess={() => {
            qc.invalidateQueries({ queryKey: ["git-integrations", orgId] })
            setAddGitOpen(false)
          }}
        />
      </section>

      {/* ── Container Registries ───────────────────────────────────────────── */}
      <RegistrySection orgId={orgId} token={token} />

      {/* ── Object Storage (mock / coming soon) ────────────────────────────── */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <SectionHeader
            icon={<HardDrive className="h-4 w-4" />}
            title="Object Storage"
            description="Store database backups and build artifacts"
          />
          <button disabled className="flex items-center gap-1.5 text-xs text-muted-foreground/40 border border-border/40 px-2.5 py-1.5 rounded-md cursor-not-allowed">
            <Plus className="h-3 w-3" />Add storage
          </button>
        </div>
        {mockStorage.length === 0 ? (
          <EmptyState icon={<HardDrive className="h-7 w-7" />} title="No storage configured" description="Coming soon — connect S3, R2, or MinIO for backups" />
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {mockStorage.map((sto) => (
              <IntegrationCard key={sto.id} name={sto.name} providerKey={sto.provider} meta={`${sto.bucket}${sto.region ? ` · ${sto.region}` : ""}`} />
            ))}
          </div>
        )}
      </section>

      {/* ── Notification Channels (mock / coming soon) ─────────────────────── */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <SectionHeader
            icon={<Bell className="h-4 w-4" />}
            title="Notification Channels"
            description="Get alerted on deployments, node status changes, and failures"
          />
          <button disabled className="flex items-center gap-1.5 text-xs text-muted-foreground/40 border border-border/40 px-2.5 py-1.5 rounded-md cursor-not-allowed">
            <Plus className="h-3 w-3" />Add channel
          </button>
        </div>
        {mockNotifications.length === 0 ? (
          <EmptyState icon={<Bell className="h-7 w-7" />} title="No notification channels" description="Coming soon — connect Slack, Discord, or webhooks" />
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {mockNotifications.map((ntf) => <NotificationCard key={ntf.id} channel={ntf} />)}
          </div>
        )}
      </section>
    </div>
  )
}

// ─── Add git source dialog ────────────────────────────────────────────────────

type AuthMethod = "pat" | "oauth"

function getAPIBase(): string {
  const configured: string =
    (window as Window & { __MESHPLOY_CONFIG__?: { apiUrl: string } })
      .__MESHPLOY_CONFIG__?.apiUrl
    ?? import.meta.env.VITE_API_URL
    ?? ""
  if (!configured) return window.location.origin
  return configured
}

function AddGitSourceDialog({ open, onClose, orgId, token, onSuccess }: {
  open: boolean
  onClose: () => void
  orgId: string
  token: string
  onSuccess: () => void
}) {
  const [provider, setProvider] = useState<GitProvider>("github")
  const [authMethod, setAuthMethod] = useState<AuthMethod>("pat")
  // shared
  const [name, setName] = useState("")
  const [baseURL, setBaseURL] = useState("")
  const [groups, setGroups] = useState("")
  // PAT
  const [pat, setPAT] = useState("")
  const [showPAT, setShowPAT] = useState(false)
  // OAuth
  const [clientID, setClientID] = useState("")
  const [clientSecret, setClientSecret] = useState("")
  const [showSecret, setShowSecret] = useState(false)
  // GitHub org name (app creation)
  const [githubOrg, setGithubOrg] = useState("")

  const [error, setError] = useState<string | null>(null)
  const [actioning, setActioning] = useState(false)

  function reset() {
    setProvider("github")
    setAuthMethod("pat")
    setGithubOrg("")
    setName("")
    setBaseURL("")
    setGroups("")
    setPAT("")
    setShowPAT(false)
    setClientID("")
    setClientSecret("")
    setShowSecret(false)
    setError(null)
    setActioning(false)
  }

  const patMutation = useMutation({
    mutationFn: (body: { provider: "gitlab" | "gitea"; name: string; base_url?: string; groups?: string; token: string }) =>
      gitApi.createPAT(orgId, body, token),
    onSuccess: () => { reset(); onSuccess() },
    onError: (err: Error) => setError(err.message),
  })

  const oauthMutation = useMutation({
    mutationFn: (body: { provider: "gitlab" | "gitea"; name: string; base_url?: string; groups?: string; redirect_uri: string; client_id: string; client_secret: string }) =>
      gitApi.initOAuth(orgId, body, token),
    onSuccess: ({ auth_url }) => { window.location.href = auth_url },
    onError: (err: Error) => setError(err.message),
  })

  async function handleGitHubSetup() {
    setError(null); setActioning(true)
    // Reset spinner if user returns via browser back / bfcache restore
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted) { setActioning(false); window.removeEventListener("pageshow", onPageShow) }
    }
    window.addEventListener("pageshow", onPageShow)
    try {
      const { github_url, manifest } = await gitApi.initGitHub(orgId, { github_org: githubOrg.trim() || undefined }, token)
      const form = document.createElement("form")
      form.method = "POST"; form.action = github_url
      const input = document.createElement("input")
      input.type = "hidden"; input.name = "manifest"; input.value = manifest
      form.appendChild(input); document.body.appendChild(form); form.submit()
    } catch (err: unknown) {
      window.removeEventListener("pageshow", onPageShow)
      setError(err instanceof Error ? err.message : "Failed to start GitHub App setup")
      setActioning(false)
    }
  }

  function submitPAT() {
    setError(null)
    patMutation.mutate({ provider: provider as "gitlab" | "gitea", name: name.trim(), base_url: baseURL.trim() || undefined, groups: groups.trim() || undefined, token: pat })
  }

  function submitOAuth() {
    setError(null)
    const redirectURI = provider === "gitlab" ? gitlabRedirectURI : giteaRedirectURI
    oauthMutation.mutate({ provider: provider as "gitlab" | "gitea", name: name.trim(), base_url: baseURL.trim() || undefined, groups: groups.trim() || undefined, redirect_uri: redirectURI, client_id: clientID.trim(), client_secret: clientSecret })
  }

  const TABS: { value: GitProvider; label: string }[] = [
    { value: "github", label: "GitHub" },
    { value: "gitlab", label: "GitLab" },
    { value: "gitea", label: "Gitea" },
  ]

  const apiBase = getAPIBase()
  const gitlabRedirectURI = `${apiBase}/api/v1/gitlab/callback`
  const giteaRedirectURI = `${apiBase}/api/v1/gitea/callback`
  const isPAT = authMethod === "pat"
  const isGitLabOrGitea = provider === "gitlab" || provider === "gitea"

  const monoCls = inputCls + " font-mono"

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) { reset(); onClose() } }}>
      <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add git source</DialogTitle>
        </DialogHeader>

        {/* Provider tabs */}
        <div className="flex gap-1 rounded-lg bg-muted/40 p-1 mt-1">
          {TABS.map((tab) => (
            <button key={tab.value} type="button"
              onClick={() => { setProvider(tab.value); setAuthMethod("pat"); setError(null) }}
              className={`flex-1 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                provider === tab.value ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              }`}
            >{tab.label}</button>
          ))}
        </div>

        {/* ── GitHub ── */}
        {provider === "github" && (
          <div className="space-y-4 mt-1">
            <p className="text-xs text-muted-foreground leading-relaxed">
              Register a new GitHub App on your account or organization. Each app gives Meshploy access to one account's repositories.
            </p>
            <input
              className={inputCls}
              placeholder="Organization name (optional)"
              value={githubOrg}
              onChange={(e) => setGithubOrg(e.target.value)}
            />
            <p className="text-[11px] text-muted-foreground/60 -mt-2">
              Leave empty to create the app under your personal account.
            </p>
            {error && <ErrorBanner message={error} />}
            <Button onClick={handleGitHubSetup} disabled={actioning} className="w-full gap-1.5">
              {actioning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Settings2 className="h-3.5 w-3.5" />}
              {actioning ? "Opening GitHub…" : "Create GitHub App"}
            </Button>
            <DialogFooter showCloseButton />
          </div>
        )}

        {/* ── GitLab / Gitea ── */}
        {isGitLabOrGitea && (
          <div className="space-y-4 mt-1">
            {/* Method selector cards */}
            <div className="grid grid-cols-2 gap-2">
              {(["pat", "oauth"] as AuthMethod[]).map((m) => (
                <button key={m} type="button" onClick={() => { setAuthMethod(m); setError(null) }}
                  className={`rounded-lg border p-3 text-left transition-colors ${
                    authMethod === m ? "border-ring bg-ring/5" : "border-border/60 hover:border-border"
                  }`}
                >
                  <p className={`text-xs font-semibold ${authMethod === m ? "text-foreground" : "text-muted-foreground"}`}>
                    {m === "pat" ? "Personal Access Token" : "OAuth Application"}
                  </p>
                  <p className="text-[11px] text-muted-foreground/70 mt-0.5 leading-relaxed">
                    {m === "pat"
                      ? "Simple — paste a token from your account settings"
                      : "Standard — stable, refreshable OAuth2 connection"}
                  </p>
                </button>
              ))}
            </div>

            {/* Instance URL (both methods) */}
            <Field label={provider === "gitlab" ? "Instance URL (leave empty for gitlab.com)" : "Instance URL"}>
              <input type="url"
                placeholder={provider === "gitlab" ? "https://gitlab.example.com" : "https://gitea.example.com"}
                value={baseURL} onChange={(e) => setBaseURL(e.target.value)}
                required={provider === "gitea"}
                className={monoCls}
              />
            </Field>

            {/* ── PAT instructions ── */}
            {isPAT && (
              <div className="rounded-md border border-border/50 bg-muted/30 px-3 py-2.5 space-y-1.5 text-xs text-muted-foreground">
                <p className="font-medium text-foreground">Setup steps</p>
                {provider === "gitlab" ? (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your GitLab profile → <span className="font-medium text-foreground">Access Tokens</span></li>
                    <li>Click <span className="font-medium text-foreground">Add new token</span></li>
                    <li>Enable scopes: <code className="bg-muted px-1 rounded">read_api</code> <code className="bg-muted px-1 rounded">read_repository</code></li>
                    <li>Copy the generated token and paste it below</li>
                  </ol>
                ) : (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your Gitea profile → <span className="font-medium text-foreground">Settings → Applications</span></li>
                    <li>Under <span className="font-medium text-foreground">Manage Access Tokens</span>, click <span className="font-medium text-foreground">Generate Token</span></li>
                    <li>Enable <code className="bg-muted px-1 rounded">repository</code> read permission</li>
                    <li>Copy the token and paste it below</li>
                  </ol>
                )}
              </div>
            )}

            {/* ── OAuth instructions ── */}
            {!isPAT && (
              <div className="rounded-md border border-border/50 bg-muted/30 px-3 py-2.5 space-y-1.5 text-xs text-muted-foreground">
                <p className="font-medium text-foreground">Setup steps</p>
                {provider === "gitlab" ? (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your GitLab profile → <span className="font-medium text-foreground">Applications</span></li>
                    <li>Create a new application — Name: <span className="font-medium text-foreground">Meshploy</span></li>
                    <li>Set Redirect URI to the value below, enable scopes: <code className="bg-muted px-1 rounded">api</code> <code className="bg-muted px-1 rounded">read_user</code> <code className="bg-muted px-1 rounded">read_repository</code></li>
                    <li>Copy the Application ID and Secret and paste them below</li>
                  </ol>
                ) : (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your Gitea profile → <span className="font-medium text-foreground">Settings → Applications</span></li>
                    <li>Under <span className="font-medium text-foreground">OAuth2 Applications</span>, click <span className="font-medium text-foreground">Create OAuth2 Application</span></li>
                    <li>Name: <span className="font-medium text-foreground">Meshploy</span>, set Redirect URI to the value below</li>
                    <li>Copy the Client ID and Secret and paste them below</li>
                  </ol>
                )}
                {/* Redirect URI copy box */}
                <div className="mt-2 space-y-1">
                  <p className="text-[11px] text-muted-foreground/60">Redirect URI — paste this into {provider === "gitlab" ? "GitLab" : "Gitea"}:</p>
                  <div className="flex items-center gap-1.5 rounded bg-muted px-2 py-1.5">
                    <code className="flex-1 text-[11px] font-mono text-foreground break-all">
                      {provider === "gitlab" ? gitlabRedirectURI : giteaRedirectURI}
                    </code>
                    <button type="button"
                      onClick={() => navigator.clipboard.writeText(provider === "gitlab" ? gitlabRedirectURI : giteaRedirectURI)}
                      className="shrink-0 text-muted-foreground hover:text-foreground transition-colors text-[11px]"
                    >Copy</button>
                  </div>
                </div>
              </div>
            )}

            {/* Group / Org scoping */}
            <Field label={provider === "gitlab" ? "Group name (optional)" : "Organization name (optional)"}>
              <input type="text"
                placeholder={provider === "gitlab" ? "e.g. my-group or my-group/sub-group" : "e.g. my-org"}
                value={groups} onChange={(e) => setGroups(e.target.value)}
                className={inputCls}
              />
            </Field>

            {/* Label */}
            <Field label="Label" required>
              <input type="text"
                placeholder={provider === "gitlab" ? "e.g. my-gitlab-org" : "e.g. my-gitea-org"}
                value={name} onChange={(e) => setName(e.target.value)}
                className={inputCls}
              />
            </Field>

            {/* PAT fields */}
            {isPAT && (
              <Field label="Personal access token" required>
                <div className="flex items-center gap-1">
                  <input type={showPAT ? "text" : "password"}
                    value={pat} onChange={(e) => setPAT(e.target.value)}
                    autoComplete="new-password"
                    placeholder={provider === "gitlab" ? "glpat-…" : ""}
                    className={`flex-1 ${monoCls}`}
                  />
                  <button type="button" onClick={() => setShowPAT((v) => !v)}
                    className="p-2 text-muted-foreground hover:text-foreground transition-colors">
                    {showPAT ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
                <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
              </Field>
            )}

            {/* OAuth fields */}
            {!isPAT && (
              <>
                <Field label={provider === "gitlab" ? "Application ID" : "Client ID"} required>
                  <input type="text" value={clientID} onChange={(e) => setClientID(e.target.value)}
                    autoComplete="off" className={monoCls}
                  />
                </Field>
                <Field label={provider === "gitlab" ? "Application Secret" : "Client Secret"} required>
                  <div className="flex items-center gap-1">
                    <input type={showSecret ? "text" : "password"}
                      value={clientSecret} onChange={(e) => setClientSecret(e.target.value)}
                      autoComplete="new-password" className={`flex-1 ${monoCls}`}
                    />
                    <button type="button" onClick={() => setShowSecret((v) => !v)}
                      className="p-2 text-muted-foreground hover:text-foreground transition-colors">
                      {showSecret ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                  <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
                </Field>
              </>
            )}

            {error && <ErrorBanner message={error} />}

            <DialogFooter showCloseButton>
              {isPAT ? (
                <Button onClick={submitPAT}
                  disabled={patMutation.isPending || !name || !pat || (provider === "gitea" && !baseURL)}
                  className="gap-1.5">
                  {patMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  Connect {provider === "gitlab" ? "GitLab" : "Gitea"}
                </Button>
              ) : (
                <Button onClick={submitOAuth}
                  disabled={oauthMutation.isPending || !name || !clientID || !clientSecret || (provider === "gitea" && !baseURL)}
                  className="gap-1.5">
                  {oauthMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  Authorize {provider === "gitlab" ? "GitLab" : "Gitea"}
                </Button>
              )}
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
      <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />{message}
    </div>
  )
}

// ─── Registry section ─────────────────────────────────────────────────────────

function RegistrySection({ orgId, token }: { orgId: string; token: string }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)

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
        <button
          onClick={() => setOpen(true)}
          className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors"
        >
          <Plus className="h-3 w-3" />Add registry
        </button>
      </div>

      {isLoading ? (
        <LoadingRow />
      ) : list.length === 0 ? (
        <EmptyState
          icon={<Box className="h-7 w-7" />}
          title="No registries connected"
          description="Add a registry to pull private images during deployments"
        >
          <button
            onClick={() => setOpen(true)}
            className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-3 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors mt-1"
          >
            <Plus className="h-3 w-3" />Add registry
          </button>
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

      <AddRegistryDialog
        open={open}
        onClose={() => setOpen(false)}
        orgId={orgId}
        token={token}
        onSuccess={() => {
          qc.invalidateQueries({ queryKey: ["registry-integrations", orgId] })
          setOpen(false)
        }}
      />
    </section>
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

// ─── Add registry dialog ──────────────────────────────────────────────────────

const PROVIDERS: { value: RegistryProvider; label: string; needsEndpoint: boolean; userLabel: string; passLabel: string; namespacePlaceholder: string }[] = [
  { value: "ghcr",      label: "GitHub Container Registry", needsEndpoint: false, userLabel: "GitHub username",    passLabel: "Personal access token",  namespacePlaceholder: "ghcr.io/my-org" },
  { value: "dockerhub", label: "Docker Hub",                needsEndpoint: false, userLabel: "Docker Hub username", passLabel: "Password or access token", namespacePlaceholder: "docker.io/my-org" },
  { value: "ecr",       label: "Amazon ECR",                needsEndpoint: true,  userLabel: "AWS access key ID",  passLabel: "AWS secret access key",   namespacePlaceholder: "123456789.dkr.ecr.us-east-1.amazonaws.com" },
  { value: "gcr",       label: "Google Container Registry", needsEndpoint: true,  userLabel: "Username (_json_key)", passLabel: "Service account JSON",  namespacePlaceholder: "gcr.io/my-project" },
  { value: "custom",    label: "Private Registry",          needsEndpoint: true,  userLabel: "Username",           passLabel: "Password or token",       namespacePlaceholder: "registry.example.com/my-org" },
]

function AddRegistryDialog({ open, onClose, orgId, token, onSuccess }: {
  open: boolean
  onClose: () => void
  orgId: string
  token: string
  onSuccess: (reg: ApiRegistryIntegration) => void
}) {
  const [provider, setProvider] = useState<RegistryProvider>("ghcr")
  const [name, setName] = useState("")
  const [endpoint, setEndpoint] = useState("")
  const [namespace, setNamespace] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [showPass, setShowPass] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const providerMeta = PROVIDERS.find((p) => p.value === provider)!

  function reset() {
    setProvider("ghcr")
    setName("")
    setEndpoint("")
    setNamespace("")
    setUsername("")
    setPassword("")
    setShowPass(false)
    setError(null)
  }

  const mutation = useMutation({
    mutationFn: (body: CreateRegistryBody) => registriesApi.create(orgId, body, token),
    onSuccess: (reg) => {
      reset()
      onSuccess(reg)
    },
    onError: (err: Error) => setError(err.message),
  })

  function submit() {
    setError(null)
    mutation.mutate({
      name: name.trim(),
      provider,
      endpoint: endpoint.trim() || undefined,
      namespace: namespace.trim() || undefined,
      username: username.trim(),
      password,
    })
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    submit()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => { if (!o) { reset(); onClose() } }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add container registry</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 mt-1">
          <Field label="Provider">
            <Select value={provider} onValueChange={(v) => setProvider(v as RegistryProvider)}>
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PROVIDERS.map((p) => (
                  <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field label="Label" required>
            <input type="text" placeholder="e.g. production-ghcr"
              value={name} onChange={(e) => setName(e.target.value)}
              className={inputCls}
            />
          </Field>

          {providerMeta.needsEndpoint && (
            <Field label="Registry endpoint">
              <input type="text" placeholder={providerMeta.namespacePlaceholder}
                value={endpoint} onChange={(e) => setEndpoint(e.target.value)}
                className={inputCls + " font-mono"}
              />
            </Field>
          )}

          <Field label="Namespace (optional)">
            <input type="text" placeholder={providerMeta.namespacePlaceholder}
              value={namespace} onChange={(e) => setNamespace(e.target.value)}
              className={inputCls + " font-mono"}
            />
          </Field>

          <Field label={providerMeta.userLabel} required>
            <input type="text" value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="off" className={inputCls}
            />
          </Field>

          <Field label={providerMeta.passLabel} required>
            <div className="flex items-center gap-1">
              <input type={showPass ? "text" : "password"}
                value={password} onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                className={"flex-1 " + inputCls + " font-mono"}
              />
              <button type="button" onClick={() => setShowPass((v) => !v)}
                className="p-2 text-muted-foreground hover:text-foreground transition-colors">
                {showPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
            <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
          </Field>

          {error && <ErrorBanner message={error} />}
        </form>

        <DialogFooter showCloseButton>
          <Button
            onClick={submit}
            disabled={mutation.isPending || !name || !username || !password}
            className="gap-1.5"
          >
            {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Add registry
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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

function NotificationCard({ channel }: { channel: NotificationChannel }) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center gap-3">
        <ProviderIcon provider={channel.type} />
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
