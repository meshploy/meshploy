import { createFileRoute, useNavigate, Link } from "@tanstack/react-router"
import React, { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Bell, Box, ChevronLeft, GitBranch, HardDrive, Mail,
  Loader2, Settings2, AlertCircle,
} from "lucide-react"
import { SiGithub, SiGitlab, SiGitea, SiBitbucket } from "@icons-pack/react-simple-icons"
import { z } from "zod"
import { EventPicker, defaultEvents, useNotificationEvents } from "@/components/notifications/event-picker"
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
  type TokenGitProvider,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Field, Section } from "@/components/services/form-primitives"
import { PasswordInput } from "@/components/forms/password-input"
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
  const { category } = Route.useSearch()
  // Back to the tab this category lives on.
  const back = () => navigate({ to: category === "registry" ? "/integrations/registries" : `/integrations/${category}` })

  return (
    <div className="new-resource-page integration-create bg-background flex flex-col">
      {/* Top bar */}
      <div className="resource-create-top sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <Button
            variant="ghost"
            onClick={back}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            Integrations
          </Button>
          <span className="text-muted-foreground/40">/</span>
          <h1 className="text-2xl font-semibold tracking-tight">Add integration</h1>
        </div>
      </div>

      <div className="resource-create-layout flex flex-1">
        {/* Sidebar */}
        <aside className="resource-type-picker w-52 shrink-0 border-r border-border/40 py-6 px-3 sticky top-14 h-[calc(100vh-3.5rem)] overflow-y-auto">
          <p className="text-[11px] font-medium text-muted-foreground/60 uppercase tracking-wider px-2 mb-2">
            Type
          </p>
          <nav className="space-y-0.5">
            {CATEGORIES.map(({ id, icon: Icon, label, soon }) => (
              <Button
                key={id}
                variant="ghost"
                onClick={() => !soon && navigate({ to: "/integrations/new", search: { category: id }, replace: true })}
                aria-pressed={category === id}
                disabled={soon}
                className={cn(
                  "w-full flex items-center gap-2.5 px-2.5 py-2 rounded-md text-sm transition-colors text-left",
                  category === id && !soon
                    ? "bg-primary/10 text-primary hover:bg-primary/10 hover:text-primary dark:hover:bg-primary/10"
                    : soon
                    ? "text-muted-foreground/40 cursor-not-allowed"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/40"
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="flex-1">{label}</span>
                {soon && (
                  <span className="text-[11px] font-mono border border-border/40 px-1 py-px rounded text-muted-foreground/40">
                    soon
                  </span>
                )}
              </Button>
            ))}
          </nav>
        </aside>

        {/* Form */}
        <main className="resource-form flex-1 py-8 px-8 max-w-2xl">
          {category === "git" && (
            <GitForm onSuccess={back} />
          )}
          {category === "registry" && (
            <RegistryForm onSuccess={back} />
          )}
          {category === "storage" && (
            <StorageForm onSuccess={back} />
          )}
          {category === "notifications" && (
            <NotificationsForm onSuccess={back} />
          )}
          {category === "email" && (
            <EmailProviderForm onSuccess={back} />
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

type GitProvider = "github" | "gitlab" | "gitea" | "bitbucket"
type AuthMethod  = "pat" | "oauth"

const GIT_PROVIDERS: { value: GitProvider; label: string; icon: React.ElementType }[] = [
  { value: "github",    label: "GitHub",    icon: SiGithub    },
  { value: "gitlab",    label: "GitLab",    icon: SiGitlab    },
  { value: "gitea",     label: "Gitea",     icon: SiGitea     },
  { value: "bitbucket", label: "Bitbucket", icon: SiBitbucket },
]

// What the three token/OAuth providers call things. GitHub is not here: it is a
// GitHub App, with its own form above.
//
// A table rather than a ternary per field: with two providers the ternaries
// read fine, with three they stop being readable and start hiding mistakes.
const GIT_PROVIDER_COPY: Record<TokenGitProvider, {
  label: string
  callbackPath: string
  instance: "required" | "optional" | "none"
  instancePlaceholder?: string
  instanceNote?: React.ReactNode
  scopeLabel: string
  scopePlaceholder: string
  namePlaceholder: string
  tokenLabel: string
  tokenPlaceholder?: string
  tokenNote?: string
  clientIDLabel: string
  clientSecretLabel: string
  // What it takes for Meshploy to add the push webhook to a repository itself.
  // Read access is enough to build; creating a webhook is a write, and on every
  // provider it also needs a role on the repository.
  hookNote: React.ReactNode
}> = {
  gitlab: {
    label: "GitLab",
    callbackPath: "/api/v1/gitlab/callback",
    instance: "optional",
    instancePlaceholder: "https://gitlab.example.com",
    scopeLabel: "Group name (optional)",
    scopePlaceholder: "e.g. my-group or my-group/sub-group",
    namePlaceholder: "e.g. my-gitlab-org",
    tokenLabel: "Personal access token",
    tokenPlaceholder: "glpat-…",
    clientIDLabel: "Application ID",
    clientSecretLabel: "Application Secret",
    hookNote: (
      <>
        To let Meshploy add the push webhook for you, the connection needs the <code className="bg-muted px-1 rounded">api</code> scope
        and Maintainer or Owner on the project. With <code className="bg-muted px-1 rounded">read_api</code> it can build, but the
        webhook has to be added by hand from the service's Auto-deploy section.
      </>
    ),
  },
  gitea: {
    label: "Gitea",
    callbackPath: "/api/v1/gitea/callback",
    instance: "required",
    instancePlaceholder: "https://gitea.example.com",
    // Codeberg runs Forgejo, a Gitea fork that kept the same API and webhook
    // signatures, so it needs no provider of its own - only saying so.
    instanceNote: "Codeberg and other Forgejo servers speak the Gitea API: use https://codeberg.org here.",
    scopeLabel: "Organization name (optional)",
    scopePlaceholder: "e.g. my-org",
    namePlaceholder: "e.g. my-gitea-org",
    tokenLabel: "Personal access token",
    clientIDLabel: "Client ID",
    clientSecretLabel: "Client Secret",
    hookNote: (
      <>
        To let Meshploy add the push webhook for you, the connection needs
        <code className="bg-muted px-1 rounded">write:repository</code> and admin rights on the repository. Otherwise it can
        build, and the webhook is added by hand from the service's Auto-deploy section.
      </>
    ),
  },
  bitbucket: {
    label: "Bitbucket",
    callbackPath: "/api/v1/bitbucket/callback",
    // Cloud only: Bitbucket Data Center is a different API, so there is nothing
    // useful to type here.
    instance: "none",
    scopeLabel: "Workspace (optional)",
    scopePlaceholder: "e.g. my-workspace",
    namePlaceholder: "e.g. my-bitbucket-workspace",
    // Atlassian replaced app passwords with scoped API tokens.
    tokenLabel: "API token",
    tokenNote: "An Atlassian API token with scopes, not an app password.",
    clientIDLabel: "Consumer Key",
    clientSecretLabel: "Consumer Secret",
    hookNote: (
      <>
        <code className="bg-muted px-1 rounded">write:webhook:bitbucket</code> — or Webhooks: Read and write on the consumer — is
        what lets Meshploy add the push webhook for you. Without it, it can build, and the webhook is added by hand from the
        service's Auto-deploy section.
      </>
    ),
  },
}

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
  const [clientID,   setClientID]   = useState("")
  const [clientSecret, setClientSecret] = useState("")
  const [githubOrg,  setGithubOrg]  = useState("")
  const [error,      setError]      = useState<string | null>(null)
  const [actioning,  setActioning]  = useState(false)

  const apiBase   = getAPIBase()
  const isPAT     = authMethod === "pat"
  // Every provider but GitHub is connected with a token or an OAuth app, and
  // copy tells this form what each of them calls things.
  const isTokenProvider = provider !== "github"
  const copy        = isTokenProvider ? GIT_PROVIDER_COPY[provider as TokenGitProvider] : null
  const redirectURI = copy ? `${apiBase}${copy.callbackPath}` : ""
  const needsInstance = copy?.instance === "required"

  const patMutation = useMutation({
    mutationFn: (body: { provider: TokenGitProvider; name: string; base_url?: string; groups?: string; token: string }) =>
      gitApi.createPAT(orgId, body, token),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["git-integrations", orgId] }); onSuccess() },
    onError: (err: Error) => setError(err.message),
  })

  const oauthMutation = useMutation({
    mutationFn: (body: { provider: TokenGitProvider; name: string; base_url?: string; groups?: string; redirect_uri: string; client_id: string; client_secret: string }) =>
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
    patMutation.mutate({ provider: provider as TokenGitProvider, name: name.trim(), base_url: baseURL.trim() || undefined, groups: groups.trim() || undefined, token: pat })
  }

  function submitOAuth() {
    setError(null)
    oauthMutation.mutate({ provider: provider as TokenGitProvider, name: name.trim(), base_url: baseURL.trim() || undefined, groups: groups.trim() || undefined, redirect_uri: redirectURI, client_id: clientID.trim(), client_secret: clientSecret })
  }

  const monoCls = inputCls + " font-mono"

  return (
    <div className="space-y-8">
      <Section title="Git Source" subtitle="Connect a git provider to deploy from repositories">
      {/* Provider selector */}
      <div className="space-y-2">
        <p className="text-xs font-medium text-muted-foreground">Provider</p>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          {GIT_PROVIDERS.map(({ value, label, icon: Icon }) => (
            <Button
              key={value}
              variant="ghost"
              onClick={() => { setProvider(value); setAuthMethod("pat"); setError(null) }}
              className={cn(
                "flex items-center justify-center gap-2 px-3 py-2.5 rounded-lg border text-sm font-medium transition-colors",
                provider === value
                  ? "border-primary/50 bg-primary/10 text-foreground hover:bg-primary/10 hover:text-foreground dark:hover:bg-primary/10"
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

      {/* ── GitLab / Gitea / Bitbucket ── */}
      {isTokenProvider && copy && (
        <div className="space-y-4">
          {/* Auth method cards */}
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">Auth method</p>
            <div className="grid grid-cols-2 gap-2">
              {(["pat", "oauth"] as AuthMethod[]).map((m) => (
                <Button key={m} variant="ghost" onClick={() => { setAuthMethod(m); setError(null) }}
                  className={cn(
                    "flex-col items-start whitespace-normal rounded-lg border p-3 text-left transition-colors h-auto",
                    authMethod === m
                      ? "border-primary/50 bg-primary/10 hover:bg-primary/10 dark:hover:bg-primary/10"
                      : "border-border/60 hover:border-border hover:bg-muted/20"
                  )}
                >
                  <p className={cn("text-xs font-semibold", authMethod === m ? "text-foreground" : "text-muted-foreground")}>
                    {m === "pat" ? copy.tokenLabel : "OAuth Application"}
                  </p>
                  <p className="text-[11px] text-muted-foreground/70 mt-0.5 leading-relaxed">
                    {m === "pat" ? "Simple — paste a token from your account settings" : "Standard — stable, refreshable OAuth2 connection"}
                  </p>
                </Button>
              ))}
            </div>
          </div>

          {copy.instance !== "none" && (
            <Field label={copy.instance === "optional" ? `Instance URL (leave empty for ${copy.label.toLowerCase()}.com)` : "Instance URL"}>
              <input type="url"
                placeholder={copy.instancePlaceholder}
                value={baseURL} onChange={(e) => setBaseURL(e.target.value)}
                required={needsInstance}
                className={monoCls}
              />
              {copy.instanceNote && (
                <p className="text-[11px] text-muted-foreground/60 mt-1">{copy.instanceNote}</p>
              )}
            </Field>
          )}

          {/* Setup instructions */}
          <div className="rounded-md border border-border/50 bg-muted/30 px-3 py-2.5 space-y-1.5 text-xs text-muted-foreground">
            <p className="font-medium text-foreground">Setup steps</p>
            {isPAT ? (
              provider === "gitlab" ? (
                <ol className="space-y-1 list-decimal list-inside">
                  <li>Go to your GitLab profile → <span className="font-medium text-foreground">Access Tokens</span></li>
                  <li>Click <span className="font-medium text-foreground">Add new token</span></li>
                  <li>
                    Enable scopes: <code className="bg-muted px-1 rounded">read_api</code>{" "}
                    <code className="bg-muted px-1 rounded">read_repository</code> — or{" "}
                    <code className="bg-muted px-1 rounded">api</code> instead of{" "}
                    <code className="bg-muted px-1 rounded">read_api</code> to have webhooks set up for you
                  </li>
                  <li>Copy the generated token and paste it below</li>
                </ol>
              ) : provider === "gitea" ? (
                <ol className="space-y-1 list-decimal list-inside">
                  <li>Go to your Gitea profile → <span className="font-medium text-foreground">Settings → Applications</span></li>
                  <li>Under <span className="font-medium text-foreground">Manage Access Tokens</span>, click <span className="font-medium text-foreground">Generate Token</span></li>
                  <li>
                    Enable <code className="bg-muted px-1 rounded">repository</code> read, and write as well to have webhooks
                    set up for you
                  </li>
                  <li>Copy the token and paste it below</li>
                </ol>
              ) : (
                <ol className="space-y-1 list-decimal list-inside">
                  <li>Go to <span className="font-medium text-foreground">Atlassian account settings → Security → API tokens</span></li>
                  <li>Click <span className="font-medium text-foreground">Create API token with scopes</span> and pick the Bitbucket product</li>
                  <li>
                    Select <code className="bg-muted px-1 rounded">read:repository:bitbucket</code>{" "}
                    <code className="bg-muted px-1 rounded">read:webhook:bitbucket</code>{" "}
                    <code className="bg-muted px-1 rounded">write:webhook:bitbucket</code>
                  </li>
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
                ) : provider === "gitea" ? (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your Gitea profile → <span className="font-medium text-foreground">Settings → Applications</span></li>
                    <li>Under <span className="font-medium text-foreground">OAuth2 Applications</span>, click <span className="font-medium text-foreground">Create OAuth2 Application</span></li>
                    <li>Name: <span className="font-medium text-foreground">Meshploy</span>, set Redirect URI below</li>
                    <li>Copy the Client ID and Secret and paste them below</li>
                  </ol>
                ) : (
                  <ol className="space-y-1 list-decimal list-inside">
                    <li>Go to your workspace → <span className="font-medium text-foreground">Settings → OAuth consumers</span></li>
                    <li>Add a consumer — Name: <span className="font-medium text-foreground">Meshploy</span>, set the Callback URL below</li>
                    <li>
                      Grant <span className="font-medium text-foreground">Repositories: Read</span> and{" "}
                      <span className="font-medium text-foreground">Webhooks: Read and write</span>. Bitbucket fixes the
                      scopes on the consumer, so they cannot be asked for later
                    </li>
                    <li>Copy the Key and Secret and paste them below</li>
                  </ol>
                )}
                <div className="mt-2 space-y-1">
                  <p className="text-[11px] text-muted-foreground/60">{provider === "bitbucket" ? "Callback URL:" : "Redirect URI:"}</p>
                  <div className="flex items-center gap-1.5 rounded bg-muted px-2 py-1.5">
                    <code className="flex-1 text-[11px] font-mono text-foreground break-all">{redirectURI}</code>
                    <Button variant="ghost" size="icon-sm"
                      onClick={() => navigator.clipboard.writeText(redirectURI)}
                      className="shrink-0 text-muted-foreground hover:text-foreground transition-colors text-[11px]"
                    >Copy</Button>
                  </div>
                </div>
              </>
            )}
            <p className="pt-1.5 text-[11px] leading-relaxed text-muted-foreground/70">{copy.hookNote}</p>
          </div>

          <Field label={copy.scopeLabel}>
            <input type="text"
              placeholder={copy.scopePlaceholder}
              value={groups} onChange={(e) => setGroups(e.target.value)}
              className={inputCls}
            />
          </Field>

          <Field label="Label" required>
            <input type="text"
              placeholder={copy.namePlaceholder}
              value={name} onChange={(e) => setName(e.target.value)}
              className={inputCls}
            />
          </Field>

          {isPAT ? (
            <Field label={copy.tokenLabel} required>
              <PasswordInput
                value={pat} onChange={(e) => setPAT(e.target.value)}
                autoComplete="new-password"
                placeholder={copy.tokenPlaceholder ?? ""}
                className={monoCls}
              />
              <p className="text-[11px] text-muted-foreground/60 mt-1">
                {copy.tokenNote ? `${copy.tokenNote} ` : ""}Stored encrypted with AES-256-GCM
              </p>
            </Field>
          ) : (
            <>
              <Field label={copy.clientIDLabel} required>
                <input type="text" value={clientID} onChange={(e) => setClientID(e.target.value)}
                  autoComplete="off" className={monoCls}
                />
              </Field>
              <Field label={copy.clientSecretLabel} required>
                <PasswordInput
                  value={clientSecret} onChange={(e) => setClientSecret(e.target.value)}
                  autoComplete="new-password" className={monoCls}
                />
                <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
              </Field>
            </>
          )}

          {error && <ErrorBanner message={error} />}

          {isPAT ? (
            <Button
              onClick={submitPAT}
              disabled={patMutation.isPending || !name || !pat || (needsInstance && !baseURL)}
              className="gap-1.5"
            >
              {patMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Connect {copy.label}
            </Button>
          ) : (
            <Button
              onClick={submitOAuth}
              disabled={oauthMutation.isPending || !name || !clientID || !clientSecret || (needsInstance && !baseURL)}
              className="gap-1.5"
            >
              {oauthMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Authorize {copy.label}
            </Button>
          )}
        </div>
      )}
      </Section>
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
    <div className="space-y-8">
      <Section title="Object Storage" subtitle="Connect an S3-compatible bucket for database backups">
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
          <PasswordInput
            value={secretKey} onChange={(e) => setSecretKey(e.target.value)}
            autoComplete="new-password"
            className={monoCls}
          />
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
      </Section>
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
    <div className="space-y-8">
      <Section title="Container Registry" subtitle="Pull and push images from private and public registries">
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
          <PasswordInput
            value={password} onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            className={monoCls}
          />
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
      </Section>
    </div>
  )
}

// ─── Notifications form ───────────────────────────────────────────────────────

function NotificationsForm({ onSuccess }: { onSuccess: () => void }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc    = useQueryClient()

  const [type,    setType]    = useState<NotificationChannelType>("webhook")
  const [name,    setName]    = useState("")
  const [url,     setUrl]     = useState("")
  const [secret,  setSecret]  = useState("")
  const [address, setAddress] = useState("")
  const [events,  setEvents]  = useState<string[]>([])
  const [error,   setError]   = useState<string | null>(null)

  // A new channel starts on the failures preset, once the catalogue says what
  // that is. Untouched selections only: a cleared list stays cleared.
  // An email channel is only usable once the org has somewhere to send from.
  const { data: emailProvider } = useQuery({
    queryKey: ["email-config", orgId],
    queryFn: () => emailConfigApi.get(orgId, token).catch(() => null),
    enabled: !!orgId,
    retry: false,
  })
  const hasEmailProvider = !!emailProvider?.host

  const { data: catalogue } = useNotificationEvents()
  const [touched, setTouched] = useState(false)
  useEffect(() => {
    if (!touched && catalogue?.length) setEvents(defaultEvents(catalogue))
  }, [catalogue, touched])

  const config: Record<string, string> =
    type === "email" ? { address }
    : type === "slack" || type === "discord" ? { webhook_url: url }
    : secret ? { url, secret } : { url }

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
    <div className="space-y-8">
      <Section title="Notification Channel" subtitle="Get alerted when events happen in your org">
        {/* Channel type */}
        <Field label="Channel type">
          <Select value={type} onValueChange={(v) => v && setType(v as NotificationChannelType)}>
            <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
              <SelectValue>{{ webhook: "Webhook", email: "Email", slack: "Slack", discord: "Discord" }[type]}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="webhook">Webhook</SelectItem>
              <SelectItem value="slack">Slack</SelectItem>
              <SelectItem value="discord">Discord</SelectItem>
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

        {/* Without a provider an email channel cannot deliver, so the rest of
            the form is not worth filling in. */}
        {type === "email" && !hasEmailProvider ? (
          <p className="text-sm text-muted-foreground">
            No email provider configured yet ·{" "}
            <Link
              to="/integrations/new"
              search={{ category: "email" as const }}
              className="text-primary hover:text-primary/80 transition-colors"
            >
              Add an email provider
            </Link>
          </p>
        ) : (
        <>
        {/* Type-specific config */}
        {type === "email" ? (
          <Field label="Send to">
            <input
              type="email"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder="ops@company.com"
              className={inputCls}
            />
          </Field>
        ) : (
          <>
            <Field label={type === "webhook" ? "URL" : "Webhook URL"}>
              <input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder={
                  type === "slack" ? "https://hooks.slack.com/services/…"
                  : type === "discord" ? "https://discord.com/api/webhooks/…"
                  : "https://example.com/hooks/meshploy"
                }
                className={inputCls}
              />
            </Field>
            {type === "webhook" && (
              <Field label="Secret (optional)">
                <PasswordInput
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                  placeholder="optional signing secret"
                  className={inputCls}
                />
              </Field>
            )}
          </>
        )}

        {/* Events */}
        <Field label="Notify on">
          <EventPicker events={events} onChange={(next) => { setTouched(true); setEvents(next) }} />
        </Field>
        </>
        )}
      </Section>

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
    <div className="space-y-8">
      <Section title="Email Provider" subtitle="Configure outbound SMTP for email notifications. One provider per organization.">

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
          <p className="text-[11px] text-muted-foreground/60 mt-1">
            Usually the full mailbox, e.g. <code className="font-mono">alerts@yourdomain.com</code>. API-key relays differ: SendGrid uses <code className="font-mono">apikey</code>.
          </p>
        </Field>

        <Field label={prefilled ? "Password (leave empty to keep current)" : "Password"} required={!prefilled}>
          <PasswordInput
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            placeholder={prefilled ? "unchanged" : ""}
            className={monoCls}
          />
          <p className="text-[11px] text-muted-foreground/60 mt-1">Stored encrypted with AES-256-GCM</p>
        </Field>

        <Field label="From address" required>
          <input type="email" placeholder="noreply@yourcompany.com"
            value={fromAddress} onChange={(e) => setFromAddress(e.target.value)}
            className={inputCls}
          />
          <p className="text-[11px] text-muted-foreground/60 mt-1">
            Mailbox providers such as Zoho, Gmail and Microsoft 365 only send as the signed-in mailbox or one of its aliases; anything else is refused as a relay.
          </p>
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
      </Section>
    </div>
  )
}
