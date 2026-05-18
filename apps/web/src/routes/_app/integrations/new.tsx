import { createFileRoute, useNavigate } from "@tanstack/react-router"
import React, { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Bell, Box, ChevronLeft, Eye, EyeOff, GitBranch, HardDrive, Mail,
  Loader2, Settings2, AlertCircle,
} from "lucide-react"
import { SiGithub, SiGitlab, SiGitea } from "@icons-pack/react-simple-icons"
import { z } from "zod"
import { Button } from "@/components/ui/button"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  gitIntegrations as gitApi,
  registries as registriesApi,
  storage as storageApi,
  notifications as notificationsApi,
  emailConfig as emailConfigApi,
  type RegistryProvider,
  type StorageProvider,
  type NotificationChannelType,
  type CreateRegistryBody,
  type CreateStorageBody,
  type ApiRegistryIntegration,
  type SaveEmailConfigBody,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Field } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

// ─── Route ────────────────────────────────────────────────────────────────────

const searchSchema = z.object({
  category: z.enum(["git", "registry", "storage", "notifications", "email"]).optional().default("git"),
})

export const Route = createFileRoute("/_app/integrations/new")({
  validateSearch: searchSchema,
  component: NewIntegrationPage,
})

// ─── Category definitions ─────────────────────────────────────────────────────

type Category = "git" | "registry" | "storage" | "notifications" | "email"

const CATEGORIES: { id: Category; icon: typeof GitBranch; label: string; description: string; soon?: boolean }[] = [
  { id: "git",           icon: GitBranch, label: "Git Source",         description: "GitHub, GitLab, or Gitea" },
  { id: "registry",      icon: Box,       label: "Container Registry", description: "Docker Hub, GHCR, ECR, GCR" },
  { id: "storage",       icon: HardDrive, label: "Object Storage",     description: "S3, R2, or MinIO" },
  { id: "notifications", icon: Bell,      label: "Notifications",      description: "Email or webhook alerts" },
  { id: "email",         icon: Mail,      label: "Email Provider",     description: "SMTP for outbound email" },
]

// ─── Page ─────────────────────────────────────────────────────────────────────

function NewIntegrationPage() {
  const navigate = useNavigate()
  const { category: initialCategory } = Route.useSearch()
  const [category, setCategory] = useState<Category>(initialCategory)

  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top bar */}
      <div className="sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <Button
            variant="ghost"
            onClick={() => navigate({ to: "/integrations" })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            Integrations
          </Button>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-sm font-medium">Add integration</span>
        </div>
      </div>

      <div className="flex flex-1">
        {/* Sidebar */}
        <aside className="w-52 shrink-0 border-r border-border/40 py-6 px-3 sticky top-14 h-[calc(100vh-3.5rem)] overflow-y-auto">
          <p className="text-[11px] font-medium text-muted-foreground/60 uppercase tracking-wider px-2 mb-2">
            Type
          </p>
          <nav className="space-y-0.5">
            {CATEGORIES.map(({ id, icon: Icon, label, soon }) => (
              <Button
                key={id}
                variant="ghost"
                onClick={() => !soon && setCategory(id)}
                disabled={soon}
                className={cn(
                  "w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm transition-colors text-left",
                  category === id && !soon
                    ? "bg-primary/10 text-primary"
                    : soon
                    ? "text-muted-foreground/40 cursor-not-allowed"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/40"
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="flex-1">{label}</span>
                {soon && (
                  <span className="text-[9px] font-mono border border-border/40 px-1 py-px rounded text-muted-foreground/40">
                    soon
                  </span>
                )}
              </Button>
            ))}
          </nav>
        </aside>

        {/* Form */}
        <main className="flex-1 py-8 px-8 max-w-2xl">
          {category === "git" && (
            <GitForm onSuccess={() => navigate({ to: "/integrations" })} />
          )}
          {category === "registry" && (
            <RegistryForm onSuccess={() => navigate({ to: "/integrations" })} />
          )}
          {category === "storage" && (
            <StorageForm onSuccess={() => navigate({ to: "/integrations" })} />
          )}
          {category === "notifications" && (
            <NotificationsForm onSuccess={() => navigate({ to: "/integrations" })} />
          )}
          {category === "email" && (
            <EmailProviderForm onSuccess={() => navigate({ to: "/integrations" })} />
          )}
        </main>
      </div>
    </div>
  )
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function getAPIBase(): string {
  const configured: string =
    (window as Window & { __MESHPLOY_CONFIG__?: { apiUrl: string } })
      .__MESHPLOY_CONFIG__?.apiUrl
    ?? import.meta.env.VITE_API_URL
    ?? ""
  if (!configured) return window.location.origin
  return configured
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
      <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />{message}
    </div>
  )
}

function ComingSoon({ label }: { label: string }) {
  return (
    <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
      <p className="text-sm font-medium">{label}</p>
      <p className="text-xs text-muted-foreground/60">Coming soon</p>
    </div>
  )
}

// ─── Git form ─────────────────────────────────────────────────────────────────

type GitProvider = "github" | "gitlab" | "gitea"
type AuthMethod  = "pat" | "oauth"

const GIT_PROVIDERS: { value: GitProvider; label: string; icon: React.ElementType }[] = [
  { value: "github", label: "GitHub", icon: SiGithub },
  { value: "gitlab", label: "GitLab", icon: SiGitlab  },
  { value: "gitea",  label: "Gitea",  icon: SiGitea   },
]

function GitForm({ onSuccess }: { onSuccess: () => void }) {
  const token  = useAuthStore((s) => s.token)!
  const orgId  = useOrgStore((s) => s.currentOrg?.id)!
  const qc     = useQueryClient()

  const [provider,   setProvider]   = useState<GitProvider>("github")
  const [authMethod, setAuthMethod] = useState<AuthMethod>("pat")
  const [name,       setName]       = useState("")
  const [baseURL,    setBaseURL]    = useState("")
  const [groups,     setGroups]     = useState("")
  const [pat,        setPAT]        = useState("")
  const [showPAT,    setShowPAT]    = useState(false)
  const [clientID,   setClientID]   = useState("")
  const [clientSecret, setClientSecret] = useState("")
  const [showSecret, setShowSecret] = useState(false)
  const [githubOrg,  setGithubOrg]  = useState("")
  const [error,      setError]      = useState<string | null>(null)
  const [actioning,  setActioning]  = useState(false)

  const apiBase         = getAPIBase()
  const gitlabRedirectURI = `${apiBase}/api/v1/gitlab/callback`
  const giteaRedirectURI  = `${apiBase}/api/v1/gitea/callback`
  const isPAT           = authMethod === "pat"
  const isGitLabOrGitea = provider === "gitlab" || provider === "gitea"

  const patMutation = useMutation({
    mutationFn: (body: { provider: "gitlab" | "gitea"; name: string; base_url?: string; groups?: string; token: string }) =>
      gitApi.createPAT(orgId, body, token),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["git-integrations", orgId] }); onSuccess() },
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

  const monoCls = inputCls + " font-mono"

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Git Source</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Connect a git provider to deploy from repositories</p>
      </div>

      {/* Provider selector */}
      <div className="space-y-2">
        <p className="text-xs font-medium text-muted-foreground">Provider</p>
        <div className="grid grid-cols-3 gap-2">
          {GIT_PROVIDERS.map(({ value, label, icon: Icon }) => (
            <Button
              key={value}
              variant="ghost"
              onClick={() => { setProvider(value); setAuthMethod("pat"); setError(null) }}
              className={cn(
                "flex items-center justify-center gap-2 px-3 py-2.5 rounded-lg border text-sm font-medium transition-colors",
                provider === value
                  ? "border-primary/50 bg-primary/10 text-foreground"
                  : "border-border/60 bg-muted/10 text-muted-foreground hover:text-foreground hover:bg-muted/30"
              )}
            >
              <Icon size={15} />
              {label}
            </Button>
          ))}
        </div>
      </div>

      {/* ── GitHub ── */}
      {provider === "github" && (
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground leading-relaxed">
            Register a new GitHub App on your account or organization. Each app gives Meshploy access to one account's repositories.
          </p>
          <Field label="Organization name (optional)">
            <input
              className={inputCls}
              placeholder="Leave empty for personal account"
              value={githubOrg}
              onChange={(e) => setGithubOrg(e.target.value)}
            />
          </Field>
          {error && <ErrorBanner message={error} />}
          <Button onClick={handleGitHubSetup} disabled={actioning} className="gap-1.5">
            {actioning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Settings2 className="h-3.5 w-3.5" />}
            {actioning ? "Opening GitHub…" : "Create GitHub App"}
          </Button>
        </div>
      )}

      {/* ── GitLab / Gitea ── */}
      {isGitLabOrGitea && (
        <div className="space-y-4">
          {/* Auth method cards */}
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">Auth method</p>
            <div className="grid grid-cols-2 gap-2">
              {(["pat", "oauth"] as AuthMethod[]).map((m) => (
                <Button key={m} variant="ghost" onClick={() => { setAuthMethod(m); setError(null) }}
                  className={cn(
                    "rounded-lg border p-3 text-left transition-colors h-auto",
                    authMethod === m ? "border-primary/50 bg-primary/10" : "border-border/60 hover:border-border"
                  )}
                >
                  <p className={cn("text-xs font-semibold", authMethod === m ? "text-foreground" : "text-muted-foreground")}>
                    {m === "pat" ? "Personal Access Token" : "OAuth Application"}
                  </p>
                  <p className="text-[11px] text-muted-foreground/70 mt-0.5 leading-relaxed">
                    {m === "pat" ? "Simple — paste a token from your account settings" : "Standard — stable, refreshable OAuth2 connection"}
                  </p>
                </Button>
              ))}
            </div>
          </div>

          <Field label={provider === "gitlab" ? "Instance URL (leave empty for gitlab.com)" : "Instance URL"}>
            <input type="url"
              placeholder={provider === "gitlab" ? "https://gitlab.example.com" : "https://gitea.example.com"}
              value={baseURL} onChange={(e) => setBaseURL(e.target.value)}
              required={provider === "gitea"}
              className={monoCls}
            />
          </Field>

          {/* Setup instructions */}
          <div className="rounded-md border border-border/50 bg-muted/30 px-3 py-2.5 space-y-1.5 text-xs text-muted-foreground">
            <p className="font-medium text-foreground">Setup steps</p>
            {isPAT ? (
              provider === "gitlab" ? (
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
              )
            ) : (
              <>
                {provider === "gitlab" ? (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your GitLab profile → <span className="font-medium text-foreground">Applications</span></li>
                    <li>Create a new application — Name: <span className="font-medium text-foreground">Meshploy</span></li>
                    <li>Set Redirect URI below, enable scopes: <code className="bg-muted px-1 rounded">api</code> <code className="bg-muted px-1 rounded">read_user</code> <code className="bg-muted px-1 rounded">read_repository</code></li>
                    <li>Copy the Application ID and Secret and paste them below</li>
                  </ol>
                ) : (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your Gitea profile → <span className="font-medium text-foreground">Settings → Applications</span></li>
                    <li>Under <span className="font-medium text-foreground">OAuth2 Applications</span>, click <span className="font-medium text-foreground">Create OAuth2 Application</span></li>
                    <li>Name: <span className="font-medium text-foreground">Meshploy</span>, set Redirect URI below</li>
                    <li>Copy the Client ID and Secret and paste them below</li>
                  </ol>
                )}
                <div className="mt-2 space-y-1">
                  <p className="text-[11px] text-muted-foreground/60">Redirect URI:</p>
                  <div className="flex items-center gap-1.5 rounded bg-muted px-2 py-1.5">
                    <code className="flex-1 text-[11px] font-mono text-foreground break-all">
                      {provider === "gitlab" ? gitlabRedirectURI : giteaRedirectURI}
                    </code>
                    <Button variant="ghost" size="icon-sm"
                      onClick={() => navigator.clipboard.writeText(provider === "gitlab" ? gitlabRedirectURI : giteaRedirectURI)}
                      className="shrink-0 text-muted-foreground hover:text-foreground transition-colors text-[11px]"
                    >Copy</Button>
                  </div>
                </div>
              </>
            )}
          </div>

          <Field label={provider === "gitlab" ? "Group name (optional)" : "Organization name (optional)"}>
            <input type="text"
              placeholder={provider === "gitlab" ? "e.g. my-group or my-group/sub-group" : "e.g. my-org"}
              value={groups} onChange={(e) => setGroups(e.target.value)}
              className={inputCls}
            />
          </Field>

          <Field label="Label" required>
            <input type="text"
              placeholder={provider === "gitlab" ? "e.g. my-gitlab-org" : "e.g. my-gitea-org"}
              value={name} onChange={(e) => setName(e.target.value)}
              className={inputCls}
            />
          </Field>

          {isPAT ? (
            <Field label="Personal access token" required>
              <div className="flex items-center gap-1">
                <input type={showPAT ? "text" : "password"}
                  value={pat} onChange={(e) => setPAT(e.target.value)}
                  autoComplete="new-password"
                  placeholder={provider === "gitlab" ? "glpat-…" : ""}
                  className={`flex-1 ${monoCls}`}
                />
                <Button variant="ghost" size="icon-sm" onClick={() => setShowPAT((v) => !v)}
                  className="p-2 text-muted-foreground hover:text-foreground transition-colors">
                  {showPAT ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
            </Field>
          ) : (
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
                  <Button variant="ghost" size="icon-sm" onClick={() => setShowSecret((v) => !v)}
                    className="p-2 text-muted-foreground hover:text-foreground transition-colors">
                    {showSecret ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </Button>
                </div>
                <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
              </Field>
            </>
          )}

          {error && <ErrorBanner message={error} />}

          {isPAT ? (
            <Button
              onClick={submitPAT}
              disabled={patMutation.isPending || !name || !pat || (provider === "gitea" && !baseURL)}
              className="gap-1.5"
            >
              {patMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Connect {provider === "gitlab" ? "GitLab" : "Gitea"}
            </Button>
          ) : (
            <Button
              onClick={submitOAuth}
              disabled={oauthMutation.isPending || !name || !clientID || !clientSecret || (provider === "gitea" && !baseURL)}
              className="gap-1.5"
            >
              {oauthMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Authorize {provider === "gitlab" ? "GitLab" : "Gitea"}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Storage form ─────────────────────────────────────────────────────────────

const STORAGE_PROVIDERS: { value: StorageProvider; label: string; needsEndpoint: boolean; endpointPlaceholder?: string }[] = [
  { value: "s3",    label: "Amazon S3",     needsEndpoint: false },
  { value: "r2",    label: "Cloudflare R2", needsEndpoint: true,  endpointPlaceholder: "https://<account-id>.r2.cloudflarestorage.com" },
  { value: "minio", label: "MinIO",         needsEndpoint: true,  endpointPlaceholder: "https://minio.example.com" },
  { value: "b2",    label: "Backblaze B2",  needsEndpoint: true,  endpointPlaceholder: "https://s3.us-west-004.backblazeb2.com" },
]

function StorageForm({ onSuccess }: { onSuccess: () => void }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc    = useQueryClient()

  const [provider,    setProvider]    = useState<StorageProvider>("s3")
  const [name,        setName]        = useState("")
  const [endpoint,    setEndpoint]    = useState("")
  const [region,      setRegion]      = useState("")
  const [bucket,      setBucket]      = useState("")
  const [accessKeyId, setAccessKeyId] = useState("")
  const [secretKey,   setSecretKey]   = useState("")
  const [showSecret,  setShowSecret]  = useState(false)
  const [error,       setError]       = useState<string | null>(null)

  const providerMeta = STORAGE_PROVIDERS.find((p) => p.value === provider)!

  const mutation = useMutation({
    mutationFn: (body: CreateStorageBody) => storageApi.create(orgId, body, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["storage-integrations", orgId] })
      onSuccess()
    },
    onError: (err: Error) => setError(err.message),
  })

  function submit() {
    setError(null)
    mutation.mutate({
      name: name.trim(),
      provider,
      endpoint: endpoint.trim() || undefined,
      region: region.trim() || undefined,
      bucket: bucket.trim(),
      access_key_id: accessKeyId.trim(),
      secret_access_key: secretKey,
    })
  }

  const monoCls = inputCls + " font-mono"

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Object Storage</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Connect an S3-compatible bucket for database backups</p>
      </div>

      <div className="space-y-4">
        <Field label="Provider">
          <Select value={provider} onValueChange={(v) => { setProvider(v as StorageProvider); setEndpoint(""); setError(null) }}>
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue>{providerMeta.label}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {STORAGE_PROVIDERS.map((p) => (
                <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field label="Label" required>
          <input type="text" placeholder="e.g. production-backups"
            value={name} onChange={(e) => setName(e.target.value)}
            className={inputCls}
          />
        </Field>

        {providerMeta.needsEndpoint && (
          <Field label="Endpoint" required>
            <input type="text" placeholder={providerMeta.endpointPlaceholder}
              value={endpoint} onChange={(e) => setEndpoint(e.target.value)}
              className={monoCls}
            />
          </Field>
        )}

        {provider === "s3" && (
          <Field label="Region" required>
            <input type="text" placeholder="us-east-1"
              value={region} onChange={(e) => setRegion(e.target.value)}
              className={monoCls}
            />
          </Field>
        )}

        <Field label="Bucket" required>
          <input type="text" placeholder="my-backups-bucket"
            value={bucket} onChange={(e) => setBucket(e.target.value)}
            className={monoCls}
          />
        </Field>

        <Field label="Access key ID" required>
          <input type="text" value={accessKeyId}
            onChange={(e) => setAccessKeyId(e.target.value)}
            autoComplete="off" className={monoCls}
          />
        </Field>

        <Field label="Secret access key" required>
          <div className="flex items-center gap-1">
            <input type={showSecret ? "text" : "password"}
              value={secretKey} onChange={(e) => setSecretKey(e.target.value)}
              autoComplete="new-password"
              className={`flex-1 ${monoCls}`}
            />
            <Button variant="ghost" size="icon-sm" onClick={() => setShowSecret((v) => !v)}
              className="p-2 text-muted-foreground hover:text-foreground transition-colors">
              {showSecret ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
        </Field>

        {error && <ErrorBanner message={error} />}

        <Button
          onClick={submit}
          disabled={mutation.isPending || !name || !bucket || !accessKeyId || !secretKey || (providerMeta.needsEndpoint && !endpoint) || (provider === "s3" && !region)}
          className="gap-1.5"
        >
          {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Add storage
        </Button>
      </div>
    </div>
  )
}

// ─── Registry form ────────────────────────────────────────────────────────────

const REGISTRY_PROVIDERS: {
  value: RegistryProvider
  label: string
  needsEndpoint: boolean
  userLabel: string
  passLabel: string
  namespacePlaceholder: string
}[] = [
  { value: "ghcr",      label: "GitHub Container Registry", needsEndpoint: false, userLabel: "GitHub username",     passLabel: "Personal access token",   namespacePlaceholder: "ghcr.io/my-org" },
  { value: "dockerhub", label: "Docker Hub",                needsEndpoint: false, userLabel: "Docker Hub username",  passLabel: "Password or access token", namespacePlaceholder: "docker.io/my-org" },
  { value: "ecr",       label: "Amazon ECR",                needsEndpoint: true,  userLabel: "AWS access key ID",   passLabel: "AWS secret access key",    namespacePlaceholder: "123456789.dkr.ecr.us-east-1.amazonaws.com" },
  { value: "gcr",       label: "Google Container Registry", needsEndpoint: true,  userLabel: "Username (_json_key)", passLabel: "Service account JSON",     namespacePlaceholder: "gcr.io/my-project" },
  { value: "custom",    label: "Private Registry",          needsEndpoint: true,  userLabel: "Username",            passLabel: "Password or token",        namespacePlaceholder: "registry.example.com/my-org" },
]

function RegistryForm({ onSuccess }: { onSuccess: (reg: ApiRegistryIntegration) => void }) {
  const token  = useAuthStore((s) => s.token)!
  const orgId  = useOrgStore((s) => s.currentOrg?.id)!
  const qc     = useQueryClient()

  const [provider,   setProvider]   = useState<RegistryProvider>("ghcr")
  const [name,       setName]       = useState("")
  const [endpoint,   setEndpoint]   = useState("")
  const [namespace,  setNamespace]  = useState("")
  const [username,   setUsername]   = useState("")
  const [password,   setPassword]   = useState("")
  const [showPass,   setShowPass]   = useState(false)
  const [error,      setError]      = useState<string | null>(null)

  const providerMeta = REGISTRY_PROVIDERS.find((p) => p.value === provider)!

  const mutation = useMutation({
    mutationFn: (body: CreateRegistryBody) => registriesApi.create(orgId, body, token),
    onSuccess: (reg) => {
      qc.invalidateQueries({ queryKey: ["registry-integrations", orgId] })
      onSuccess(reg)
    },
    onError: (err: Error) => setError(err.message),
  })

  function submit() {
    setError(null)
    mutation.mutate({ name: name.trim(), provider, endpoint: endpoint.trim() || undefined, namespace: namespace.trim() || undefined, username: username.trim(), password })
  }

  const monoCls = inputCls + " font-mono"

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Container Registry</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Pull and push images from private and public registries</p>
      </div>

      <div className="space-y-4">
        <Field label="Provider">
          <Select value={provider} onValueChange={(v) => setProvider(v as RegistryProvider)}>
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue>{providerMeta.label}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {REGISTRY_PROVIDERS.map((p) => (
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
              className={monoCls}
            />
          </Field>
        )}

        <Field label="Namespace (optional)">
          <input type="text" placeholder={providerMeta.namespacePlaceholder}
            value={namespace} onChange={(e) => setNamespace(e.target.value)}
            className={monoCls}
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
              className={`flex-1 ${monoCls}`}
            />
            <Button variant="ghost" size="icon-sm" onClick={() => setShowPass((v) => !v)}
              className="p-2 text-muted-foreground hover:text-foreground transition-colors">
              {showPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
        </Field>

        {error && <ErrorBanner message={error} />}

        <Button
          onClick={submit}
          disabled={mutation.isPending || !name || !username || !password}
          className="gap-1.5"
        >
          {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Add registry
        </Button>
      </div>
    </div>
  )
}

// ─── Notifications form ───────────────────────────────────────────────────────

const ALL_EVENTS = [
  { value: "deploy.success", label: "Deploy succeeded" },
  { value: "deploy.failed",  label: "Deploy failed"    },
  { value: "node.offline",   label: "Node went offline" },
  { value: "backup.success", label: "Backup succeeded" },
  { value: "backup.failed",  label: "Backup failed"    },
]

function NotificationsForm({ onSuccess }: { onSuccess: () => void }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc    = useQueryClient()

  const [type,    setType]    = useState<NotificationChannelType>("webhook")
  const [name,    setName]    = useState("")
  const [url,     setUrl]     = useState("")
  const [secret,  setSecret]  = useState("")
  const [address, setAddress] = useState("")
  const [events,  setEvents]  = useState<string[]>(["deploy.failed", "node.offline"])
  const [showSecret, setShowSecret] = useState(false)
  const [error,   setError]   = useState<string | null>(null)

  const toggleEvent = (ev: string) =>
    setEvents((prev) => prev.includes(ev) ? prev.filter((e) => e !== ev) : [...prev, ev])

  const config: Record<string, string> =
    type === "email" ? { address } : secret ? { url, secret } : { url }

  const mutation = useMutation({
    mutationFn: () => notificationsApi.create(orgId, { name: name.trim(), type, config, events }, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["notification-channels", orgId] })
      onSuccess()
    },
    onError: (err: Error) => setError(err.message),
  })

  const isValid = name.trim() &&
    (type === "email" ? address.trim() : url.trim()) &&
    events.length > 0

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-sm font-semibold">Add notification channel</h2>
        <p className="text-xs text-muted-foreground mt-0.5">Get alerted when events happen in your org</p>
      </div>

      <div className="space-y-4">
        {/* Channel type */}
        <Field label="Channel type">
          <Select value={type} onValueChange={(v) => v && setType(v as NotificationChannelType)}>
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue>{type === "email" ? "Email" : "Webhook"}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="webhook">Webhook</SelectItem>
              <SelectItem value="email">Email</SelectItem>
            </SelectContent>
          </Select>
        </Field>

        {/* Name */}
        <Field label="Name">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={type === "email" ? "ops-email" : "deploy-webhook"}
            className={inputCls}
          />
        </Field>

        {/* Type-specific config */}
        {type === "webhook" ? (
          <>
            <Field label="URL">
              <input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/hooks/meshploy"
                className={inputCls}
              />
            </Field>
            <Field label="Secret (optional)">
              <div className="relative">
                <input
                  type={showSecret ? "text" : "password"}
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                  placeholder="optional signing secret"
                  className={cn(inputCls, "pr-9")}
                />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setShowSecret((s) => !s)}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground"
                >
                  {showSecret ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </Field>
          </>
        ) : (
          <Field label="Email address">
            <input
              type="email"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder="ops@company.com"
              className={inputCls}
            />
          </Field>
        )}

        {/* Events */}
        <Field label="Notify on">
          <div className="space-y-1.5 pt-0.5">
            {ALL_EVENTS.map(({ value, label }) => (
              <label key={value} className="flex items-center gap-2.5 cursor-pointer group">
                <input
                  type="checkbox"
                  checked={events.includes(value)}
                  onChange={() => toggleEvent(value)}
                  className="h-3.5 w-3.5 rounded accent-primary"
                />
                <span className="text-xs text-muted-foreground group-hover:text-foreground transition-colors">
                  {label}
                </span>
                <code className="text-[10px] font-mono text-muted-foreground/50">{value}</code>
              </label>
            ))}
          </div>
        </Field>
      </div>

      {error && <ErrorBanner message={error} />}

      <Button
        onClick={() => mutation.mutate()}
        disabled={mutation.isPending || !isValid}
        className="gap-1.5"
      >
        {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
        Add channel
      </Button>
    </div>
  )
}

// ─── Email provider form ──────────────────────────────────────────────────────

function EmailProviderForm({ onSuccess }: { onSuccess: () => void }) {
  const token  = useAuthStore((s) => s.token)!
  const orgId  = useOrgStore((s) => s.currentOrg?.id)!
  const qc     = useQueryClient()

  const [host,        setHost]        = useState("")
  const [port,        setPort]        = useState("587")
  const [username,    setUsername]    = useState("")
  const [password,    setPassword]    = useState("")
  const [fromAddress, setFromAddress] = useState("")
  const [fromName,    setFromName]    = useState("")
  const [useTLS,      setUseTLS]      = useState(true)
  const [showPass,    setShowPass]    = useState(false)
  const [error,       setError]       = useState<string | null>(null)

  // Pre-fill from existing config if present.
  const { data: existing } = useQuery({
    queryKey: ["email-config", orgId],
    queryFn: () => emailConfigApi.get(orgId, token).catch(() => null),
    enabled: !!orgId,
  })

  const prefilled = existing != null
  useEffect(() => {
    if (!existing) return
    setHost(existing.host)
    setPort(String(existing.port))
    setUsername(existing.username)
    setFromAddress(existing.from_address)
    setFromName(existing.from_name)
    setUseTLS(existing.use_tls)
  }, [existing])

  const mutation = useMutation({
    mutationFn: (body: SaveEmailConfigBody) => emailConfigApi.save(orgId, body, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["email-config", orgId] })
      onSuccess()
    },
    onError: (err: Error) => setError(err.message),
  })

  function submit() {
    setError(null)
    mutation.mutate({
      host: host.trim(),
      port: parseInt(port, 10) || 587,
      username: username.trim(),
      password,
      from_address: fromAddress.trim(),
      from_name: fromName.trim() || undefined,
      use_tls: useTLS,
    })
  }

  const monoCls = inputCls + " font-mono"
  const isValid = host.trim() && fromAddress.trim() && (!prefilled ? password : true)

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Email Provider</h2>
        <p className="text-sm text-muted-foreground mt-0.5">
          Configure outbound SMTP for email notifications. One provider per organization.
        </p>
      </div>

      <div className="space-y-4">
        <div className="grid grid-cols-3 gap-3">
          <div className="col-span-2">
            <Field label="SMTP host" required>
              <input type="text" placeholder="smtp.example.com"
                value={host} onChange={(e) => setHost(e.target.value)}
                className={monoCls}
              />
            </Field>
          </div>
          <Field label="Port" required>
            <input type="number" placeholder="587"
              value={port} onChange={(e) => setPort(e.target.value)}
              min={1} max={65535}
              className={monoCls}
            />
          </Field>
        </div>

        <Field label="Username">
          <input type="text" placeholder="user@example.com"
            value={username} onChange={(e) => setUsername(e.target.value)}
            autoComplete="off"
            className={inputCls}
          />
        </Field>

        <Field label={prefilled ? "Password (leave empty to keep current)" : "Password"} required={!prefilled}>
          <div className="flex items-center gap-1">
            <input
              type={showPass ? "text" : "password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              placeholder={prefilled ? "unchanged" : ""}
              className={`flex-1 ${monoCls}`}
            />
            <Button variant="ghost" size="icon-sm" onClick={() => setShowPass((v) => !v)}
              className="p-2 text-muted-foreground hover:text-foreground transition-colors">
              {showPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
        </Field>

        <Field label="From address" required>
          <input type="email" placeholder="noreply@yourcompany.com"
            value={fromAddress} onChange={(e) => setFromAddress(e.target.value)}
            className={inputCls}
          />
        </Field>

        <Field label="From name (optional)">
          <input type="text" placeholder="Meshploy"
            value={fromName} onChange={(e) => setFromName(e.target.value)}
            className={inputCls}
          />
        </Field>

        <label className="flex items-center gap-2.5 cursor-pointer">
          <input type="checkbox" checked={useTLS} onChange={(e) => setUseTLS(e.target.checked)}
            className="h-3.5 w-3.5 rounded accent-primary"
          />
          <span className="text-xs text-muted-foreground">Use TLS / STARTTLS</span>
        </label>

        {error && <ErrorBanner message={error} />}

        <Button
          onClick={submit}
          disabled={mutation.isPending || !isValid}
          className="gap-1.5"
        >
          {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          {prefilled ? "Update provider" : "Save provider"}
        </Button>
      </div>
    </div>
  )
}
