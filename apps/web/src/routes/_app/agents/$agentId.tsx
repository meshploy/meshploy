import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ArrowLeft, Bot, Check, Copy, KeyRound, Loader2, Plus, Shield, Trash2, User,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import {
  agents as agentsApi,
  tokenState,
  type AgentDTO,
  type AgentRole,
  type AgentTokenDTO,
} from "@/lib/api"
import { PrincipalPermissions } from "@/components/permissions/principal-permissions"
import { TokenRevealDialog } from "@/components/agents/token-reveal-dialog"
import { useMcpUrl } from "@/components/agents/use-mcp-url"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useOrgRole } from "@/store/org-store"
import { cn, formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/agents/$agentId")({
  component: AgentDetailPage,
})

function AgentDetailPage() {
  const { agentId } = Route.useParams()
  const role = useOrgRole()
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const mcpUrl = useMcpUrl(orgId, token)

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const { data: agents = [], isLoading } = useQuery({
    queryKey: ["agents", orgId],
    queryFn: () => agentsApi.list(orgId, token),
    enabled: !!orgId,
  })

  const agent = agents.find((a) => a.id === agentId)

  const [showAddToken, setShowAddToken] = useState(false)
  const [revealToken, setRevealToken] = useState<string | null>(null)

  if (!agent && agents.length > 0) {
    return <div className="p-6 text-sm text-muted-foreground">Agent not found.</div>
  }

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div className="space-y-4">
        <Link
          to="/agents"
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          Agents
        </Link>

        {agent && <AgentHeader agent={agent} />}
      </div>

      {isLoading || !agent ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : (
        <>
          <McpConnectPanel mcpUrl={mcpUrl} />

          <TokensSection
            agent={agent}
            orgId={orgId}
            token={token}
            onAddToken={() => setShowAddToken(true)}
          />

          <PrincipalPermissions orgId={orgId} principalId={agent.id} token={token} />

          <DangerZone agent={agent} orgId={orgId} token={token} />

          <AddTokenDialog
            open={showAddToken}
            onOpenChange={setShowAddToken}
            orgId={orgId}
            agentId={agent.id}
            token={token}
            onCreated={(plaintext) => { setShowAddToken(false); setRevealToken(plaintext) }}
          />

          <TokenRevealDialog
            open={!!revealToken}
            onOpenChange={(o) => { if (!o) setRevealToken(null) }}
            token={revealToken}
            agentName={agent.name}
            mcpUrl={mcpUrl}
          />
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

function AgentHeader({ agent }: { agent: AgentDTO }) {
  return (
    <div className="flex items-center gap-3">
      <div className="flex items-center justify-center w-10 h-10 rounded-full bg-primary/10 shrink-0">
        <Bot className="h-5 w-5 text-primary" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-base font-semibold truncate">{agent.name}</p>
          <RoleBadge role={agent.role} />
        </div>
        <p className="text-sm text-muted-foreground">
          Created {formatRelativeTime(new Date(agent.created_at))}
        </p>
      </div>
    </div>
  )
}

function RoleBadge({ role }: { role: AgentRole }) {
  if (role === "admin") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10">
      <Shield className="h-2.5 w-2.5" />admin
    </Badge>
  )
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}

// ---------------------------------------------------------------------------
// MCP connect
// ---------------------------------------------------------------------------

function McpConnectPanel({ mcpUrl }: { mcpUrl: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Remote MCP</p>
      </div>
      <div className="p-4 space-y-1.5">
        <p className="text-xs text-muted-foreground font-medium">Connect endpoint</p>
        <div className="flex items-center gap-2">
          <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
            {mcpUrl}
          </code>
          <Button
            size="icon"
            variant="ghost"
            className="h-8 w-8 shrink-0"
            onClick={async () => { await navigator.clipboard.writeText(mcpUrl); setCopied(true); setTimeout(() => setCopied(false), 2000) }}
          >
            {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground/60">
          Paste this URL plus one of the agent's tokens into your agent platform to connect over MCP.
        </p>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------

function TokensSection({ agent, orgId, token, onAddToken }: {
  agent: AgentDTO
  orgId: string
  token: string
  onAddToken: () => void
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">Tokens</h2>
        <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs" onClick={onAddToken}>
          <Plus className="h-3.5 w-3.5" />
          New token
        </Button>
      </div>

      {agent.tokens.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/50 py-8 flex flex-col items-center gap-2 text-muted-foreground">
          <p className="text-xs">No tokens — mint one to let this agent authenticate.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {agent.tokens.map((t) => (
            <TokenRow key={t.id} tokenMeta={t} agentId={agent.id} orgId={orgId} token={token} />
          ))}
        </div>
      )}
    </div>
  )
}

function TokenRow({ tokenMeta, agentId, orgId, token }: {
  tokenMeta: AgentTokenDTO
  agentId: string
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const state = tokenState(tokenMeta)

  const { mutate: revoke, isPending } = useMutation({
    mutationFn: () => agentsApi.revokeToken(orgId, agentId, tokenMeta.id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["agents", orgId] }),
  })

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-muted/40 shrink-0">
        <KeyRound className="h-3.5 w-3.5 text-muted-foreground" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium truncate">{tokenMeta.name || "token"}</p>
          <TokenStateBadge state={state} />
        </div>
        <p className="text-xs text-muted-foreground font-mono">
          {tokenMeta.token_prefix}…
          <span className="font-sans ml-2">
            {tokenMeta.last_used_at
              ? `used ${formatRelativeTime(new Date(tokenMeta.last_used_at))}`
              : "never used"}
          </span>
          {tokenMeta.expires_at && state !== "expired" && (
            <span className="font-sans ml-2">· expires {new Date(tokenMeta.expires_at).toLocaleDateString()}</span>
          )}
        </p>
      </div>

      {state === "active" && (
        <Button
          size="sm"
          variant="ghost"
          className="h-7 text-xs text-destructive hover:text-destructive shrink-0"
          onClick={() => revoke()}
          disabled={isPending}
        >
          {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : "Revoke"}
        </Button>
      )}
    </div>
  )
}

function TokenStateBadge({ state }: { state: ReturnType<typeof tokenState> }) {
  if (state === "active") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-emerald-500/10 text-emerald-400 border-emerald-500/20 hover:bg-emerald-500/10">
      active
    </Badge>
  )
  if (state === "expired") return (
    <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-5 text-muted-foreground">expired</Badge>
  )
  return (
    <Badge className="text-[10px] px-1.5 py-0 h-5 bg-destructive/10 text-destructive border-destructive/20 hover:bg-destructive/10">
      revoked
    </Badge>
  )
}

// ---------------------------------------------------------------------------
// Add token dialog
// ---------------------------------------------------------------------------

function AddTokenDialog({ open, onOpenChange, orgId, agentId, token, onCreated }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  orgId: string
  agentId: string
  token: string
  onCreated: (plaintext: string) => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState("")
  const [expiresAt, setExpiresAt] = useState("")

  useEffect(() => {
    if (open) { setName(""); setExpiresAt("") }
  }, [open])

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => agentsApi.createToken(orgId, agentId, {
      name: name.trim() || undefined,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
    }, token),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["agents", orgId] })
      onCreated(res.token)
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New token</DialogTitle>
          <DialogDescription>Mint an additional token for this agent. Shown once.</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Name <span className="text-muted-foreground/50">(optional)</span></label>
            <Input
              placeholder="ci-runner"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-9 text-sm"
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Expires <span className="text-muted-foreground/50">(optional)</span></label>
            <Input
              type="date"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
              className="h-9 text-sm"
            />
          </div>
          {error && <p className="text-xs text-destructive">{(error as Error).message}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => mutate()} disabled={isPending} className="gap-1.5">
            {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <KeyRound className="h-3.5 w-3.5" />}
            Generate token
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------------------
// Danger zone
// ---------------------------------------------------------------------------

function DangerZone({ agent, orgId, token }: { agent: AgentDTO; orgId: string; token: string }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [confirm, setConfirm] = useState("")

  const { mutate, isPending, error, isError } = useMutation({
    mutationFn: () => agentsApi.remove(orgId, agent.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["agents", orgId] })
      navigate({ to: "/agents" })
    },
  })

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-sm font-medium text-destructive/80">Danger zone</h2>
        <p className="text-xs text-muted-foreground mt-0.5">Irreversible actions. Proceed with caution.</p>
      </div>

      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 space-y-4">
        <div>
          <p className="text-sm font-medium">Delete agent</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            Permanently deletes this agent, revokes all its tokens, and removes its permission grants.
          </p>
        </div>
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            Type <code className="font-mono text-foreground">{agent.name}</code> to confirm:
          </p>
          <Input
            className={cn("h-9 text-sm max-w-xs")}
            placeholder={agent.name}
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
          />
        </div>
        <Button
          variant="destructive"
          size="sm"
          className="gap-1.5"
          disabled={confirm !== agent.name || isPending}
          onClick={() => mutate()}
        >
          {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
          Delete agent
        </Button>
        {isError && <p className="text-xs text-destructive">{(error as Error).message}</p>}
      </div>
    </section>
  )
}
