import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bot, ChevronRight, Key, Loader2, Plus, Shield, User } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  agents as agentsApi,
  isTokenActive,
  type AgentDTO,
  type AgentRole,
} from "@/lib/api"
import { TokenRevealDialog } from "@/components/agents/token-reveal-dialog"
import { useMcpUrl } from "@/components/agents/use-mcp-url"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin, useOrgRole } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute("/_app/agents/")({
  component: AgentsPage,
})

function AgentsPage() {
  const role = useOrgRole()
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const isAdmin = useIsAdmin()
  const qc = useQueryClient()
  const mcpUrl = useMcpUrl(orgId, token)

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const [showCreate, setShowCreate] = useState(false)
  const [revealToken, setRevealToken] = useState<string | null>(null)
  const [revealName, setRevealName] = useState<string>("")

  const { data: agents = [], isLoading } = useQuery({
    queryKey: ["agents", orgId],
    queryFn: () => agentsApi.list(orgId, token),
    enabled: !!orgId,
  })

  function onCreated(agentName: string, plaintext: string) {
    qc.invalidateQueries({ queryKey: ["agents", orgId] })
    setShowCreate(false)
    setRevealName(agentName)
    setRevealToken(plaintext)
  }

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Agents</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {agents.length} {agents.length === 1 ? "agent" : "agents"} · machine principals for automation & MCP
          </p>
        </div>
        {isAdmin && (
          <Button
            size="sm"
            variant="outline"
            className="gap-1.5 h-7 text-xs shrink-0"
            onClick={() => setShowCreate(true)}
          >
            <Plus className="h-3.5 w-3.5" />
            New agent
          </Button>
        )}
      </div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      ) : agents.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border/50 py-12 flex flex-col items-center gap-3 text-center">
          <div className="flex items-center justify-center w-10 h-10 rounded-full bg-muted/40">
            <Bot className="h-5 w-5 text-muted-foreground" />
          </div>
          <div className="space-y-1">
            <p className="text-sm font-medium">No agents yet</p>
            <p className="text-xs text-muted-foreground max-w-xs">
              Agents are machine principals with their own tokens and permissions —
              use them to connect automation or an MCP client to your org.
            </p>
          </div>
          {isAdmin && (
            <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs mt-1" onClick={() => setShowCreate(true)}>
              <Plus className="h-3.5 w-3.5" />
              New agent
            </Button>
          )}
        </div>
      ) : (
        <div className="rounded-lg border border-border/60 overflow-hidden divide-y divide-border/40">
          {agents.map((agent) => (
            <AgentRow key={agent.id} agent={agent} />
          ))}
        </div>
      )}

      {agents.length > 0 && (
        <p className="text-[11px] text-muted-foreground/60">
          Remote MCP endpoint: <code className="font-mono text-muted-foreground">{mcpUrl}</code> — pair it with an agent token to connect.
        </p>
      )}

      <CreateAgentDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        orgId={orgId}
        token={token}
        onCreated={onCreated}
      />

      <TokenRevealDialog
        open={!!revealToken}
        onOpenChange={(o) => { if (!o) setRevealToken(null) }}
        token={revealToken}
        agentName={revealName}
        mcpUrl={mcpUrl}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------

function AgentRow({ agent }: { agent: AgentDTO }) {
  const activeTokens = agent.tokens.filter(isTokenActive).length
  const lastUsedMs = agent.tokens
    .map((t) => (t.last_used_at ? new Date(t.last_used_at).getTime() : 0))
    .reduce((a, b) => Math.max(a, b), 0)

  return (
    <Link
      to="/agents/$agentId"
      params={{ agentId: agent.id }}
      className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/20 transition-colors"
    >
      <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 shrink-0">
        <Bot className="h-4 w-4 text-primary" />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium truncate">{agent.name}</p>
        <p className="text-xs text-muted-foreground">
          Created {formatRelativeTime(new Date(agent.created_at))}
        </p>
      </div>

      <RoleBadge role={agent.role} />

      <div className="hidden sm:flex items-center gap-1 text-xs text-muted-foreground shrink-0 w-24 justify-end">
        <Key className="h-3 w-3" />
        {activeTokens} active
      </div>

      <div className="hidden md:block text-xs text-muted-foreground shrink-0 w-24 text-right">
        {lastUsedMs ? formatRelativeTime(new Date(lastUsedMs)) : "Never used"}
      </div>

      <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0 ml-1" />
    </Link>
  )
}

function RoleBadge({ role }: { role: AgentRole }) {
  if (role === "admin") return (
    <Badge className="gap-1 text-[10px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10 shrink-0">
      <Shield className="h-2.5 w-2.5" />admin
    </Badge>
  )
  return (
    <Badge variant="secondary" className="gap-1 text-[10px] px-1.5 py-0 h-5 shrink-0">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}

// ---------------------------------------------------------------------------

function CreateAgentDialog({ open, onOpenChange, orgId, token, onCreated }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  orgId: string
  token: string
  onCreated: (agentName: string, plaintext: string) => void
}) {
  const [name, setName] = useState("")
  const [role, setRole] = useState<AgentRole>("member")
  const [tokenName, setTokenName] = useState("")
  const [expiresAt, setExpiresAt] = useState("")

  useEffect(() => {
    if (open) {
      setName(""); setRole("member"); setTokenName(""); setExpiresAt("")
    }
  }, [open])

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => agentsApi.create(orgId, {
      name: name.trim(),
      role,
      token_name: tokenName.trim() || undefined,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
    }, token),
    onSuccess: (res) => onCreated(res.agent.name, res.token),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New agent</DialogTitle>
          <DialogDescription>
            Create a machine principal. Its first token is generated now and shown once.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Name</label>
            <Input
              placeholder="ci-deploy-bot"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-9 text-sm"
              autoFocus
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Role</label>
            <Select value={role} onValueChange={(v) => v && setRole(v as AgentRole)}>
              <SelectTrigger className="w-full h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue>{role}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">member</SelectItem>
                <SelectItem value="admin">admin</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">Token name <span className="text-muted-foreground/50">(optional)</span></label>
              <Input
                placeholder="default"
                value={tokenName}
                onChange={(e) => setTokenName(e.target.value)}
                className="h-9 text-sm"
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
          </div>

          {error && (
            <p className="text-xs text-destructive">{(error as Error).message}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => mutate()} disabled={isPending || !name.trim()} className="gap-1.5">
            {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
            Create agent
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
